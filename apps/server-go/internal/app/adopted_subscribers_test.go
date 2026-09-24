package app_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/app"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/adopt"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/newsletter"
)

// adoptedLegacyOriginalDatabase builds the recorded legacy Node schema
// (internal/database/adopt/testdata) with the production subscribers table
// (created_at DATETIME DEFAULT CURRENT_TIMESTAMP, nullable; email TEXT
// UNIQUE NOT NULL), seeds subscribers, adopts it with cmd/adopt's Run, and
// opens it the way cmd/api does.
func adoptedLegacyOriginalDatabase(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	testdata := filepath.Join("..", "database", "adopt", "testdata")
	recorded, err := os.ReadFile(filepath.Join(testdata, "node-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	subscribersSQL, err := os.ReadFile(filepath.Join(testdata, "legacy-original-subscribers.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Legacy []struct {
			Name string `json:"name"`
			SQL  string `json:"sql"`
		} `json:"legacy"`
	}
	if err := json.Unmarshal(recorded, &schema); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "technews.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range schema.Legacy {
		statement := object.SQL
		if object.Name == "subscribers" {
			statement = string(subscribersSQL)
		}
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create %s: %v", object.Name, err)
		}
	}
	for _, statement := range []string{
		// Both Node formats, as in production: CURRENT_TIMESTAMP's
		// 'YYYY-MM-DD HH:MM:SS' and Node's later ISO strings.
		`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES
			(501, 'active@example.invalid', 'active', '2026-01-05 08:00:00', '2026-01-05 08:00:00'),
			(502, 'gone@example.invalid', 'unsubscribed', '2026-01-06 09:00:00', '2026-02-01T00:00:00.000Z')`,
		`INSERT INTO subscribers (id, email, status, source_placement, confirmation_sent_at, created_at, updated_at) VALUES
			(503, 'pending@example.invalid', 'pending', 'footer', '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z', '2026-09-18T00:00:00.000Z')`,
		// Node's original signup inserted only the email.
		`INSERT INTO subscribers (id, email) VALUES (504, 'default@example.invalid')`,
		`UPDATE subscribers SET status = 'active', updated_at = created_at WHERE id = 504`,
		// The column is nullable, so a NULL is possible even though
		// production has none: Go never reads it, so the row must still work.
		`INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (505, 'null@example.invalid', 'unsubscribed', NULL, NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	result, err := adopt.Run(ctx, adopt.Options{DatabasePath: path, Apply: true, Descriptors: app.Migrations(), Out: &out,
		Now: func() time.Time { return time.Date(2026, 9, 24, 7, 30, 15, 0, time.UTC) }})
	if err != nil || result.Outcome != adopt.OutcomeAdopted {
		t.Fatalf("adopt: Outcome = %v, err = %v\n%s", result.Outcome, err, out.String())
	}
	db, err = database.OpenExisting(ctx, path, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := app.CheckSchema(ctx, db); err != nil {
		t.Fatalf("CheckSchema after adoption: %v", err)
	}
	return db
}

type subscriberRow struct {
	Email, Status                          string
	CreatedAt, CreatedAtType, UpdatedAt    sql.NullString
	ConfirmedAt, UnsubscribedAt, Placement sql.NullString
}

func subscriberRows(t *testing.T, db *sql.DB) map[int64]subscriberRow {
	t.Helper()
	// modernc.org/sqlite converts a DATETIME-declared column's text that
	// looks like a time into time.Time, which scans back as RFC 3339
	// ('2026-09-18 00:00:00' -> '2026-09-18T00:00:00Z'); the CAST reads the
	// stored text as it is.
	rows, err := db.Query(`SELECT id, email, status, CAST(created_at AS TEXT), typeof(created_at), updated_at, confirmed_at, unsubscribed_at, source_placement FROM subscribers ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64]subscriberRow{}
	for rows.Next() {
		var id int64
		var row subscriberRow
		if err := rows.Scan(&id, &row.Email, &row.Status, &row.CreatedAt, &row.CreatedAtType, &row.UpdatedAt, &row.ConfirmedAt, &row.UnsubscribedAt, &row.Placement); err != nil {
			t.Fatal(err)
		}
		got[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNewsletterFlowsWorkOnAnAdoptedLegacyOriginalSubscribersTable(t *testing.T) {
	db := adoptedLegacyOriginalDatabase(t)
	handler, resend := newsletterApplicationOn(t, db, nil)
	before := subscriberRows(t, db)
	if got := before[504]; !got.CreatedAt.Valid || len(got.CreatedAt.String) != len("2006-01-02 15:04:05") || got.CreatedAtType.String != "text" {
		t.Fatalf("test setup: CURRENT_TIMESTAMP default gave %+v", got)
	}
	// Why no Go code may read subscribers.created_at without a CAST on this
	// table: the driver rewrites both Node formats (see subscriberRows).
	for id, want := range map[int64]string{501: "2026-01-05T08:00:00Z", 503: "2026-09-18T00:00:00Z"} {
		var raw string
		if err := db.QueryRow(`SELECT created_at FROM subscribers WHERE id = ?`, id).Scan(&raw); err != nil || raw != want {
			t.Errorf("driver read of subscriber %d created_at = %q, %v; want the driver's %q", id, raw, err, want)
		}
	}

	expect := func(response interface {
		Result() *http.Response
	}, status int, body string) {
		t.Helper()
		result := response.Result()
		var buffer bytes.Buffer
		_, _ = buffer.ReadFrom(result.Body)
		if result.StatusCode != status || strings.TrimSpace(buffer.String()) != body {
			t.Fatalf("response = %d %s, want %d %s", result.StatusCode, buffer.String(), status, body)
		}
	}
	token := func(id int64, purpose newsletter.Purpose) string {
		return url.QueryEscape(newsletter.CreateToken(id, purpose, contractNewsletterSecret, nil))
	}

	// Subscribe: a new address (Go writes created_at), an unsubscribed
	// legacy row, the NULL created_at row, and an already active one.
	expect(request(t, handler, http.MethodPost, "/api/subscribe", `{"email":"New@Example.invalid","placement":"footer"}`, ""),
		http.StatusOK, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	expect(request(t, handler, http.MethodPost, "/api/subscribe", `{"email":"gone@example.invalid","placement":"hero"}`, ""),
		http.StatusOK, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	expect(request(t, handler, http.MethodPost, "/api/subscribe", `{"email":"null@example.invalid","placement":"hero"}`, ""),
		http.StatusOK, `{"success":true,"state":"subscribed","message":"You're subscribed. The next digest will arrive in your inbox."}`)
	expect(request(t, handler, http.MethodPost, "/api/subscribe", `{"email":"active@example.invalid"}`, ""),
		http.StatusOK, `{"success":true,"state":"already_active","message":"You're already subscribed."}`)
	// Confirm the pending subscriber (sends the welcome email), then again.
	expect(request(t, handler, http.MethodGet, "/api/newsletter/confirm?token="+token(503, newsletter.PurposeConfirm), "", ""),
		http.StatusOK, `{"state":"confirmed","welcomeSent":true}`)
	expect(request(t, handler, http.MethodGet, "/api/newsletter/confirm?token="+token(503, newsletter.PurposeConfirm), "", ""),
		http.StatusOK, `{"state":"already_confirmed","welcomeSent":false}`)
	// Unsubscribe the default-created row (GET link) and an ISO row (POST).
	expect(request(t, handler, http.MethodGet, "/api/newsletter/unsubscribe?token="+token(504, newsletter.PurposeUnsubscribe), "", ""),
		http.StatusOK, `{"state":"unsubscribed"}`)
	expect(request(t, handler, http.MethodPost, "/api/newsletter/unsubscribe", `{"token":"`+newsletter.CreateToken(501, newsletter.PurposeUnsubscribe, contractNewsletterSecret, nil)+`"}`, ""),
		http.StatusOK, `{"state":"unsubscribed"}`)

	if emails := resend.snapshot(); len(emails) != 1 || !reflect.DeepEqual(emails[0].Body.To, []string{"pending@example.invalid"}) || emails[0].IdempotencyKey != "newsletter-welcome-503" {
		t.Fatalf("emails = %+v, want one welcome email to subscriber 503", emails)
	}

	const now = "2026-09-20T12:00:00.000Z" // newsletterApplicationOn's fixed clock
	after := subscriberRows(t, db)
	var newID int64
	for id, row := range after {
		if row.Email == "new@example.invalid" {
			newID = id
		}
	}
	// A Go INSERT stores its ISO created_at as TEXT despite DATETIME's
	// NUMERIC affinity.
	if got := after[newID]; got.Status != "active" || got.CreatedAt.String != now || got.CreatedAtType.String != "text" || got.UpdatedAt.String != now || got.Placement.String != "footer" {
		t.Errorf("new subscriber = %+v", got)
	}
	// Updates never touch created_at, whatever its format (or NULL).
	for id := range before {
		if after[id].CreatedAt != before[id].CreatedAt || after[id].CreatedAtType != before[id].CreatedAtType {
			t.Errorf("subscriber %d created_at changed from %+v to %+v", id, before[id].CreatedAt, after[id].CreatedAt)
		}
	}
	for id, want := range map[int64]string{501: "unsubscribed", 502: "active", 503: "active", 504: "unsubscribed", 505: "active"} {
		if got := after[id]; got.Status != want || got.UpdatedAt.String != now {
			t.Errorf("subscriber %d = %+v, want status %s updated at %s", id, got, want, now)
		}
	}
	if after[503].ConfirmedAt.String != now || after[504].UnsubscribedAt.String != now || after[502].UnsubscribedAt.Valid {
		t.Errorf("confirm/unsubscribe timestamps: 503 %+v, 504 %+v, 502 %+v", after[503], after[504], after[502])
	}
}
