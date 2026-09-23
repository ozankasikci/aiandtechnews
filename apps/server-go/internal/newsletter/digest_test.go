package newsletter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// createContentTables creates Node's categories, authors, and articles
// tables (apps/server/src/db.ts:18-56); the newsletter only reads them.
func createContentTables(t *testing.T, db *sql.DB) {
	t.Helper()
	exec(t, db, `CREATE TABLE categories (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      description TEXT NOT NULL DEFAULT '',
      color TEXT NOT NULL DEFAULT '#6366f1'
    )`)
	exec(t, db, `CREATE TABLE authors (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      name TEXT NOT NULL,
      email TEXT NOT NULL UNIQUE,
      password_hash TEXT NOT NULL,
      avatar TEXT,
      bio TEXT,
      role TEXT NOT NULL DEFAULT 'editor' CHECK(role IN ('admin', 'editor'))
    )`)
	exec(t, db, `CREATE TABLE articles (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      title TEXT NOT NULL,
      slug TEXT NOT NULL UNIQUE,
      excerpt TEXT NOT NULL DEFAULT '',
      content TEXT NOT NULL DEFAULT '',
      featured_image TEXT,
      category_id INTEGER NOT NULL,
      author_id INTEGER NOT NULL,
      status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft', 'published', 'scheduled')),
      published_at TEXT,
      meta_title TEXT,
      meta_description TEXT,
      source TEXT,
      source_url TEXT,
      view_count INTEGER NOT NULL DEFAULT 0,
      created_at TEXT NOT NULL DEFAULT (datetime('now')),
      updated_at TEXT NOT NULL DEFAULT (datetime('now')),
      FOREIGN KEY (category_id) REFERENCES categories(id),
      FOREIGN KEY (author_id) REFERENCES authors(id)
    )`)
	exec(t, db, `INSERT INTO authors (id, name, email, password_hash) VALUES (1, 'A', 'a@example.invalid', 'x')`)
}

func words(count int) string {
	return strings.TrimSuffix(strings.Repeat("word ", count), " ")
}

