package editorial

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

//go:embed migrations/001_authors.sql
var authorsSchema string

// Migrations returns a fresh slice containing editorial's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{{Version: 1, Name: "editorial authors", SQL: authorsSchema}}
}
