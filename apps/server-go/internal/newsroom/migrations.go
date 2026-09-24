package newsroom

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

//go:embed migrations/003_newsroom.sql
var newsroomSchema string

//go:embed migrations/004_newsroom_published_index.sql
var publishedIndexSchema string

//go:embed migrations/007_newsroom_publish_now.sql
var publishNowSchema string

// Migrations returns a fresh slice containing newsroom's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 3, Name: "newsroom candidates", SQL: newsroomSchema},
		{Version: 4, Name: "newsroom published index", SQL: publishedIndexSchema},
		{Version: 7, Name: "newsroom publish now", SQL: publishNowSchema},
	}
}