func insertArticle(t *testing.T, db *sql.DB, title, slug, excerpt, content string, category int, status string, publishedAt any) {
	t.Helper()
	exec(t, db, `INSERT INTO articles (title, slug, excerpt, content, category_id, author_id, status, published_at) VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		title, slug, excerpt, content, category, status, publishedAt)
}

// countingPace records how often the digest paused between deliveries.
type countingPace struct{ calls atomic.Int64 }

func (c *countingPace) pace(context.Context) error { c.calls.Add(1); return nil }

func (c *countingPace) delays() []int {
	delays := make([]int, c.calls.Load())
	for i := range delays {
		delays[i] = int(DefaultPacing / time.Millisecond)
	}
	return delays
}

func editionRows(t *testing.T, db *sql.DB) []editionRow {
	t.Helper()
	rows, err := db.Query(`SELECT id, edition_key, subject, articles, created_at FROM newsletter_editions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []editionRow
	for rows.Next() {
		var row editionRow
		if err := rows.Scan(&row.ID, &row.EditionKey, &row.Subject, &row.Articles, &row.CreatedAt); err != nil {
			t.Fatal(err)
		}
		result = append(result, row)
	}
	return result
}

func deliveryRows(t *testing.T, db *sql.DB) []deliveryRow {
	t.Helper()
	rows, err := db.Query(`SELECT subscriber_id, edition_key, status, provider_message_id, error, created_at, sent_at FROM newsletter_deliveries ORDER BY subscriber_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []deliveryRow
	for rows.Next() {
		var row deliveryRow
		var provider, message, sentAt sql.NullString
		if err := rows.Scan(&row.SubscriberID, &row.EditionKey, &row.Status, &provider, &message, &row.CreatedAt, &sentAt); err != nil {
			t.Fatal(err)
		}
		row.ProviderMessageID, row.Error, row.SentAt = nullable(provider), nullable(message), nullable(sentAt)
		result = append(result, row)
	}
	return result
}

func assertDeliveryRows(t *testing.T, name string, got, want []deliveryRow) {
	t.Helper()
	for i := range want {
		if want[i].Error != nil {
			text := nodeText(*want[i].Error)
			want[i].Error = &text
		}
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(want)
		t.Errorf("%s deliveries =\n%s\nwant (Node)\n%s", name, gotJSON, wantJSON)
	}
}

func assertResult(t *testing.T, name string, got DigestResult, want goldenDigestResult) {
	t.Helper()
	if got.Edition != want.Edition || got.Articles != want.Articles || got.Sent != want.Sent || got.Skipped != want.Skipped || got.Failed != want.Failed {
		t.Errorf("%s result = %+v, want %+v", name, got, want)
	}
}

func assertEditions(t *testing.T, got, want []editionRow, compareIDs bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("editions = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !compareIDs {
			got[i].ID = want[i].ID
		}
		if got[i] != want[i] {
			t.Errorf("edition row %d =\n%+v\nwant (Node, byte for byte)\n%+v", i, got[i], want[i])
		}
	}
}

func assertSent(t *testing.T, got []sentRecord, want []sentEmail) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sent %d emails, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].key != want[i].IdempotencyKey || got[i].email.To != want[i].To {
			t.Errorf("email %d = %s %s, want %s %s", i, got[i].email.To, got[i].key, want[i].To, want[i].IdempotencyKey)
		}
		if want[i].HTML != "" {
			expected := nodeEmail{To: want[i].To, Subject: want[i].Subject, HTML: want[i].HTML, Text: want[i].Text, Headers: want[i].Headers, Tags: want[i].Tags}
			if expected.Headers == nil && expected.Tags == nil {
				// This recording kept the rendered content only.
				expected.Headers, expected.Tags = headerMap(got[i].email.Headers), tagsOf(got[i].email.Tags)
			}
			assertEmail(t, "digest to "+want[i].To, got[i].email, expected)
		}
	}
}

func TestDigestSelectsArticlesLikeNode(t *testing.T) {
	recorded := golden(t).DigestSelection
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Programming', 'programming')`)
	for _, raw := range recorded.Articles {
		var article []json.RawMessage
		if err := json.Unmarshal(raw, &article); err != nil {
			t.Fatal(err)
		}
		var slug, status string
		var publishedAt *string
		var category, count int
		for i, target := range []any{&slug, &status, &publishedAt, &category, &count} {
			if err := json.Unmarshal(article[i], target); err != nil {
				t.Fatal(err)
			}
		}
		insertArticle(t, db, "Title "+slug, slug, "Excerpt "+slug, "<p>"+words(count)+"</p>", category, status, publishedAt)
	}
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (1, 'one@example.com', 'active', 'x', 'x')`)
	sender := &recordingSender{}
	pace := &countingPace{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: pace.pace})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, recorded.Now))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "selection", result, recorded.Result)
	assertEditions(t, editionRows(t, db), recorded.Editions, true)
	assertDeliveryRows(t, "selection", deliveryRows(t, db), recorded.Delivery)
	assertSent(t, sender.sent, recorded.Sent)
	if !reflect.DeepEqual(pace.delays(), append([]int{}, recorded.Delays...)) {
		t.Errorf("pauses = %v, want %v", pace.delays(), recorded.Delays)
	}
}

func TestDigestOnlyConsidersTheTwentyNewestPublishedAtTextsLikeNode(t *testing.T) {
	recorded := golden(t).DigestWindow
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	for i := 0; i < 20; i++ {
		insertArticle(t, db, "Future "+strconv.Itoa(i), "future-"+strconv.Itoa(i), "x", "", 1, "published", "2027-01-"+strconv.Itoa(101 + i)[1:]+"T00:00:00.000Z")
	}
	insertArticle(t, db, "Recent", "recent", "x", "", 1, "published", "2026-09-20T04:00:00.000Z")
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "window", result, recorded.Result)
	if rows := editionRows(t, db); len(rows) != 0 {
		t.Errorf("an empty digest recorded an edition: %+v", rows)
	}
}

func TestDigestTakesTheFiveNewestAndRendersThemLikeNode(t *testing.T) {
	recorded := golden(t).DigestTopFive
	db := migratedDatabase(t)
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai'), (2, 'Code', 'code')`)
	for index, stamp := range recorded.Stamps {
		insertArticle(t, db, "Story "+strconv.Itoa(index), "story-"+strconv.Itoa(index), "Excerpt "+strconv.Itoa(index), "<p>"+words(index*150)+"</p>", index%2+1, "published", stamp)
	}
	exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (3, 'three@example.com', 'active', 'x', 'x')`)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{SiteURL: "https://aiandtech.news/some/path?q=1", TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "top five", result, recorded.Result)
	assertEditions(t, editionRows(t, db), recorded.Editions, false)
	assertSent(t, sender.sent, recorded.Sent)
}

// seedDeliveryLifecycle reproduces record-node.ts recordDeliveryLifecycle.
func seedDeliveryLifecycle(t *testing.T, db *sql.DB) {
	t.Helper()
	createContentTables(t, db)
	exec(t, db, `INSERT INTO categories (id, name, slug) VALUES (1, 'AI', 'ai')`)
	insertArticle(t, db, "Lead story", "lead", "Lead excerpt", "<p>lead</p>", 1, "published", "2026-09-20 04:00:00")
	for _, subscriber := range golden(t).DeliveryLifecycle.Subscribers {
		var id int64
		var email, status string
		for i, target := range []any{&id, &email, &status} {
			if err := json.Unmarshal(subscriber[i], target); err != nil {
				t.Fatal(err)
			}
		}
		exec(t, db, `INSERT INTO subscribers (id, email, status, created_at, updated_at) VALUES (?, ?, ?, 'x', 'x')`, id, email, status)
	}
	exec(t, db, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, error, created_at)
    VALUES (5, '2026-09-20', 'sending', 'old-id', 'old error', '2026-09-20T04:59:00.000Z')`)
	exec(t, db, `INSERT INTO newsletter_deliveries (subscriber_id, edition_key, status, provider_message_id, created_at, sent_at)
    VALUES (6, '2026-09-20', 'sent', 'sent-id', '2026-09-20T04:59:00.000Z', '2026-09-20T04:59:30.000Z')`)
}

