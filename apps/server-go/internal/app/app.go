package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

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
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsroom"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/publisher"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/settings"
)

type App struct {
	address       string
	handler       http.Handler
	server        *httpserver.Server
	background    []func(context.Context)
	feedCollector *collector.Collector
	// newsletter is bound to Run's context so shutdown stops a digest run.
	newsletter *newsletter.Service
	logger     *slog.Logger
	// drains run after the server and background tasks stop, to finish
	// fire-and-forget work such as dashboard IndexNow submissions.
	drains []func()
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
	if cfg.UploadsDir == "" {
		return nil, errors.New("uploads directory is required")
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
	if !cfg.IndexNowEnabled {
		logger.Info("IndexNow disabled; published and dashboard-changed URLs will not be submitted")
	}
	dashboardIndexNow, drainIndexNow := newDashboardIndexNow(cfg, logger)
	dashboardContent := content.NewAdminHandler(content.NewAdminService(contentStore, now), dashboardIndexNow, logger)
	dashboardSettings := settings.NewHandler(settings.NewService(settings.NewSQLiteStore(db)), logger)
	// The publisher and S3 media storage share one checked S3 client.
	var objectAPI media.ObjectAPI
	if cfg.PublisherEnabled || cfg.MediaStorage == config.MediaStorageS3 {
		objectAPI, err = openPublisherStorage(context.Background(), cfg)
		if err != nil {
			if cfg.PublisherEnabled {
				return nil, fmt.Errorf("publisher storage check failed: %w", err)
			}
			return nil, fmt.Errorf("media storage check failed: %w", err)
		}
	}
	// UPLOADS_DIR is always served at /uploads/*: with MEDIA_STORAGE=s3 it
	// still holds the legacy files existing articles reference.
	uploads := media.NewUploads(cfg.UploadsDir)
	var s3Uploads *media.S3Uploads
	if cfg.MediaStorage == config.MediaStorageS3 {
		s3Uploads = media.NewS3Uploads(media.S3UploadsConfig{Bucket: cfg.S3Bucket, Prefix: cfg.MediaS3Prefix, PublicBaseURL: cfg.S3PublicURL},
			objectAPI, newPublicHTTPClient())
	}
	mediaStorage := media.NewStorage(uploads, s3Uploads)
	newsletterService, err := newsletter.NewService(newsletter.NewSQLiteStore(db),
		newsletter.NewResendSender(newsletter.ResendConfig{
			APIKey: cfg.ResendAPIKey, From: cfg.NewsletterFrom, ReplyTo: cfg.NewsletterReplyTo,
			Endpoint: resendEndpoint, Client: newResendHTTPClient(),
		}),
		newsletter.ServiceConfig{SiteURL: cfg.NewsletterSiteURL, TokenSecret: cfg.NewsletterTokenSecret, Pace: newsletterPace},
		logger)
	if err != nil {
		return nil, fmt.Errorf("newsletter: %w", err)
	}
	if strings.TrimSpace(cfg.ResendAPIKey) == "" || strings.TrimSpace(cfg.NewsletterFrom) == "" {
		logger.Info("Newsletter delivery not configured (RESEND_API_KEY, NEWSLETTER_FROM); signups still work, digest deliveries will fail")
	}
	newsletterHandler := newsletter.NewHandler(newsletterService, cfg.NewsletterCronSecret, now, logger)
	dashboardMedia := media.NewHandler(media.NewLibrary(media.NewSQLiteLibrary(db), mediaStorage, now), mediaStorage, logger)
	handler := httpserver.NewRouter(logger, func(router chi.Router) {
		// Health answers 503 when the database cannot answer a query.
		health.MountChecked(router, func(ctx context.Context) error {
			var one int
			return db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
		})
		articles.MountPublic(router)
		categories.MountPublic(router)
		authors.MountPublic(router)
		auth.Mount(router)
		newsletterHandler.Mount(router)
		newsroomHandler.Mount(router, auth.RequireAuth)
		// Node guards every dashboard path, including unknown ones, with
		// requireAuth before routing (dashboard.ts:94).
		router.Route("/dashboard", func(dashboard chi.Router) {
			dashboard.Use(auth.RequireAuth)
			dashboardContent.Mount(dashboard)
			dashboardSettings.Mount(dashboard)
			dashboardMedia.Mount(dashboard)
		})
	}, uploads.MountStatic)
	server := httpserver.NewServer(cfg.Address, handler, logger)
	application := &App{address: cfg.Address, handler: handler, server: server, newsletter: newsletterService, logger: logger, drains: []func(){drainIndexNow}}
	if feedCollector != nil {
		application.feedCollector = feedCollector
		application.background = append(application.background, func(ctx context.Context) {
			feedCollector.Loop(ctx, cfg.CollectorInterval)
		})
	}
	if cfg.PublisherEnabled {
		geminiClient := gemini.New(cfg.GeminiAPIKey, cfg.GeminiTextModel,
			gemini.WithImageModel(cfg.GeminiImageModel), gemini.WithImageSize(cfg.GeminiImageSize),
			gemini.WithVisionModel(cfg.GeminiVisionModel))
		imageStore := media.NewStore(media.Config{Region: cfg.AWSRegion, Bucket: cfg.S3Bucket, Prefix: cfg.S3Prefix, PublicBaseURL: cfg.S3PublicURL},
			objectAPI, newPublicHTTPClient(), now)
		newsPublisher := publisher.New(publisher.Deps{
			Store:       newsroomStore,
			Fetcher:     collector.NewFetcher(),
			Rewriter:    publisher.NewRewriter(geminiClient),
			Illustrator: illustration.NewS3Illustrator(illustration.NewGenerator(geminiClient, logger), imageStore, illustration.NewReferenceClient(), logger),
			Articles:    publisher.NewSQLiteArticles(db, now),
			Notifier:    newPublisherNotifier(cfg, logger),
			Now:         now,
			Logger:      logger,
		})
		application.background = append(application.background, func(ctx context.Context) {
			newsPublisher.Loop(ctx, cfg.PublisherInterval)
		})
	}
	return application, nil
}

// newsletterDrainTimeout bounds how long Run waits for an in-flight digest
// or welcome email after shutdown cancelled it (a delivery outcome write
// takes at most 10s).
const newsletterDrainTimeout = 15 * time.Second

// resendEndpoint, newResendHTTPClient, and newsletterPace configure newsletter
// delivery. They are variables only so app tests can point delivery at an
// httptest server and skip Node's 550ms pause; nil means the newsletter
// package's defaults (a client with ResendAttemptTimeout, 550ms pacing).
var (
	resendEndpoint                                  = newsletter.DefaultResendEndpoint
	newResendHTTPClient                             = func() *http.Client { return nil }
	newsletterPace      func(context.Context) error = nil
)

// newPublicHTTPClient builds the client that verifies uploaded objects
// through their public URL. It is a variable so app tests can stay offline.
var newPublicHTTPClient = func() *http.Client { return &http.Client{} }

// newPublisherNotifier chooses the publisher's IndexNow notifier. A local dev
// publish must never ping IndexNow for an article that only exists in a dev
// database, so submission is opt-in via INDEXNOW_ENABLED; when it is off, a
// no-op notifier is used instead. The startup log explaining that covers both
// this and the dashboard notifier is logged once by the caller.
func newPublisherNotifier(cfg config.Config, logger *slog.Logger) publisher.Notifier {
	if cfg.IndexNowEnabled {
		return indexnow.New()
	}
	return publisher.NoopNotifier{}
}

// CheckSchema is the read-only startup check for production: the database
// must be managed by the migration ledger (never a Node-created database
// that was not adopted) with every migration applied and unchanged.
func CheckSchema(ctx context.Context, q migrate.Queryer) error {
	pending, err := migrate.Status(ctx, q, Migrations())
	if errors.Is(err, migrate.ErrUnmanagedDatabase) {
		return fmt.Errorf("%w: the database has no migration ledger; adopt it with cmd/adopt --apply first", err)
	}
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		names := make([]string, len(pending))
		for i, descriptor := range pending {
			names[i] = fmt.Sprintf("%d %s", descriptor.Version, descriptor.Name)
		}
		return fmt.Errorf("%d migration(s) pending (%s); run cmd/migrate first", len(pending), strings.Join(names, ", "))
	}
	return nil
}

