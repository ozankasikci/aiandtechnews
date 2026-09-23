package newsletter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// DefaultSiteURL is Node's NEWSLETTER_SITE_URL default.
	DefaultSiteURL = "https://aiandtech.news"
	// DefaultPacing is Node's pause between two digest deliveries, which
	// keeps the digest under Resend's rate limit.
	DefaultPacing = 550 * time.Millisecond
)

// ErrInvalidEmail is Node's TypeError("Valid email required").
var ErrInvalidEmail = errors.New("Valid email required")

type SubscriptionState string

const (
	StateSubscribed    SubscriptionState = "subscribed"
	StateAlreadyActive SubscriptionState = "already_active"
)

type ConfirmationState string

const (
	ConfirmationConfirmed        ConfirmationState = "confirmed"
	ConfirmationAlreadyConfirmed ConfirmationState = "already_confirmed"
	ConfirmationInvalid          ConfirmationState = "invalid"
)

// ConfirmationResult is the confirm route's JSON body.
type ConfirmationResult struct {
	State       ConfirmationState `json:"state"`
	WelcomeSent bool              `json:"welcomeSent"`
}

type UnsubscribeState string

const (
	UnsubscribeUnsubscribed        UnsubscribeState = "unsubscribed"
	UnsubscribeAlreadyUnsubscribed UnsubscribeState = "already_unsubscribed"
	UnsubscribeInvalid             UnsubscribeState = "invalid"
)

// Edition is one archived digest. Articles is the stored JSON array,
// passed through like Node's JSON.parse/JSON.stringify round trip.
type Edition struct {
	Edition   string          `json:"edition"`
	Subject   string          `json:"subject"`
	Articles  json.RawMessage `json:"articles"`
	CreatedAt string          `json:"createdAt"`
}

// Sender delivers one email; it returns the provider's message id.
type Sender interface {
	Send(ctx context.Context, email Email, idempotencyKey string) (string, error)
}

// ServiceConfig holds the raw settings; like Node they are trimmed on use.
type ServiceConfig struct {
	SiteURL     string // NEWSLETTER_SITE_URL
	TokenSecret string // NEWSLETTER_TOKEN_SECRET
	// Local is the zone for published_at values without an offset, which
	// V8's Date.parse reads in the process time zone. Defaults to time.Local.
	Local *time.Location
	// Pace waits between digest deliveries (Node: 550ms). Defaults to
	// DefaultPacing with a context-aware timer.
	Pace func(context.Context) error
}

// Service is Node's NewsletterService.
type Service struct {
	store       *SQLiteStore
	sender      Sender
	siteURL     string
	tokenSecret string
	local       *time.Location
	pace        func(context.Context) error
	logger      *slog.Logger

	// digestSlot serializes digest runs; waiting for it ends at shutdown.
	digestSlot chan struct{}
	// lifecycle bounds digest runs and welcome emails (see Bind).
	lifecycle struct {
		sync.Mutex
		ctx context.Context
	}
	// inflight counts running digests and welcome emails (see Wait).
	inflight struct {
		sync.Mutex
		count int
		idle  chan struct{}
	}
}

// NewService validates NEWSLETTER_SITE_URL like Node's constructor does, so
// a bad value fails composition (Node fails to start).
func NewService(store *SQLiteStore, sender Sender, cfg ServiceConfig, logger *slog.Logger) (*Service, error) {
	if store == nil || sender == nil || logger == nil {
		return nil, errors.New("newsletter: store, sender, and logger are required")
	}
	siteURL, err := SiteOrigin(cfg.SiteURL)
	if err != nil {
		return nil, err
	}
	service := &Service{store: store, sender: sender, siteURL: siteURL, tokenSecret: cfg.TokenSecret, local: cfg.Local, pace: cfg.Pace, logger: logger,
		digestSlot: make(chan struct{}, 1)}
	if service.local == nil {
		service.local = time.Local
	}
	if service.pace == nil {
		service.pace = func(ctx context.Context) error { return sleepContext(ctx, DefaultPacing) }
	}
	return service, nil
}

// SiteOrigin is Node's safeSiteUrl: the origin of NEWSLETTER_SITE_URL
// (default https://aiandtech.news), which must be https unless the host is
// localhost. Go accepts only http and https URLs with ASCII hosts; Node's
// WHATWG parser would also take other schemes on localhost and IDN hosts.
func SiteOrigin(raw string) (string, error) {
	value := jsTrim(raw)
	if value == "" {
		value = DefaultSiteURL
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be a valid URL"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	hostname := strings.ToLower(parsed.Hostname())
	if scheme != "https" && hostname != "localhost" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must use HTTPS"}
	}
	if scheme != "https" && scheme != "http" {
		return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be an http or https URL"}
	}
	for _, r := range hostname {
		if r >= utf8.RuneSelf {
			return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must use an ASCII host name"}
		}
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	origin := scheme + "://" + hostname
	if port := parsed.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		number, err := strconv.Atoi(port)
		if err != nil || number > 65535 {
			return "", &ConfigurationError{message: "NEWSLETTER_SITE_URL must be a valid URL"}
		}
		origin += ":" + strconv.Itoa(number)
	}
	return origin, nil
}

