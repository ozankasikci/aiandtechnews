package collector

import (
	"context"
	"html"
	"regexp"
	"strings"
	"sync"
	"time"
)

// JudgeItem is one feed item for the AI relevance judge, with the start of
// the article's own text when the page could be fetched.
type JudgeItem struct {
	Key     string // the item's URL
	Source  string
	Title   string
	Summary string
	Text    string
}

// Verdict is the judge's decision for one item.
type Verdict struct {
	Candidate bool
	Reason    string
}

// Judge decides which new feed items should become candidates. It replaces
// the AI keyword rule; the structural rules (approved source, no deals,
// videos, reviews or old reposts) still run first.
type Judge interface {
	Judge(ctx context.Context, items []JudgeItem) (map[string]Verdict, error)
}

// JudgeMemory remembers items the judge rejected, so each is judged once.
type JudgeMemory interface {
	KnownURLs(ctx context.Context, urls []string) (map[string]bool, error)
	RejectedURLs(ctx context.Context, urls []string) (map[string]bool, error)
	RememberRejected(ctx context.Context, reasons map[string]string) error
}

// JudgedRejection is the report key for items the judge turned down.
const JudgedRejection = "not a news candidate (AI review)"

const (
	judgeBatch       = 25
	articleTextLimit = 2000
	fetchConcurrency = 4
	fetchTimeout     = 20 * time.Second
)

// WithJudge makes the collector ask judge about every new item instead of
// applying the keyword rule. memory must be the candidate store.
func (c *Collector) WithJudge(judge Judge, memory JudgeMemory) *Collector {
	c.judge, c.memory = judge, memory
	return c
}

var (
	paragraphTag = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	anyTag       = regexp.MustCompile(`(?s)<[^>]+>`)
)

// ArticleText is the start of the page's paragraph text.
func ArticleText(body string) string {
	var b strings.Builder
	for _, match := range paragraphTag.FindAllStringSubmatch(body, -1) {
		text := strings.Join(strings.Fields(html.UnescapeString(anyTag.ReplaceAllString(match[1], " "))), " ")
		if len(text) < 40 {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
		if b.Len() >= articleTextLimit {
			break
		}
	}
	text := b.String()
	if len(text) > articleTextLimit {
		text = text[:articleTextLimit]
	}
	return strings.TrimSpace(text)
}

// judgeItems fetches each item's article text and asks the judge in batches.
// Items the judge could not decide are returned in undecided.
func (c *Collector) judgeItems(ctx context.Context, items []FeedItem) (verdicts map[string]Verdict, undecided []FeedItem) {
	judgeItems := make([]JudgeItem, len(items))
	var wg sync.WaitGroup
	slots := make(chan struct{}, fetchConcurrency)
	for i, item := range items {
		judgeItems[i] = JudgeItem{Key: item.URL, Source: item.Source, Title: item.Title, Summary: item.Summary}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
			defer cancel()
			if body, _, err := c.fetcher.FetchText(fetchCtx, item.URL, item.Source); err == nil {
				judgeItems[i].Text = ArticleText(body)
			}
		}()
	}
	wg.Wait()

	verdicts = map[string]Verdict{}
	for start := 0; start < len(judgeItems); start += judgeBatch {
		batch := judgeItems[start:min(start+judgeBatch, len(judgeItems))]
		answer, err := c.judge.Judge(ctx, batch)
		if err != nil {
			c.logger.WarnContext(ctx, "relevance judge failed; using the keyword rule for these items", "items", len(batch), "error", err)
		}
		for _, item := range batch {
			if verdict, ok := answer[item.Key]; ok && err == nil {
				verdicts[item.Key] = verdict
			}
		}
	}
	for _, item := range items {
		if _, ok := verdicts[item.URL]; !ok {
			undecided = append(undecided, item)
		}
	}
	return verdicts, undecided
}
