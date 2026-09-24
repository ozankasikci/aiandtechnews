package newsroom

import (
	"errors"
	"time"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusPublished  Status = "published"
	StatusFailed     Status = "failed"
	StatusRejected   Status = "rejected"
)

var allStatuses = []Status{StatusPending, StatusQueued, StatusProcessing, StatusPublished, StatusFailed, StatusRejected}

// ParseStatus returns the Status named by value.
func ParseStatus(value string) (Status, bool) {
	for _, status := range allStatuses {
		if string(status) == value {
			return status, true
		}
	}
	return "", false
}

var (
	ErrNotFound        = errors.New("candidate not found")
	ErrStaleTransition = errors.New("candidate status changed")
	ErrInvalidDelay    = errors.New("publish delay minutes must be between 1 and 1440, with min not above max")
)

// Candidate is the API shape of a potential article.
type Candidate struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	FeedSummary     string  `json:"feed_summary"`
	SourceName      string  `json:"source_name"`
	SourceURL       string  `json:"source_url"`
	SourceImageURL  *string `json:"source_image_url"`
	FeedPublishedAt *string `json:"feed_published_at"`
	DiscoveredAt    string  `json:"discovered_at"`
	Status          Status  `json:"status"`
	ScheduledFor    *string `json:"scheduled_for"`
	Attempts        int64   `json:"attempts"`
	LastError       *string `json:"last_error"`
	ArticleSlug     *string `json:"article_slug"`
	// PublishNow is set while an editor's "publish now" request is pending:
	// the publisher claims the candidate ahead of the queue and without the
	// minimum gap since the last publish.
	PublishNow bool `json:"publish_now"`
}

// NewCandidate is what the collector stores for a policy-passing feed item.
type NewCandidate struct {
	SourceURL       string
	SourceName      string
	FeedURL         string
	Title           string
	FeedSummary     string
	SourceImageURL  *string
	FeedPublishedAt *time.Time
}

type Page struct {
	Candidates []Candidate `json:"candidates"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	TotalPages int64       `json:"totalPages"`
}

type Overview struct {
	Pending         int64   `json:"pending"`
	Queued          int64   `json:"queued"`
	Processing      int64   `json:"processing"`
	Failed          int64   `json:"failed"`
	PublishedToday  int64   `json:"published_today"`
	NextPublishAt   *string `json:"next_publish_at"`
	LastCollectedAt *string `json:"last_collected_at"`
}

type PublishDelay struct {
	MinMinutes int `json:"publish_delay_min_minutes"`
	MaxMinutes int `json:"publish_delay_max_minutes"`
}

func (d PublishDelay) Validate() error {
	if d.MinMinutes < 1 || d.MinMinutes > d.MaxMinutes || d.MaxMinutes > 1440 {
		return ErrInvalidDelay
	}
	return nil
}

// Skipped reports an id a batch operation did not apply, with a reason for the client.
type Skipped struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// Timestamps are stored and served as RFC 3339 UTC with second precision so
// that SQLite's lexical ordering matches chronological ordering.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339, value) }
