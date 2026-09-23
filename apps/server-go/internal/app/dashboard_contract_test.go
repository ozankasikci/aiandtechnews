package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// dashboardContentOperations is every non-media dashboard operation in the
// canonical Node fixture, in canonical order. Media (27-29) replays in
// media_contract_test.go.
var dashboardContentOperations = []string{
	"dashboard.articles.list", "dashboard.articles.get", "dashboard.articles.create",
	"dashboard.articles.update", "dashboard.articles.delete",
	"dashboard.categories.list", "dashboard.categories.create",
	"dashboard.categories.update", "dashboard.categories.delete",
	"dashboard.settings.get", "dashboard.settings.update",
}

// seedContractSettings reproduces the synthetic Node capture database's
// settings (apps/server/scripts/contracts/synthetic-seed.ts).
//
// Deviation from the plan (reviewed contract change, user decision 2):
// GET /api/dashboard/settings hides newsroom.* keys, so the newsroom rows
// migration 3 seeds no longer need to be deleted to match the Node capture
// database, which never had them. They are left in place here to prove the
// replay is unaffected by their presence.
func seedContractSettings(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES
('site_name','Synthetic TechNews'),
('site_description','Synthetic contract fixture'),
('social_twitter','https://social.example.invalid/synthetic'),
('social_linkedin',''),
('social_github','https://code.example.invalid/synthetic'),
('newsletter_enabled','true'),
('newsletter_provider','recorder'),
('newsletter_webhook_url','https://hooks.example.invalid/newsletter')`); err != nil {
		t.Fatal(err)
	}
}

// dashboardApplication composes the app at the fixture's fixed clock over the
// synthetic capture data.
func dashboardApplication(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	handler, db, _ := dashboardApplicationWithUploads(t)
	return handler, db
}

// dashboardApplicationWithUploads also returns the temporary uploads directory.
func dashboardApplicationWithUploads(t *testing.T) (http.Handler, *sql.DB, string) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	seedContractSettings(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	uploads := t.TempDir()
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"), JWTSecret: authTestSecret, UploadsDir: uploads}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), db, uploads
}

// contractBindings logs in through the recorded auth.login operation and
// resolves $PASSWORD, $JWT and $AUTHORIZATION exactly like the auth replay.
func contractBindings(t *testing.T, handler http.Handler, contract *contracttest.Contract) map[string]string {
	t.Helper()
	passwordBinding, err := authBinding(contract.Replay.Bindings, "$PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	password, err := resolveAuthSecret(passwordBinding, map[string]string{"CONTRACT_TEST_PASSWORD": authTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	login, ok := contract.Operation("auth.login")
	if !ok {
		t.Fatal("operation auth.login missing")
	}
	resolvedLogin, err := resolveAuthOperation(login, map[string]string{"$PASSWORD": password})
	if err != nil {
		t.Fatal(err)
	}
	loginResponse := executeAuthOperation(t, handler, resolvedLogin)
	jwtBinding, err := authBinding(contract.Replay.Bindings, "$JWT")
	if err != nil {
		t.Fatal(err)
	}
	token, err := resolveAuthResponse(jwtBinding, login.OperationID, loginResponse.Body.Bytes())
	if err != nil {
		t.Fatalf("resolve $JWT: %v", err)
	}
	if err := verifyAuthBindingVector(jwtBinding, token); err != nil {
		t.Fatal(err)
	}
	bindings := map[string]string{"$PASSWORD": password, "$JWT": token}
	authorizationBinding, err := authBinding(contract.Replay.Bindings, "$AUTHORIZATION")
	if err != nil {
		t.Fatal(err)
	}
	if bindings["$AUTHORIZATION"], err = resolveAuthTemplate(authorizationBinding, bindings); err != nil {
		t.Fatal(err)
	}
	return bindings
}

func TestDashboardContentMatchesApprovedNodeContractSequence(t *testing.T) {
	handler, _ := dashboardApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}

	var fixtureOperations []string
	for _, operation := range contract.Operations {
		if strings.HasPrefix(operation.OperationID, "dashboard.") && !strings.HasPrefix(operation.OperationID, "dashboard.media.") {
			fixtureOperations = append(fixtureOperations, operation.OperationID)
		}
	}
	if !reflect.DeepEqual(fixtureOperations, dashboardContentOperations) {
		t.Fatalf("fixture dashboard operations = %v, want %v", fixtureOperations, dashboardContentOperations)
	}

	// Canonical state: articles.getBySlug increments view_count 42 -> 43 before
	// dashboard.articles.list is recorded. Newsletter (7-14) and media (27-29,
	// replayed by TestDashboardMediaMatchesApprovedNodeContractSequence)
	// operations touch no table these operations read, so they are skipped.
	previous := -1
	for _, id := range []string{"articles.list", "articles.trending", "articles.getBySlug", "articles.getById", "categories.list", "authors.list"} {
		op, _ := contract.Operation(id)
		previous = assertAfter(t, contract, id, previous)
		if err := contracttest.Replay(handler, op); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}
	bindings := contractBindings(t, handler, contract)
	previous = assertAfter(t, contract, "auth.login", previous)
	for _, id := range dashboardContentOperations {
		previous = assertAfter(t, contract, id, previous)
		op, _ := contract.Operation(id)
		resolved, err := resolveAuthOperation(op, bindings)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		// Dashboard replay errors print only response bodies, never the JWT.
		if err := contracttest.Replay(handler, resolved); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}
}

func assertAfter(t *testing.T, contract *contracttest.Contract, id string, previous int) int {
	t.Helper()
	index := operationIndex(contract.Operations, id)
	if index <= previous {
		t.Fatalf("operation %s missing or not in canonical order", id)
	}
	return index
}

func TestDashboardUnknownRouteMatchesRecordedObservation(t *testing.T) {
	handler, _ := dashboardApplication(t)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observations struct {
		DashboardUnknownRoute struct {
			Request struct {
				Method  string            `json:"method"`
				Path    string            `json:"path"`
				Headers map[string]string `json:"headers"`
			} `json:"request"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"dashboardUnknownRoute"`
	}
	if err := json.Unmarshal(contract.Observations, &observations); err != nil {
		t.Fatal(err)
	}
	observed := observations.DashboardUnknownRoute
	request := httptest.NewRequest(observed.Request.Method, observed.Request.Path, nil)
	for name, value := range observed.Request.Headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var got, want any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(observed.Body, &want); err != nil {
		t.Fatal(err)
	}
	if response.Code != observed.Status || !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown dashboard route = %d %s, want %d %s", response.Code, response.Body.String(), observed.Status, observed.Body)
	}
}
