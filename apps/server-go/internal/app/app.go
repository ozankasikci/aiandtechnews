package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/collector"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/content"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/editorial"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/gemini"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/health"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/httpserver"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/indexnow"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/media"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
)

type App struct {
	address       string
	handler       http.Handler
	server        *httpserver.Server
	background    []func(context.Context)
	feedCollector *collector.Collector
}

// New composes the health-only application without opening or inspecting a database.
func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	handler := httpserver.NewRouter(logger, health.MountPublic)
	server := httpserver.NewServer(cfg.Address, handler, logger)
	return &App{address: cfg.Address, handler: handler, server: server}, nil
}

// NewWithDatabase composes database-backed routes around a caller-owned pool.
// It does not ping, migrate, seed, or close the database.
func NewWithDatabase(cfg config.Config, logger *slog.Logger, db *sql.DB) (*App, error) {
	return NewWithDatabaseAt(cfg, logger, db, time.Now)
}

// NewWithDatabaseAt is the deterministic database-backed composition seam.
// Production uses NewWithDatabase, which supplies time.Now.
func NewWithDatabaseAt(cfg config.Config, logger *slog.Logger, db *sql.DB, now func() time.Time) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger is required")
	}
	if db == nil {
		return nil, errors.New("database is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tokens, err := editorial.NewJWT(cfg.JWTSecret, now)
	if err != nil {
		return nil, err
	}
	contentStore := content.NewSQLiteStore(db)
	articles := content.NewPublicHandler(content.NewService(contentStore), logger)
	categories := content.NewCategoryPublicHandler(content.NewCategoryService(contentStore), logger)
	editorialStore := editorial.NewSQLiteStore(db)
	authors := editorial.NewPublicHandler(editorial.NewService(editorialStore), logger)
	auth := editorial.NewAuthHandler(editorial.NewLoginService(editorialStore, editorial.NewBcryptVerifier(), tokens), tokens, logger)
	newsroomStore := newsroom.NewSQLiteStore(db)
	newsroomService, err := newsroom.NewService(newsroomStore, now, newsroom.RandomMinutes)
	if err != nil {
		return nil, err
	}
	var feedCollector *collector.Collector
	var newsroomCollector newsroom.Collector
	if cfg.CollectorEnabled {
		feedCollector = collector.New(collector.NewFetcher(), newsroomStore, content.ApprovedFeeds(), now, logger)
		newsroomCollector = feedCollector
	}
	newsroomHandler := newsroom.NewHandler(newsroomService, newsroomCollector, logger)
	handler := httpserver.NewRouter(logger, func(router chi.Router) {
		health.MountPublic(router)
		articles.MountPublic(router)
		categories.MountPublic(router)
		authors.MountPublic(router)
		auth.Mount(router)
		newsroomHandler.Mount(router, auth.RequireAuth)
	})
	server := httpserver.NewServer(cfg.Address, handler, logger)
	application := &App{address: cfg.Address, handler: handler, server: server}
	if feedCollector != nil {
		application.feedCollector = feedCollector
		application.background = append(application.background, func(ctx context.Context) {
			feedCollector.Loop(ctx, cfg.CollectorInterval)
		})
	}
	if cfg.PublisherEnabled {
		awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(cfg.AWSRegion))
		if err != nil {
			return nil, fmt.Errorf("load AWS configuration: %w", err)
		}
		geminiClient := gemini.New(cfg.GeminiAPIKey, cfg.GeminiTextModel,
			gemini.WithImageModel(cfg.GeminiImageModel), gemini.WithVisionModel(cfg.GeminiVisionModel))
		httpClient := &http.Client{}
		imageStore := media.NewStore(media.Config{Region: cfg.AWSRegion, Bucket: cfg.S3Bucket, Prefix: cfg.S3Prefix, PublicBaseURL: cfg.S3PublicURL},
			s3.NewFromConfig(awsCfg), httpClient, now)
		newsPublisher := publisher.New(publisher.Deps{
			Store:       newsroomStore,
			Fetcher:     collector.NewFetcher(),
			Rewriter:    publisher.NewRewriter(geminiClient),
			Illustrator: illustration.NewS3Illustrator(illustration.NewGenerator(geminiClient, logger), imageStore, httpClient, logger),
			Articles:    publisher.NewSQLiteArticles(db, now),
			Notifier:    indexnow.New(),
			Now:         now,
			Logger:      logger,
		})
		application.background = append(application.background, func(ctx context.Context) {
			newsPublisher.Loop(ctx, cfg.PublisherInterval)
		})
	}
	return application, nil
}

// Migrations explicitly collects capability-owned descriptors in global order.
func Migrations() []migrate.Descriptor {
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	return append(descriptors, newsroom.Migrations()...)
}

func (a *App) Address() string       { return a.address }
func (a *App) Handler() http.Handler { return a.handler }

// Run serves HTTP and runs background tasks (the collector loop) until ctx is
// done, then waits for the tasks to stop. It also binds the feed collector's
// lifecycle to the tasks context, so a "Collect now" run started in the
// background (collector.Collector.Start) is cancelled and waited for too,
// instead of being abandoned mid-write on shutdown.
func (a *App) Run(ctx context.Context) error {
	tasksCtx, cancel := context.WithCancel(ctx)
	if a.feedCollector != nil {
		a.feedCollector.Bind(tasksCtx)
	}
	var wg sync.WaitGroup
	for _, task := range a.background {
		wg.Add(1)
		go func(task func(context.Context)) {
			defer wg.Done()
			task(tasksCtx)
		}(task)
	}
	err := a.server.Run(ctx)
	cancel()
	wg.Wait()
	if a.feedCollector != nil {
		a.feedCollector.Wait()
	}
	return err
}
