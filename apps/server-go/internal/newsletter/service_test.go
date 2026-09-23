package newsletter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/testutil"
)

const testTokenSecret = "test-newsletter-token-secret-with-32-characters"

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func migratedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := testutil.OpenDatabase(t)
	if err := migrate.Run(context.Background(), db, Migrations()); err != nil {
		t.Fatal(err)
	}
	return db
}

func exec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()
	if _, err := db.Exec(statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func mustTime(t testing.TB, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// recordingSender records every email like the Node recorder's sendEmail
// stub and answers message-<n>, failing for the addresses in fail.
type recordingSender struct {
	mu   sync.Mutex
	sent []sentRecord
	fail map[string]error
}

type sentRecord struct {
	email Email
	key   string
}

func (r *recordingSender) Send(_ context.Context, email Email, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentRecord{email: email, key: key})
	if err := r.fail[email.To]; err != nil {
		return "", err
	}
	return "message-" + strconv.Itoa(len(r.sent)), nil
}

func newTestService(t *testing.T, db *sql.DB, sender Sender, cfg ServiceConfig) *Service {
	t.Helper()
	if cfg.SiteURL == "" {
		cfg.SiteURL = "https://aiandtech.news"
	}
	if cfg.Local == nil {
		cfg.Local = istanbulZone(t)
	}
	if cfg.Pace == nil {
		cfg.Pace = func(context.Context) error { return nil }
	}
	service, err := NewService(NewSQLiteStore(db), sender, cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// nodeText maps a string read back from the Node recording to the bytes
// Node stored: a lone surrogate better-sqlite3 wrote as WTF-8 (ED A0 BD)
// was read back as three U+FFFD characters.
func nodeText(value string) string {
	return strings.ReplaceAll(value, "\ufffd\ufffd\ufffd", "\xed\xa0\xbd")
}

func nullable(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func subscriberRows(t *testing.T, db *sql.DB) []subscriberRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, email, status, source_placement, confirmation_sent_at, confirmed_at, unsubscribed_at, created_at, updated_at FROM subscribers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []subscriberRow
	for rows.Next() {
		var row subscriberRow
		var placement, sent, confirmed, unsubscribed sql.NullString
		if err := rows.Scan(&row.ID, &row.Email, &row.Status, &placement, &sent, &confirmed, &unsubscribed, &row.CreatedAt, &row.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		row.SourcePlacement, row.ConfirmationSentAt, row.ConfirmedAt, row.UnsubscribedAt = nullable(placement), nullable(sent), nullable(confirmed), nullable(unsubscribed)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSubscriberRows(t *testing.T, got, want []subscriberRow) {
	t.Helper()
	for i := range want {
		if want[i].SourcePlacement != nil {
			text := nodeText(*want[i].SourcePlacement)
			want[i].SourcePlacement = &text
		}
	}
	if len(got) != len(want) {
		t.Fatalf("subscribers = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			gotJSON, _ := json.Marshal(got[i])
			wantJSON, _ := json.Marshal(want[i])
			t.Errorf("subscriber row %d =\n%s\nwant (Node)\n%s", i, gotJSON, wantJSON)
		}
	}
}

func TestSubscribeReplaysNodesSignupScenario(t *testing.T) {
	recorded := golden(t).Subscriptions
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordSubscriptions).
	exec(t, db, `INSERT INTO subscribers (id, email, status, confirmed_at, created_at, updated_at) VALUES (10, 'pending@example.com', 'pending', NULL, 'c', 'u')`)
	exec(t, db, `INSERT INTO subscribers (id, email, status, source_placement, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (11, 'gone@example.com', 'unsubscribed', 'old', '2026-01-01T00:00:00.000Z', '2026-02-01T00:00:00.000Z', 'c', 'u')`)
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (12, 'MixedCase@Example.com', 'active', 'c', 'u')`)
	service := newTestService(t, db, &recordingSender{fail: map[string]error{}}, ServiceConfig{})
	for _, outcome := range recorded.Outcomes {
		state, err := service.Subscribe(context.Background(), outcome.Address, outcome.Placement, mustTime(t, outcome.At))
		switch {
		case outcome.Error != nil:
			if !errors.Is(err, ErrInvalidEmail) || err.Error() != outcome.Error.Message {
				t.Errorf("Subscribe(%q) = %q, %v, want %s", outcome.Address, state, err, outcome.Error.Message)
			}
		case err != nil || string(state) != outcome.Result.State:
			t.Errorf("Subscribe(%q) = %q, %v, want %q", outcome.Address, state, err, outcome.Result.State)
		}
	}
	assertSubscriberRows(t, subscriberRows(t, db), recorded.Subscribers)
}

func TestNormalizeEmailMatchesNode(t *testing.T) {
	normalized := golden(t).Subscriptions.Normalized
	if len(normalized) < 20 {
		t.Fatalf("recorded email vectors = %d", len(normalized))
	}
	for _, vector := range normalized {
		got, ok := normalizeEmail(vector.Input)
		switch {
		case vector.Error != nil && ok:
			t.Errorf("normalizeEmail(%q) = %q, want %s", vector.Input, got, *vector.Error)
		case vector.Email != nil && (!ok || got != *vector.Email):
			t.Errorf("normalizeEmail(%q) = %q, %t, want %q", vector.Input, got, ok, *vector.Email)
		}
	}
}

func TestSubscribeRunsWithoutAnyEmailOrTokenSettings(t *testing.T) {
	db := migratedDatabase(t)
	sender := NewResendSender(ResendConfig{Endpoint: "http://127.0.0.1:1/never"})
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: ""})
	if state, err := service.Subscribe(context.Background(), "reader@example.com", "inline", time.Now()); err != nil || state != StateSubscribed {
		t.Fatalf("Subscribe = %q, %v", state, err)
	}
}

func TestConcurrentSignupsForOneAddressStoreOneRow(t *testing.T) {
	db := migratedDatabase(t)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Subscribe(context.Background(), "Same@Example.com", "inline", time.Now()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscribers`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows = %d, %v", count, err)
	}
}

func TestConfirmAndUnsubscribeReplayNodeWithNodeMintedTokens(t *testing.T) {
	recorded := golden(t).ConfirmAndUnsubscribe
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordConfirmAndUnsubscribe).
	for _, row := range []struct {
		id                          int64
		email, status               string
		confirmedAt, unsubscribedAt any
	}{
		{1, "pending@example.com", "pending", nil, nil},
		{2, "active@example.com", "active", "2026-01-01T00:00:00.000Z", nil},
		{3, "gone@example.com", "unsubscribed", "2026-01-01T00:00:00.000Z", "2026-02-01T00:00:00.000Z"},
		{4, "pending-fail@example.com", "pending", nil, nil},
	} {
		exec(t, db, `INSERT INTO subscribers (id, email, status, confirmed_at, unsubscribed_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'c', 'u')`,
			row.id, row.email, row.status, row.confirmedAt, row.unsubscribedAt)
	}
	sender := &recordingSender{fail: map[string]error{"pending-fail@example.com": errors.New("provider down")}}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	now := mustTime(t, recorded.Now)
	for _, vector := range recorded.Confirms {
		result, err := service.Confirm(context.Background(), vector.Token, now)
		if err != nil || string(result.State) != vector.Result.State || result.WelcomeSent != vector.Result.WelcomeSent {
			t.Errorf("confirm %s = %+v, %v, want %+v", vector.Name, result, err, vector.Result)
		}
	}
	for _, vector := range recorded.Unsubscribes {
		state, err := service.Unsubscribe(context.Background(), vector.Token, now)
		if err != nil || string(state) != vector.Result {
			t.Errorf("unsubscribe %s = %q, %v, want %q", vector.Name, state, err, vector.Result)
		}
	}
	if len(sender.sent) != len(recorded.Sent) {
		t.Fatalf("sent %d emails, want %d", len(sender.sent), len(recorded.Sent))
	}
	for i, want := range recorded.Sent {
		if sender.sent[i].key != want.IdempotencyKey {
			t.Errorf("welcome %d idempotency key = %q, want %q", i, sender.sent[i].key, want.IdempotencyKey)
		}
		assertEmail(t, "welcome "+want.To, sender.sent[i].email, want.nodeEmail)
	}
	assertSubscriberRows(t, subscriberRows(t, db), recorded.Subscribers)
}

func TestEditionsReplayNode(t *testing.T) {
	recorded := golden(t).Editions
	db := migratedDatabase(t)
	// The recorder's seed rows (record-node.ts, recordEditions).
	for _, row := range [][4]string{
		{"2026-09-17", "Object", `{"not":"array"}`, "2026-09-17T05:00:00.000Z"},
		{"2026-09-18", "Malformed", "{malformed", "2026-09-18T05:00:00.000Z"},
		{"2026-09-19", "Loose", ` [ {"title":"T","extra":[1,2],"readingMinutes":1.5}, 7, null ] `, "2026-09-19T05:00:00.000Z"},
		{"2026-09-20", "Typed", `[{"title":"<T&>","slug":"s","excerpt":"e","category":"c","readingMinutes":2}]`, "2026-09-20T05:00:00.000Z"},
		{"2026-09-16T", "Odd key", "[]", "x"},
	} {
		exec(t, db, `INSERT INTO newsletter_editions (edition_key, subject, articles, created_at) VALUES (?, ?, ?, ?)`, row[0], row[1], row[2], row[3])
	}
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{})
	for _, vector := range recorded.Lists {
		editions, err := service.ListEditions(context.Background(), float64(vector.Limit))
		if err != nil {
			t.Fatal(err)
		}
		assertSameJSON(t, "list "+strconv.Itoa(vector.Limit), editions, vector.Editions)
	}
	for _, vector := range recorded.Gets {
		edition, ok, err := service.Edition(context.Background(), vector.Key)
		if err != nil {
			t.Fatal(err)
		}
		if string(vector.Edition) == "null" {
			if ok {
				t.Errorf("edition %q found, Node: null", vector.Key)
			}
			continue
		}
		assertSameJSON(t, "edition "+vector.Key, edition, vector.Edition)
	}
	edition, _, _ := service.Edition(context.Background(), "2026-09-19")
	assertSameJSON(t, "JSON text", edition, json.RawMessage(recorded.JSONText))
}

// assertSameJSON compares the JSON encoding of got with Node's JSON,
// semantically (encoding/json escapes <, >, & where JSON.stringify does not).
func assertSameJSON(t *testing.T, name string, got, want any) {
	t.Helper()
	gotData, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	wantData, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	gotDecoder := json.NewDecoder(bytes.NewReader(gotData))
	gotDecoder.UseNumber()
	wantDecoder := json.NewDecoder(bytes.NewReader(wantData))
	wantDecoder.UseNumber()
	if gotDecoder.Decode(&gotValue) != nil || wantDecoder.Decode(&wantValue) != nil || !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("%s = %s\nwant (Node) %s", name, gotData, wantData)
	}
}

func TestSiteOriginMatchesNodesSafeSiteURL(t *testing.T) {
	for _, vector := range golden(t).SiteURLs {
		input := ""
		if vector.Input != nil {
			input = *vector.Input
		}
		got, err := SiteOrigin(input)
		switch {
		case input == "ftp://localhost":
			// Documented difference: Go accepts only http and https.
			if err == nil {
				t.Errorf("SiteOrigin(%q) = %q, want a configuration error", input, got)
			}
		case vector.Error != nil:
			if err == nil {
				t.Errorf("SiteOrigin(%q) = %q, want error %q", input, got, vector.Error.Message)
			}
			var configuration *ConfigurationError
			if !errors.As(err, &configuration) {
				t.Errorf("SiteOrigin(%q) error %v is not a configuration error", input, err)
			}
			if vector.Error.Configuration && err.Error() != vector.Error.Message {
				t.Errorf("SiteOrigin(%q) error = %q, want %q", input, err, vector.Error.Message)
			}
		default:
			if err != nil || got != *vector.SiteURL {
				t.Errorf("SiteOrigin(%q) = %q, %v, want %q", input, got, err, *vector.SiteURL)
			}
		}
	}
}

func TestTokenSecretRequirementMatchesNode(t *testing.T) {
	recorded := golden(t).TokenSecrets
	db := migratedDatabase(t)
	for _, vector := range recorded.Results {
		secret := ""
		if vector.Secret != nil {
			secret = *vector.Secret
		}
		service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: secret})
		state, err := service.Unsubscribe(context.Background(), "a.b", time.Now())
		if vector.Error != nil {
			var configuration *ConfigurationError
			if !errors.As(err, &configuration) || err.Error() != vector.Error.Message {
				t.Errorf("secret %q: Unsubscribe = %q, %v, want %q", secret, state, err, vector.Error.Message)
			}
			continue
		}
		if err != nil || string(state) != *vector.Unsubscribe {
			t.Errorf("secret %q: Unsubscribe = %q, %v, want %q", secret, state, err, *vector.Unsubscribe)
		}
	}
	// The trimmed secret is the HMAC key.
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'a@b.c', 'active', 'c', 'u')`)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: "  " + testTokenSecret + "  "})
	state, err := service.Unsubscribe(context.Background(), CreateToken(1, PurposeUnsubscribe, testTokenSecret, nil), mustTime(t, "2026-09-20T12:00:00.000Z"))
	if err != nil || string(state) != recorded.TrimmedKeyResult {
		t.Fatalf("padded secret: %q, %v, want %q", state, err, recorded.TrimmedKeyResult)
	}
}

func TestConfirmDoesNotSendTwiceForConcurrentClicks(t *testing.T) {
	db := migratedDatabase(t)
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'pending@example.com', 'pending', 'c', 'u')`)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	token := CreateToken(1, PurposeConfirm, testTokenSecret, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Confirm(context.Background(), token, time.Now()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(sender.sent) != 1 {
		t.Fatalf("welcome emails = %d, want 1", len(sender.sent))
	}
}

// Shutdown cancels a welcome email still in flight; the confirmation itself
// is already committed, so the answer is confirmed with welcomeSent false.
func TestShutdownCancelsTheWelcomeEmail(t *testing.T) {
	db := migratedDatabase(t)
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'pending@example.com', 'pending', 'c', 'u')`)
	lifecycle, stop := context.WithCancel(context.Background())
	sender := newBlockingSender()
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	service.Bind(lifecycle)
	done := make(chan ConfirmationResult, 1)
	go func() {
		result, err := service.Confirm(context.Background(), CreateToken(1, PurposeConfirm, testTokenSecret, nil), time.Now())
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	if key := <-sender.started; key != "newsletter-welcome-1" {
		t.Fatalf("key = %q", key)
	}
	stop()
	select {
	case result := <-done:
		if result.State != ConfirmationConfirmed || result.WelcomeSent {
			t.Fatalf("result = %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the welcome email ignored shutdown")
	}
	if err := service.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
