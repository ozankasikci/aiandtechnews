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

//go:embed migrations/012_article_views_hourly.sql
var articleViewsHourlySchema string

//go:embed migrations/013_article_image_credits.sql
var articleImageCreditsSchema string

//go:embed migrations/014_topics.sql
var topicsSchema string

//go:embed migrations/015_featured_reimage.sql
var featuredReimageSchema string

//go:embed migrations/016_telegram_posts.sql
var telegramPostsSchema string

// Migrations returns a fresh slice containing content's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 2, Name: "content categories and articles", SQL: contentSchema},
		{Version: 8, Name: "article views per day", SQL: articleViewsSchema},
		{Version: 9, Name: "article summaries", SQL: articleSummariesSchema},
		{Version: 11, Name: "article inline images", SQL: articleImagesSchema},
		{Version: 12, Name: "article views per hour", SQL: articleViewsHourlySchema},
		{Version: 13, Name: "article image credits", SQL: articleImageCreditsSchema},
		{Version: 14, Name: "topics", SQL: topicsSchema},
		{Version: 15, Name: "featured image reimage retries", SQL: featuredReimageSchema},
		{Version: 16, Name: "telegram channel posts", SQL: telegramPostsSchema},
	}
}
