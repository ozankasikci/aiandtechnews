package app_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/config"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/contracttest"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

// The synthetic Node capture's newsletter settings
// (apps/server/scripts/contract-server.ts:17-19, 171-176, 184).
const (
	contractNewsletterSecret = "synthetic-contract-newsletter-secret-000000000000"
	contractCronSecret       = "synthetic-contract-cron-secret-never-production"
	contractResendKey        = "re_synthetic_contract_key_never_production"
)

// newsletterOperations is every newsletter operation in the canonical Node
// fixture, in canonical order.
var newsletterOperations = []string{
	"newsletter.subscribe", "newsletter.confirm", "newsletter.unsubscribeGet", "newsletter.unsubscribePost",
	"newsletter.editions", "newsletter.edition", "newsletter.digestGet", "newsletter.digestPost",
}

// seedContractNewsletter reproduces the synthetic Node capture's subscribers
// and editions (apps/server/scripts/contracts/synthetic-seed.ts:64-84).
func seedContractNewsletter(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO subscribers (id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES
(501, 'pending@example.invalid', 'pending', 'contract', '2026-09-18T00:00:00.000Z', NULL, NULL, '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z'),
(502, 'active@example.invalid', 'active', 'contract', NULL, '2026-09-18T01:00:00.000Z', NULL, '2026-09-18T00:00:00.000Z', '2026-09-18T01:00:00.000Z'),
(503, 'unsubscribed@example.invalid', 'unsubscribed', 'contract', NULL, '2026-09-17T01:00:00.000Z', '2026-09-18T02:00:00.000Z', '2026-09-17T00:00:00.000Z', '2026-09-18T02:00:00.000Z')`,
		`INSERT INTO newsletter_editions (id, edition_key, subject, articles, created_at) VALUES
(601, '2026-09-19', 'Synthetic daily digest', '[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}]', '2026-09-19T13:00:00.000Z'),
(602, '2026-09-18', 'Synthetic malformed digest', '{malformed', '2026-09-18T13:00:00.000Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeResend is an httptest stand-in for the Resend API that records each
// email and answers like the Node capture's recorder (synthetic-email-<n>).
type fakeResend struct {
	mu       sync.Mutex
	requests []resendRequest
}

type resendRequest struct {
	Authorization  string
	IdempotencyKey string
	Body           struct {
		From    string            `json:"from"`
		To      []string          `json:"to"`
		Subject string            `json:"subject"`
		HTML    string            `json:"html"`
		Text    string            `json:"text"`
		ReplyTo string            `json:"reply_to"`
		Headers map[string]string `json:"headers"`
		Tags    []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"tags"`
	}
}

func (f *fakeResend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request resendRequest
	request.Authorization = r.Header.Get("Authorization")
	request.IdempotencyKey = r.Header.Get("Idempotency-Key")
	data, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(data, &request.Body)
	f.mu.Lock()
	f.requests = append(f.requests, request)
	count := len(f.requests)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"synthetic-email-`+strconv.Itoa(count)+`"}`)
}

func (f *fakeResend) snapshot() []resendRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]resendRequest(nil), f.requests...)
}

func newsletterApplication(t *testing.T, mutate func(*config.Config)) (http.Handler, *sql.DB, *fakeResend) {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	seedContractNewsletter(t, db)
	handler, resend := newsletterApplicationOn(t, db, mutate)
	return handler, db, resend
}

// newsletterApplicationOn composes the application with the contract's
// newsletter settings over db, with delivery going to a fake Resend.
func newsletterApplicationOn(t *testing.T, db *sql.DB, mutate func(*config.Config)) (http.Handler, *fakeResend) {
	t.Helper()
	resend := &fakeResend{}
	server := httptest.NewServer(resend)
	t.Cleanup(server.Close)
	app.StubNewsletterDeliveryForTest(t, server.URL+"/emails", server.Client(), func(context.Context) error { return nil })

	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		JWTSecret: authTestSecret, UploadsDir: t.TempDir(),
		NewsletterSiteURL: "https://site.example.invalid", NewsletterTokenSecret: contractNewsletterSecret,
		NewsletterCronSecret: contractCronSecret, ResendAPIKey: contractResendKey,
		NewsletterFrom: "Synthetic Contract <newsletter@example.invalid>", NewsletterReplyTo: "reply@example.invalid"}
	if mutate != nil {
		mutate(&cfg)
	}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application.Handler(), resend
}

