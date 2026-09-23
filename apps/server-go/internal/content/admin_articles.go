package content

import (
	"context"
	"errors"
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

// UpdateArticle ports dashboard.ts:316-462: a partial update in which every
// defined property is written (null included), with the publishing policy
// applied to the merged next state.
func (s *AdminService) UpdateArticle(ctx context.Context, id string, body jsonbody.Object) (ArticleChange, error) {
	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		existing, err := tx.storedArticle(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return articleNotFound()
		}
		if err != nil {
			return err
		}

		sourceURL := body.Get("source_url")
		normalizedSourceURL := existing.SourceURL
		if sourceURL.Defined() {
			if sourceURL.IsNull() || sourceURL.Is("") {
				normalizedSourceURL = nil
			} else {
				normalized, err := NormalizeSourceURL(sourceURL.JSString())
				if err != nil {
					return badRequest("Source URL is invalid")
				}
				normalizedSourceURL = &normalized
			}
		}

		existingSource := jsonbody.Null()
		if existing.Source != nil {
			existingSource = jsonbody.String(*existing.Source)
		}
		status, categoryID := body.Get("status"), body.Get("category_id")
		nextTitle := body.Get("title").Or(jsonbody.String(existing.Title))
		nextSlug := body.Get("slug").Or(jsonbody.String(existing.Slug))
		nextExcerpt := body.Get("excerpt").Or(jsonbody.String(existing.Excerpt))
		nextContent := body.Get("content").Or(jsonbody.String(existing.Content))
		nextSource := body.Get("source").Or(existingSource)
		nextPublished := status.Or(jsonbody.String(existing.Status)).Is("published")

		var nextCategoryID any = existing.CategoryID
		if !categoryID.Nullish() {
			if nextCategoryID, err = categoryID.Bind(); err != nil {
				return fmt.Errorf("bind category_id: %w", err)
			}
		}
		if err := checkCategory(ctx, tx, nextCategoryID); err != nil {
			return err
		}

		if nextPublished {
			if !nextSource.Truthy() || normalizedSourceURL == nil {
				return badRequest("Published articles require source and source_url")
			}
			errs := validatePublishedArticle(nextTitle.StringOrEmpty(), nextExcerpt.StringOrEmpty(), nextContent.StringOrEmpty(), nextSource, *normalizedSourceURL)
			if len(errs) > 0 {
				return policyFailure(errs)
			}
		}

		boundSlug, err := nextSlug.Bind()
		if err != nil {
			return fmt.Errorf("bind slug: %w", err)
		}
		duplicate, err := tx.articleConflict(ctx, &id, boundSlug, normalizedSourceURL)
		if err != nil {
			return err
		}
		if duplicate {
			return duplicateArticle()
		}

		now := s.now().UTC()
		publishedAt := body.Get("published_at")
		if status.Is("published") && !publishedAt.Truthy() {
			publishedAt = jsonbody.String(now.Format(javaScriptISO))
		}
		source := body.Get("source")
		if source.Defined() && !source.Truthy() {
			source = jsonbody.Null() // source || null
		}

		type field struct {
			column string
			value  jsonbody.Value
		}
		fields := make([]field, 0, 11)
		for _, candidate := range []field{
			{"title", body.Get("title")}, {"slug", body.Get("slug")}, {"excerpt", body.Get("excerpt")},
			{"content", body.Get("content")}, {"featured_image", body.Get("featured_image")},
			{"category_id", categoryID}, {"status", status}, {"published_at", publishedAt},
			{"meta_title", body.Get("meta_title")}, {"meta_description", body.Get("meta_description")},
			{"source", source},
		} {
			if candidate.value.Defined() {
				fields = append(fields, candidate)
			}
		}
		var authorID int64
		if nextPublished {
			if authorID, err = requireEditorialAuthor(ctx, tx); err != nil {
				return err
			}
		}
		if len(fields) == 0 && !sourceURL.Defined() && !nextPublished {
			return badRequest("No fields to update")
		}

		assignments := make([]assignment, 0, len(fields)+3)
		for _, f := range fields {
			value, err := f.value.Bind()
			if err != nil {
				return fmt.Errorf("bind %s: %w", f.column, err)
			}
			assignments = append(assignments, assignment{f.column, value})
		}
		if sourceURL.Defined() {
			assignments = append(assignments, assignment{"source_url", nullableString(normalizedSourceURL)})
		}
		if nextPublished {
			assignments = append(assignments, assignment{"author_id", authorID})
		}
		assignments = append(assignments, assignment{"updated_at", now.Format(sqliteTimestamp)})
		if err := tx.updateRow(ctx, "articles", id, assignments); err != nil {
			return err
		}
		if change.Article, err = tx.article(ctx, id); err != nil {
			return err
		}

		var slugs []string
		if existing.Status == "published" {
			slugs = append(slugs, existing.Slug)
		}
		if nextPublished {
			slugs = append(slugs, nextSlug.JSString())
		}
		change.IndexNowSlugs = uniqueStrings(slugs)
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}

// DeleteArticle ports dashboard.ts:464-479.
func (s *AdminService) DeleteArticle(ctx context.Context, id string) (ArticleChange, error) {
	var change ArticleChange
	err := s.store.inAdminTx(ctx, func(tx adminTx) error {
		slug, status, found, err := tx.articleSlugStatus(ctx, id)
		if err != nil {
			return err
		}
		deleted, err := tx.deleteRow(ctx, "articles", id)
		if err != nil {
			return err
		}
		if !deleted {
			return articleNotFound()
		}
		if found && status == "published" {
			change.IndexNowSlugs = []string{slug}
		}
		return nil
	})
	if err != nil {
		return ArticleChange{}, err
	}
	return change, nil
}
