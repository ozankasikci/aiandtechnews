package content

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/jsonbody"
)

const (
	// sqliteTimestamp is Node's formatSqliteTimestamp (toISOString, "T"->" ", first 19 chars).
	sqliteTimestamp = "2006-01-02 15:04:05"
	// javaScriptISO is Date.prototype.toISOString, used by Node for default published_at values.
	javaScriptISO = "2006-01-02T15:04:05.000Z"
)

// orEmpty is `value || ""` and orNull is `value || null`, bound like better-sqlite3.
func orEmpty(v jsonbody.Value) (any, error) {
	if v.Truthy() {
		return v.Bind()
	}
	return "", nil
}

func orNull(v jsonbody.Value) (any, error) {
	if v.Truthy() {
		return v.Bind()
	}
	return nil, nil
}

// validatePublishedArticle ports dashboard.ts:180-196. validateRewrittenArticle
// treats non-string input as "", which StringOrEmpty reproduces at the call sites.
func validatePublishedArticle(title, excerpt, content string, source jsonbody.Value, sourceURL string) []string {
	errs := ValidateRewrittenArticle(RewrittenArticle{Title: title, Excerpt: excerpt, Content: content}, ArticleValidationOptions{})
	expected, ok := SourceForURL(sourceURL)
	if !ok {
		errs = append(errs, "source URL is not from an approved publication")
	}
	if ok && !source.Is(expected) {
		errs = append(errs, "source does not match source URL")
	}
	return uniqueStrings(errs)
}

// uniqueStrings is JavaScript [...new Set(values)].
func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

// checkCategory ports the category lookup and deals guard (dashboard.ts:248-256, 363-371).
func checkCategory(ctx context.Context, tx adminTx, categoryID any) error {
	slug, found, err := tx.categorySlug(ctx, categoryID)
	if err != nil {
		return err
	}
	if !found {
		return badRequest("Category not found")
	}
	if slug == "deals" {
		return badRequest("Deals articles are not allowed")
	}
	return nil
}

func requireEditorialAuthor(ctx context.Context, tx adminTx) (int64, error) {
	id, found, err := tx.editorialAuthorID(ctx)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, &AdminError{Status: http.StatusInternalServerError, Message: "TechNews Editorial author is missing"}
	}
	return id, nil
}

func duplicateArticle() *AdminError {
	return &AdminError{Status: http.StatusConflict, Message: "Article slug or source_url already exists"}
}

func policyFailure(details []string) *AdminError {
	return &AdminError{Status: http.StatusBadRequest, Message: "Article failed publishing policy", Details: details}
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// bindAll binds values in order, failing like better-sqlite3's .run() would.
func bindAll(binders ...func() (any, error)) ([]any, error) {
	values := make([]any, 0, len(binders))
	for _, bind := range binders {
		value, err := bind()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func constant(value any) func() (any, error) {
	return func() (any, error) { return value, nil }
}

// CreateArticle ports dashboard.ts:198-314. The author is always the
// TechNews Editorial account, whoever is signed in.
func (s *AdminService) CreateArticle(ctx context.Context, body jsonbody.Object) (ArticleChange, error) {
	title, slug, categoryID := body.Get("title"), body.Get("slug"), body.Get("category_id")
	if !title.Truthy() || !slug.Truthy() || !categoryID.Truthy() {
		return ArticleChange{}, badRequest("Title, slug, and category_id are required")
	}
	status := body.Get("status")
	if !status.Truthy() {
		status = jsonbody.String("draft")
	}
	published := status.Is("published")

	var normalizedSourceURL *string
	if sourceURL := body.Get("source_url"); sourceURL.Truthy() {
		normalized, err := NormalizeSourceURL(sourceURL.JSString())
		if err != nil {
			return ArticleChange{}, badRequest("Source URL is invalid")
		}
		normalizedSourceURL = &normalized
	}
	source := body.Get("source")
	if published {
		if !source.Truthy() || normalizedSourceURL == nil {
			return ArticleChange{}, badRequest("Published articles require source and source_url")
		}
		errs := validatePublishedArticle(title.StringOrEmpty(), body.Get("excerpt").StringOrEmpty(), body.Get("content").StringOrEmpty(), source, *normalizedSourceURL)
		if len(errs) > 0 {
			return ArticleChange{}, policyFailure(errs)
		}
	}

	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		boundCategoryID, err := categoryID.Bind()
		if err != nil {
			return fmt.Errorf("bind category_id: %w", err)
		}
		if err := checkCategory(ctx, tx, boundCategoryID); err != nil {
			return err
		}
		boundSlug, err := slug.Bind()
		if err != nil {
			return fmt.Errorf("bind slug: %w", err)
		}
		duplicate, err := tx.articleConflict(ctx, nil, boundSlug, normalizedSourceURL)
		if err != nil {
			return err
		}
		if duplicate {
			return duplicateArticle()
		}
		authorID, err := requireEditorialAuthor(ctx, tx)
		if err != nil {
			return err
		}

		now := s.now().UTC()
		publishedAt := body.Get("published_at")
		if published && !publishedAt.Truthy() {
			publishedAt = jsonbody.String(now.Format(javaScriptISO))
		}
		stamp := now.Format(sqliteTimestamp)
		values, err := bindAll(
			title.Bind, slug.Bind,
			func() (any, error) { return orEmpty(body.Get("excerpt")) },
			func() (any, error) { return orEmpty(body.Get("content")) },
			func() (any, error) { return orNull(body.Get("featured_image")) },
			categoryID.Bind, constant(authorID), status.Bind,
			func() (any, error) { return orNull(publishedAt) },
			func() (any, error) { return orNull(body.Get("meta_title")) },
			func() (any, error) { return orNull(body.Get("meta_description")) },
			func() (any, error) { return orNull(source) },
			constant(nullableString(normalizedSourceURL)), constant(stamp), constant(stamp),
		)
		if err != nil {
			return fmt.Errorf("bind article insert: %w", err)
		}
		id, err := tx.insertArticle(ctx, values)
		if err != nil {
			return err
		}
		if change.Article, err = tx.article(ctx, id); err != nil {
			return err
		}
		if published {
			change.IndexNowSlugs = []string{slug.JSString()}
		}
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}
