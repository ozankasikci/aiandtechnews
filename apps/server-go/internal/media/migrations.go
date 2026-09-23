package media

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// mediaSchema is Node's media table (apps/server/src/db.ts:58-65), verbatim.
// CREATE TABLE IF NOT EXISTS keeps it a no-op on a database Node created.
//
//go:embed migrations/005_media.sql
var mediaSchema string

// Migrations returns a fresh slice containing media's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 5, Name: "media library", SQL: mediaSchema},
	}
}