// resolveNewsletterBindings mints the fixture's newsletter tokens with the Go
// implementation and checks each against Node's recorded SHA-256 and
// payload: links Node signed must be the links Go signs.
func resolveNewsletterBindings(t *testing.T, contract *contracttest.Contract) map[string]string {
	t.Helper()
	secrets := map[string]string{"NEWSLETTER_TOKEN_SECRET": contractNewsletterSecret, "CRON_SECRET": contractCronSecret}
	values := map[string]string{}
	for _, binding := range contract.Replay.Bindings {
		resolver := binding.Resolver
		switch resolver.Type {
		case "newsletterToken":
			secret, ok := secrets[resolver.SecretRef]
			if !ok {
				t.Fatalf("binding %s references an unavailable test secret", binding.Placeholder)
			}
			expiresAt, err := time.Parse(time.RFC3339Nano, resolver.ExpiresAt)
			if err != nil {
				t.Fatal(err)
			}
			token := newsletter.CreateToken(int64(resolver.SubscriberID), newsletter.Purpose(resolver.Purpose), secret, &expiresAt)
			var vector struct {
				Payload json.RawMessage `json:"payload"`
				SHA256  string          `json:"sha256"`
			}
			if err := json.Unmarshal(binding.Vector, &vector); err != nil || vector.SHA256 == "" {
				t.Fatalf("binding %s has no vector", binding.Placeholder)
			}
			digest := sha256.Sum256([]byte(token))
			if hex.EncodeToString(digest[:]) != vector.SHA256 {
				t.Fatalf("binding %s: the Go token's SHA-256 does not match Node's recorded vector", binding.Placeholder)
			}
			payload, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[0])
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if json.Unmarshal(payload, &got) != nil || json.Unmarshal(vector.Payload, &want) != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("binding %s payload = %s, want %s", binding.Placeholder, payload, vector.Payload)
			}
			values[binding.Placeholder] = token
		case "secretRefTemplate":
			secret, ok := secrets[resolver.SecretRef]
			if !ok {
				t.Fatalf("binding %s references an unavailable test secret", binding.Placeholder)
			}
			values[binding.Placeholder] = strings.ReplaceAll(resolver.Value, "${secret}", secret)
		}
	}
	for _, placeholder := range []string{"$NEWSLETTER_CONFIRM_TOKEN", "$NEWSLETTER_UNSUBSCRIBE_ACTIVE_TOKEN", "$NEWSLETTER_UNSUBSCRIBE_OLD_TOKEN", "$CRON_AUTHORIZATION"} {
		if values[placeholder] == "" {
			t.Fatalf("binding %s was not resolved", placeholder)
		}
	}
	return values
}

func TestNewsletterMatchesApprovedNodeContractSequence(t *testing.T) {
	handler, db, resend := newsletterApplication(t, nil)
	contract, err := contracttest.Load(filepath.Join("..", "..", "contracts", "fixtures", "node-contracts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtureOperations []string
	for _, operation := range contract.Operations {
		if strings.HasPrefix(operation.OperationID, "newsletter.") {
			fixtureOperations = append(fixtureOperations, operation.OperationID)
		}
	}
	if !reflect.DeepEqual(fixtureOperations, newsletterOperations) {
		t.Fatalf("fixture newsletter operations = %v, want %v", fixtureOperations, newsletterOperations)
	}
	if !contract.FixedClock.Equal(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("fixed clock = %v", contract.FixedClock)
	}
	bindings := resolveNewsletterBindings(t, contract)
	previous := -1
	for _, id := range newsletterOperations {
		previous = assertAfter(t, contract, id, previous)
		operation, _ := contract.Operation(id)
		resolved, err := resolveAuthOperation(operation, bindings)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		// Replay errors print response bodies only, never tokens or secrets.
		if err := contracttest.Replay(handler, resolved); err != nil {
			t.Fatalf("%s replay: %v", id, err)
		}
	}

	// The capture's email effects: the welcome email for the confirmed
	// pending subscriber, then one digest to each active subscriber (the
	// confirmed 501 and the newly subscribed 504); digestPost skips both.
	requests := resend.snapshot()
	var got []string
	for _, request := range requests {
		got = append(got, strings.Join(request.Body.To, ",")+" "+request.IdempotencyKey+" "+request.Body.Subject)
		if request.Authorization != "Bearer "+contractResendKey || request.Body.From != "Synthetic Contract <newsletter@example.invalid>" || request.Body.ReplyTo != "reply@example.invalid" {
			t.Errorf("request %s: authorization matches = %t, from = %q, reply_to = %q", request.IdempotencyKey, request.Authorization == "Bearer "+contractResendKey, request.Body.From, request.Body.ReplyTo)
		}
		if unsubscribe := request.Body.Headers["List-Unsubscribe"]; !strings.HasPrefix(unsubscribe, "<https://site.example.invalid/api/newsletter/unsubscribe?token=") {
			t.Errorf("request %s List-Unsubscribe = %q", request.IdempotencyKey, unsubscribe)
		}
	}
	want := []string{
		"pending@example.invalid newsletter-welcome-501 Welcome to AI & Tech News",
		"pending@example.invalid newsletter-digest-2026-09-20-501 Synthetic Published Newer",
		"capture@example.invalid newsletter-digest-2026-09-20-504 Synthetic Published Newer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("emails = %q, want %q", got, want)
	}
	var sent int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_deliveries WHERE edition_key = '2026-09-20' AND status = 'sent'`).Scan(&sent); err != nil || sent != 2 {
		t.Fatalf("sent deliveries = %d, %v", sent, err)
	}
	var subject, articles string
	if err := db.QueryRow(`SELECT subject, articles FROM newsletter_editions WHERE edition_key = '2026-09-20'`).Scan(&subject, &articles); err != nil {
		t.Fatal(err)
	}
	if subject != "Synthetic Published Newer" || articles != `[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}]` {
		t.Fatalf("edition = %q %s", subject, articles)
	}
}

func TestNewsletterSignupWorksWithoutAnyNewsletterSettings(t *testing.T) {
	handler, _, resend := newsletterApplication(t, func(cfg *config.Config) {
		cfg.NewsletterSiteURL, cfg.NewsletterTokenSecret, cfg.NewsletterCronSecret = "", "", ""
		cfg.ResendAPIKey, cfg.NewsletterFrom, cfg.NewsletterReplyTo = "", "", ""
	})
	assertAuthResponse(t, request(t, handler, "POST", "/api/subscribe", `{"email":"new@example.invalid","placement":"footer"}`, ""),
		200, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/unsubscribe?token=a.b", "", ""),
		503, `{"state":"unavailable","error":"Unsubscribe is temporarily unavailable"}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/confirm?token=a.b", "", ""),
		503, `{"state":"unavailable","error":"Newsletter confirmation is temporarily unavailable"}`)
	// No cron secret: the digest refuses everything.
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/digest", "", "Bearer "),
		401, `{"error":"Unauthorized"}`)
	if len(resend.snapshot()) != 0 {
		t.Fatal("an unconfigured newsletter reached the email provider")
	}
}

