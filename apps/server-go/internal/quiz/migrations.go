package quiz

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

//go:embed migrations/010_quizzes.sql
var quizzesSchema string

// Migrations returns a fresh slice containing the quiz module's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 10, Name: "daily quizzes", SQL: quizzesSchema},
	}
}
