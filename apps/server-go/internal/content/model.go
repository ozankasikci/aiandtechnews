package content

// Article mirrors the public Node article representation. Pointer fields encode
// SQL NULL as JSON null while timestamp values remain unparsed source strings.
type Article struct {
	ID              int64    `json:"id"`
	Title           string   `json:"title"`
	Slug            string   `json:"slug"`
	Excerpt         string   `json:"excerpt"`
	Content         string   `json:"content"`
	FeaturedImage   *string  `json:"featured_image"`
	CategoryID      int64    `json:"category_id"`
	AuthorID        int64    `json:"author_id"`
	Status          string   `json:"status"`
	PublishedAt     *string  `json:"published_at"`
	MetaTitle       *string  `json:"meta_title"`
	MetaDescription *string  `json:"meta_description"`
	Source          *string  `json:"source"`
	SourceURL       *string  `json:"source_url"`
	ViewCount       int64    `json:"view_count"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	Category        Category `json:"category"`
	Author          Author   `json:"author"`
	// TLDR and WhyItMatters come from article_summaries and are loaded only
	// for a single article read by slug.
	TLDR         []string `json:"tldr,omitempty"`
	WhyItMatters string   `json:"why_it_matters,omitempty"`
	// PrimarySource is set on a single article read when the article was
	// reported from a primary document (a company's own announcement or
	// release notes): the website links to it. Press sources are never shown.
	PrimarySource *PrimarySourceRef `json:"primary_source,omitempty"`
	// InlineImages are the ready illustrations inside the body, from
	// article_images; also loaded only for a single article read by slug.
	InlineImages []InlineImage `json:"inlineImages,omitempty"`
	// Topics are the article's live topic hubs (at most MaxArticleTopics),
	// loaded for public reads only. Absent when it has none.
	Topics []TopicRef `json:"topics,omitempty"`
}

// PrimarySourceRef names the organisation behind a primary document and links to it.
type PrimarySourceRef struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// InlineImage is an illustration placed after paragraph AfterParagraph
// (counting the body's <p> blocks from 1).
type InlineImage struct {
	URL            string `json:"url"`
	Alt            string `json:"alt"`
	AfterParagraph int    `json:"afterParagraph"`
	// Credit is set for a real photo ("Photo: Jane Doe / CC BY-SA 4.0, via
	// Wikimedia Commons", "Image: WiCi") and links to CreditURL (the Commons
	// file page or the maker's page); License and LicenseURL are for Commons
	// photos. A generated illustration has none of them.
	Credit     string `json:"credit,omitempty"`
	CreditURL  string `json:"creditUrl,omitempty"`
	License    string `json:"license,omitempty"`
	LicenseURL string `json:"licenseUrl,omitempty"`
}

type Category struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

type Author struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Email  string  `json:"email"`
	Avatar *string `json:"avatar"`
	Bio    *string `json:"bio"`
	Role   string  `json:"role"`
}

type ListQuery struct {
	Page     float64
	Limit    int
	Category string
	Search   string
}

type ListResult struct {
	Articles []Article
	Total    int64
}