func TestDigestDeliveryStatesAndRetriesMatchNode(t *testing.T) {
	recorded := golden(t).DeliveryLifecycle
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{fail: map[string]error{"fails@example.com": errors.New(strings.Repeat("\u00e9", 499) + "\U0001f680 provider rejected")}}
	pace := &countingPace{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: pace.pace})

	first, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "first run", first, recorded.First)
	assertDeliveryRows(t, "first run", deliveryRows(t, db), recorded.AfterFirst)
	if got := pace.delays(); !reflect.DeepEqual(got, recorded.FirstDelays) {
		t.Errorf("first run pauses = %v, want %v", got, recorded.FirstDelays)
	}

	// The failed delivery is retried with the same idempotency key; sent
	// ones are skipped.
	sender.fail = nil
	pace.calls.Store(0)
	second, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:10:00.000Z"))
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, "second run", second, recorded.Second)
	assertDeliveryRows(t, "second run", deliveryRows(t, db), recorded.AfterSecond)
	if got := pace.delays(); !reflect.DeepEqual(got, recorded.SecondDelays) {
		t.Errorf("second run pauses = %v, want %v", got, recorded.SecondDelays)
	}
	assertSent(t, sender.sent, recorded.Sent)
	assertEditions(t, editionRows(t, db)[:1], []editionRow{{
		ID: 1, EditionKey: recorded.Editions[0].EditionKey, Subject: recorded.Editions[0].Subject,
		Articles: recorded.Editions[0].Articles, CreatedAt: recorded.Editions[0].CreatedAt,
	}}, true)
}

func TestDigestRequiresTheTokenSecretBeforeTouchingAnything(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: "short"})
	_, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	var configuration *ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("SendDailyDigest error = %v, want a configuration error", err)
	}
	if len(sender.sent) != 0 || len(editionRows(t, db)) != 0 {
		t.Fatal("a digest without a token secret sent or recorded something")
	}
}

func TestUnconfiguredDeliveryFailsEachDeliveryLikeNode(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	service := newTestService(t, db, NewResendSender(ResendConfig{Endpoint: "http://127.0.0.1:1/never"}), ServiceConfig{TokenSecret: testTokenSecret})
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil || result.Sent != 0 || result.Failed != 3 || result.Skipped != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	for _, row := range deliveryRows(t, db) {
		if row.Status == "failed" && (row.Error == nil || *row.Error != "Newsletter delivery is not configured") {
			t.Errorf("failed delivery error = %v", row.Error)
		}
	}
}

// Go-only guard: two digest requests at once (a cron retry and a manual
// POST) run one after the other, so the second sees the first one's sent
// rows instead of sending the same edition again.
func TestConcurrentDigestRunsNeverSendTwice(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	keys := map[string]int{}
	for _, sent := range sender.sent {
		keys[sent.key]++
	}
	if len(sender.sent) != 3 || len(keys) != 3 {
		t.Fatalf("sent = %v, want each of the 3 unsent subscribers exactly once", keys)
	}
}

// Like Node, a digest keeps going when the HTTP client goes away (Vercel's
// function times out long before a large digest ends).
func TestDigestIsNotCancelledWithTheRequest(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	service := newTestService(t, db, &recordingSender{}, ServiceConfig{TokenSecret: testTokenSecret})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.SendDailyDigest(ctx, mustTime(t, "2026-09-20T05:00:00.000Z"))
	if err != nil || result.Sent != 3 {
		t.Fatalf("result = %+v, %v", result, err)
	}
}

