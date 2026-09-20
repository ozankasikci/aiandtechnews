package app_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

func TestArticlesMatchApprovedNodeContractsInCaptureOrder(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db")}
	application, err := app.NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"articles.list", "articles.trending", "articles.getBySlug", "articles.getById"} {
		op, ok := contract.Operation(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if err := contracttest.Replay(application.Handler(), op); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}
}

func seedContractArticles(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}) {
	t.Helper()
	statements := []string{
		`INSERT INTO categories(id,name,slug,description,color) VALUES
(101,'Synthetic AI','synthetic-ai','Synthetic artificial intelligence fixtures','#111111'),
(102,'Synthetic Code','synthetic-code','Synthetic programming fixtures','#222222'),
(103,'Synthetic Startups','synthetic-startups','Synthetic startup fixtures','#333333'),
(104,'Synthetic Unused','synthetic-unused','Intentionally unused contract category','#444444')`,
		`INSERT INTO authors(id,name,email,password_hash,avatar,bio,role) VALUES
(201,'TechNews Editorial','editorial@example.invalid','fake','/uploads/synthetic-contract-image.png','Synthetic editorial contract fixture.','admin'),
(202,'Synthetic Reporter','reporter@example.invalid','fake',NULL,NULL,'editor')`,
		`INSERT INTO articles(id,title,slug,excerpt,content,featured_image,category_id,author_id,status,published_at,meta_title,meta_description,source,source_url,view_count,created_at,updated_at) VALUES
(301,'Synthetic Published Newer','synthetic-published-newer','Newer synthetic excerpt','<p>Entirely synthetic contract article content.</p>','/uploads/synthetic-contract-image.png',101,201,'published','2026-09-19T12:00:00.000Z','Synthetic meta title','Synthetic meta description','Synthetic Wire','https://news.example.invalid/newer',42,'2026-09-19T10:00:00.000Z','2026-09-19T12:00:00.000Z'),
(302,'Synthetic Published Null Options','synthetic-published-null-options','Null option excerpt','Synthetic plain text.',NULL,102,202,'published','2026-09-18 09:00:00',NULL,NULL,NULL,NULL,0,'2026-09-18 08:00:00','2026-09-18 09:00:00'),
(303,'Synthetic Draft','synthetic-draft','Draft excerpt','<p>Synthetic draft.</p>',NULL,103,201,'draft',NULL,NULL,NULL,'Synthetic Wire','https://news.example.invalid/draft',3,'2026-09-17T00:00:00.000Z','2026-09-17T00:00:00.000Z'),
(304,'Synthetic Scheduled','synthetic-scheduled','Scheduled excerpt','<p>Synthetic scheduled.</p>',NULL,101,202,'scheduled','2030-01-01T00:00:00.000Z',NULL,NULL,'Synthetic Wire','https://news.example.invalid/scheduled',1,'2026-09-16T00:00:00.000Z','2026-09-16T00:00:00.000Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
