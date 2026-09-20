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