// Shutdown (the bound lifecycle context) stops a digest between deliveries
// and leaves no row in 'sending'; the next run resumes with the same keys.
func TestShutdownStopsTheDigestBetweenDeliveries(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	lifecycle, stop := context.WithCancel(context.Background())
	sender := &recordingSender{}
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret, Pace: func(ctx context.Context) error {
		stop()
		<-ctx.Done()
		return ctx.Err()
	}})
	service.Bind(lifecycle)
	result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	if !errors.Is(err, context.Canceled) || result.Sent != 1 {
		t.Fatalf("result = %+v, %v, want one delivery then cancellation", result, err)
	}
	for _, row := range deliveryRows(t, db) {
		if row.Status == "sending" && row.SubscriberID != 5 {
			t.Errorf("row left sending: %+v", row)
		}
	}
	resumed := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	second, err := resumed.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:05:00.000Z"))
	if err != nil || second.Sent != 2 || second.Skipped != 2 {
		t.Fatalf("resumed = %+v, %v", second, err)
	}
}

func TestDigestArticlesJSONIsJSONStringify(t *testing.T) {
	got := articlesJSON([]DigestArticle{{Title: "<T&>\u2028", Slug: "s", Excerpt: "\"e\"", Category: "c", ReadingMinutes: 2}})
	want := `[{"title":"<T&>` + "\u2028" + `","slug":"s","excerpt":"\"e\"","category":"c","readingMinutes":2}]`
	if got != want {
		t.Fatalf("articlesJSON = %s, want %s", got, want)
	}
	if articlesJSON(nil) != "[]" {
		t.Fatal("empty list")
	}
}

// blockingSender stands in for a Resend request that is still in flight at
// shutdown: it signals, then waits for its context to end.
type blockingSender struct {
	started chan string
	release chan struct{}
}

func newBlockingSender() *blockingSender {
	return &blockingSender{started: make(chan string, 16), release: make(chan struct{})}
}

func (b *blockingSender) Send(ctx context.Context, _ Email, key string) (string, error) {
	b.started <- key
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-b.release:
		return "released", nil
	}
}

func TestShutdownDuringASendRecordsAFailureAndTheRetryReusesTheKey(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	lifecycle, stop := context.WithCancel(context.Background())
	sender := newBlockingSender()
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	service.Bind(lifecycle)
	type outcome struct {
		result DigestResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
		done <- outcome{result, err}
	}()
	key := <-sender.started
	if key != "newsletter-digest-2026-09-20-1" {
		t.Fatalf("first key = %q", key)
	}
	stop()
	got := <-done
	if !errors.Is(got.err, context.Canceled) || got.result.Failed != 1 || got.result.Sent != 0 {
		t.Fatalf("result = %+v, %v", got.result, got.err)
	}
	for _, row := range deliveryRows(t, db) {
		if row.SubscriberID == 1 && (row.Status != "failed" || row.Error == nil || *row.Error != "context canceled") {
			t.Fatalf("interrupted delivery = %+v (error %v)", row, row.Error)
		}
	}
	// The next run retries the interrupted delivery with the same key.
	recorder := &recordingSender{}
	resumed := newTestService(t, db, recorder, ServiceConfig{TokenSecret: testTokenSecret})
	if _, err := resumed.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:05:00.000Z")); err != nil {
		t.Fatal(err)
	}
	if len(recorder.sent) == 0 || recorder.sent[0].key != "newsletter-digest-2026-09-20-1" {
		t.Fatalf("resumed keys = %+v", recorder.sent)
	}
}

func TestQueuedDigestRunsExitAtShutdown(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	lifecycle, stop := context.WithCancel(context.Background())
	sender := newBlockingSender()
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	service.Bind(lifecycle)
	first := make(chan error, 1)
	go func() {
		_, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
		first <- err
	}()
	<-sender.started
	queued := make(chan error, 1)
	go func() {
		_, err := service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
		queued <- err
	}()
	select {
	case err := <-queued:
		t.Fatalf("queued run did not wait for the running one: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	stop()
	for name, result := range map[string]chan error{"running": first, "queued": queued} {
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("%s run = %v, want context.Canceled", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s run did not exit at shutdown", name)
		}
	}
	if len(sender.started) != 0 {
		t.Fatal("the queued run sent after shutdown")
	}
}

func TestWaitReturnsOnceInFlightRunsEnd(t *testing.T) {
	db := migratedDatabase(t)
	seedDeliveryLifecycle(t, db)
	sender := newBlockingSender()
	service := newTestService(t, db, sender, ServiceConfig{TokenSecret: testTokenSecret})
	if err := service.Wait(context.Background()); err != nil {
		t.Fatalf("idle Wait = %v", err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = service.SendDailyDigest(context.Background(), mustTime(t, "2026-09-20T05:00:00.000Z"))
	}()
	<-sender.started
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait during a run = %v, want the deadline", err)
	}
	close(sender.release)
	if err := service.Wait(context.Background()); err != nil {
		t.Fatalf("Wait = %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Wait returned before the run finished")
	}
}