// requireTokenSecret is Node's requireTokenSecret: the trimmed secret must
// have at least 32 UTF-16 code units.
func (s *Service) requireTokenSecret() (string, error) {
	secret := jsTrim(s.tokenSecret)
	if utf16Length(secret) < 32 {
		return "", &ConfigurationError{message: "NEWSLETTER_TOKEN_SECRET must contain at least 32 characters"}
	}
	return secret, nil
}

// normalizeEmail is Node's normalizeEmail: trimmed, lowercased, matching
// /^[^\s@]+@[^\s@]+\.[^\s@]+$/, and at most 254 UTF-16 code units.
func normalizeEmail(raw string) (string, bool) {
	value := jsToLower(jsTrim(raw))
	at := strings.IndexByte(value, '@')
	if at <= 0 || utf16Length(value) > 254 || strings.ContainsFunc(value, isJSWhitespace) {
		return "", false
	}
	domain := value[at+1:]
	if strings.IndexByte(domain, '@') >= 0 {
		return "", false
	}
	// [^\s@]+\.[^\s@]+: some dot with a character before and after it.
	if len(domain) < 3 || !strings.Contains(domain[1:len(domain)-1], ".") {
		return "", false
	}
	return value, true
}

func (s *Service) unsubscribeURL(subscriberID int64, secret string) string {
	return s.siteURL + "/api/newsletter/unsubscribe?token=" + encodeURIComponent(CreateToken(subscriberID, PurposeUnsubscribe, secret, nil))
}

// Subscribe is requestSubscription: the address becomes active at once and
// nothing is sent, so it works without any email or token settings.
func (s *Service) Subscribe(ctx context.Context, rawEmail, placement string, now time.Time) (SubscriptionState, error) {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return "", ErrInvalidEmail
	}
	return s.store.Subscribe(ctx, email, sliceUTF16(placement, 80), isoTimestamp(now))
}

// Confirm is confirmSubscription, kept for confirm links in emails sent by
// the old double opt-in flow. Confirming a pending subscriber sends the
// welcome email (idempotency key newsletter-welcome-<id>); a failed send is
// logged and reported as welcomeSent: false, never as an error.
func (s *Service) Confirm(ctx context.Context, token string, now time.Time) (ConfirmationResult, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return ConfirmationResult{}, err
	}
	id, ok := VerifyToken(token, PurposeConfirm, secret, now)
	if !ok {
		return ConfirmationResult{State: ConfirmationInvalid}, nil
	}
	state, email, err := s.store.Confirm(ctx, id, isoTimestamp(now))
	if err != nil || state != ConfirmationConfirmed {
		return ConfirmationResult{State: state}, err
	}
	unsubscribeURL := s.unsubscribeURL(id, secret)
	// Like Node, the send outlives the request; shutdown cancels it.
	sendCtx, finish := s.begin(ctx)
	_, err = s.sender.Send(sendCtx, WelcomeEmail(email, s.siteURL, unsubscribeURL), "newsletter-welcome-"+strconv.FormatInt(id, 10))
	finish()
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to send newsletter welcome email", "error", err, "subscriber_id", id)
		return ConfirmationResult{State: ConfirmationConfirmed}, nil
	}
	return ConfirmationResult{State: ConfirmationConfirmed, WelcomeSent: true}, nil
}

// Unsubscribe is unsubscribe: a valid unsubscribe token for any existing
// subscriber (pending ones included) unsubscribes it.
func (s *Service) Unsubscribe(ctx context.Context, token string, now time.Time) (UnsubscribeState, error) {
	secret, err := s.requireTokenSecret()
	if err != nil {
		return "", err
	}
	id, ok := VerifyToken(token, PurposeUnsubscribe, secret, now)
	if !ok {
		return UnsubscribeInvalid, nil
	}
	return s.store.Unsubscribe(ctx, id, isoTimestamp(now))
}

// ListEditions is listEditions: newest edition key first, limit clamped to 1..100.
func (s *Service) ListEditions(ctx context.Context, limit float64) ([]Edition, error) {
	return s.store.ListEditions(ctx, int(min(max(limit, 1), 100)))
}

// Edition is getEdition; ok is false when the key does not exist.
func (s *Service) Edition(ctx context.Context, key string) (Edition, bool, error) {
	return s.store.Edition(ctx, key)
}