// Migrations explicitly collects capability-owned descriptors in global order.
func Migrations() []migrate.Descriptor {
	descriptors := editorial.Migrations()
	descriptors = append(descriptors, content.Migrations()...)
	descriptors = append(descriptors, newsroom.Migrations()...)
	descriptors = append(descriptors, media.Migrations()...)
	return append(descriptors, newsletter.Migrations()...)
}

func (a *App) Address() string       { return a.address }
func (a *App) Handler() http.Handler { return a.handler }

// Run serves HTTP and runs background tasks (the collector loop) until ctx is
// done, then waits for the tasks to stop. It also binds the feed collector's
// lifecycle to the tasks context, so a "Collect now" run started in the
// background (collector.Collector.Start) is cancelled and waited for too,
// instead of being abandoned mid-write on shutdown.
func (a *App) Run(ctx context.Context) error {
	return a.run(ctx, a.server.Run)
}

func (a *App) run(ctx context.Context, serve func(context.Context) error) error {
	tasksCtx, cancel := context.WithCancel(ctx)
	if a.newsletter != nil {
		// A digest outlives its HTTP request (like Node) but not shutdown.
		a.newsletter.Bind(ctx)
	}
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
	err := serve(ctx)
	cancel()
	wg.Wait()
	if a.feedCollector != nil {
		a.feedCollector.Wait()
	}
	if a.newsletter != nil {
		// The server has stopped and shutdown has cancelled any digest or
		// welcome email; wait (bounded) until each has recorded its outcome.
		waitCtx, stopWaiting := context.WithTimeout(context.Background(), newsletterDrainTimeout)
		if err := a.newsletter.Wait(waitCtx); err != nil {
			a.logger.Error("newsletter work still running at shutdown", "error", err)
		}
		stopWaiting()
	}
	for _, drain := range a.drains {
		drain()
	}
	return err
}
