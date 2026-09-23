// Package newsletter ports the Node newsletter (apps/server/src/newsletter and
// the newsletter routes of apps/server/src/routes/public.ts): immediate
// signup, signed confirm and unsubscribe links, the public edition archive,
// and the daily digest delivered through Resend.
package newsletter

import (
	_ "embed"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

// newsletterSchema is Node's final newsletter schema (apps/server/src/db.ts:72-103,
// 138-139), verbatim. CREATE ... IF NOT EXISTS keeps it a no-op on a
// database Node created, including one whose subscribers columns Node added
// with ALTER TABLE.
//
//go:embed migrations/006_newsletter.sql
var newsletterSchema string

// Migrations returns a fresh slice containing the newsletter's immutable schema descriptors.
func Migrations() []migrate.Descriptor {
	return []migrate.Descriptor{
		{Version: 6, Name: "newsletter", SQL: newsletterSchema},
	}
}
