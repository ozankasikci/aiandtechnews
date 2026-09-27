package content

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

//go:embed migrations/002_categories_articles.sql
var contentSchema string

//go:embed migrations/008_article_views.sql
var articleViewsSchema string

//go:embed migrations/009_article_summaries.sql
var articleSummariesSchema string

//go:embed migrations/011_article_images.sql
var articleImagesSchema string

// Migrations returns a fresh slice containing content's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 2, Name: "content categories and articles", SQL: contentSchema},
		{Version: 8, Name: "article views per day", SQL: articleViewsSchema},
		{Version: 9, Name: "article summaries", SQL: articleSummariesSchema},
		{Version: 11, Name: "article inline images", SQL: articleImagesSchema},
	}
}
