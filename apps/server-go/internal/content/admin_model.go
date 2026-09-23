package content

import "net/http"

// AdminError is a dashboard request failure carrying Node's exact status and
// JSON error body ({"error": Message} plus "details" when present).
type AdminError struct {
	Status  int
	Message string
	Details []string
}

func (e *AdminError) Error() string { return e.Message }

func badRequest(message string) *AdminError {
	return &AdminError{Status: http.StatusBadRequest, Message: message}
}

func articleNotFound() *AdminError {
	return &AdminError{Status: http.StatusNotFound, Message: "Article not found"}
}

func categoryNotFound() *AdminError {
	return &AdminError{Status: http.StatusNotFound, Message: "Category not found"}
}

// DashboardQuery is GET /api/dashboard/articles after Node's query parsing.
type DashboardQuery struct {
	Page     float64
	Limit    int
	Status   string
	Category string
	Search   string
}

// CategoryWithCount is one GET /api/dashboard/categories row (c.* plus article_count).
type CategoryWithCount struct {
	Category
	ArticleCount int64 `json:"article_count"`
}

// ArticleChange is the result of a dashboard article mutation: the response
// article and the public slugs Node hands to IndexNow after responding.
type ArticleChange struct {
	Article       Article
	IndexNowSlugs []string
}

// storedArticle is the subset of `SELECT * FROM articles` the Node update
// handler reads from the existing row.
type storedArticle struct {
	Title      string
	Slug       string
	Excerpt    string
	Content    string
	CategoryID int64
	Status     string
	Source     *string
	SourceURL  *string
}

// assignment is one `column = ?` of a partial UPDATE. Columns always come
// from fixed allowlists in this package, never from request input.
type assignment struct {
	column string
	value  any
}