func TestNewsletterRoutesNeedNoDashboardLogin(t *testing.T) {
	handler, _, _ := newsletterApplication(t, nil)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/editions?limit=1", "", ""), 200,
		`{"editions":[{"edition":"2026-09-19","subject":"Synthetic daily digest","articles":[{"title":"Synthetic Published Newer","slug":"synthetic-published-newer","excerpt":"Newer synthetic excerpt","category":"Synthetic AI","readingMinutes":1}],"createdAt":"2026-09-19T13:00:00.000Z"}]}`)
	assertAuthResponse(t, request(t, handler, "GET", "/api/newsletter/editions/2026-09-17", "", ""), 404, `{"error":"Edition not found"}`)
	assertAuthResponse(t, request(t, handler, "POST", "/api/newsletter/digest", "", "Bearer wrong"), 401, `{"error":"Unauthorized"}`)
}

func TestCompositionFailsOnAnInvalidNewsletterSiteURL(t *testing.T) {
	db, _ := testutil.OpenDatabase(t)
	for _, siteURL := range []string{"http://example.com", "not a url"} {
		cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
			JWTSecret: authTestSecret, UploadsDir: t.TempDir(), NewsletterSiteURL: siteURL}
		if _, err := app.NewWithDatabase(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db); err == nil || !strings.Contains(err.Error(), "NEWSLETTER_SITE_URL") {
			t.Errorf("NEWSLETTER_SITE_URL=%q: composition error = %v", siteURL, err)
		}
	}
}

// App.Run waits (bounded) for an in-flight digest after the HTTP server
// stops, so the outcome of the delivery being made is recorded before exit.
func TestRunWaitsForAnInFlightDigestAfterTheServerStops(t *testing.T) {
	var paceReturned atomic.Bool
	paused := make(chan struct{})
	var once sync.Once
	resend := &fakeResend{}
	server := httptest.NewServer(resend)
	t.Cleanup(server.Close)
	application, db := newsletterAppWithPace(t, server, func(context.Context) error {
		once.Do(func() { close(paused) })
		time.Sleep(300 * time.Millisecond) // ignores cancellation on purpose
		paceReturned.Store(true)
		return nil
	})
	if _, err := db.Exec(`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (505, 'second@example.invalid', 'active', 'c', 'u')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err := app.RunWithServeForTest(application, ctx, func(context.Context) error {
		go request(t, application.Handler(), "POST", "/api/newsletter/digest", "", "Bearer "+contractCronSecret)
		<-paused
		cancel() // shutdown begins while the digest pauses between deliveries
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !paceReturned.Load() {
		t.Fatal("Run returned while the digest was still running")
	}
	var sending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM newsletter_deliveries WHERE status = 'sending'`).Scan(&sending); err != nil || sending != 0 {
		t.Fatalf("deliveries left sending = %d, %v", sending, err)
	}
}

func newsletterAppWithPace(t *testing.T, server *httptest.Server, pace func(context.Context) error) (*app.App, *sql.DB) {
	t.Helper()
	app.StubNewsletterDeliveryForTest(t, server.URL+"/emails", server.Client(), pace)
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, app.Migrations()); err != nil {
		t.Fatal(err)
	}
	seedContractArticles(t, db)
	seedContractNewsletter(t, db)
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cfg := config.Config{Mode: config.ModeDevelopment, Address: "127.0.0.1:4402", DatabasePath: filepath.Join(t.TempDir(), "unused.db"),
		JWTSecret: authTestSecret, UploadsDir: t.TempDir(), NewsletterTokenSecret: contractNewsletterSecret,
		NewsletterCronSecret: contractCronSecret, ResendAPIKey: contractResendKey, NewsletterFrom: "Synthetic Contract <newsletter@example.invalid>"}
	application, err := app.NewWithDatabaseAt(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), db, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	return application, db
}
