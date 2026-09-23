package adopt

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/database/migrate"
)

func TestCompareCountsAllowsOnlyTheNewsroomSettingsToGrow(t *testing.T) {
	before := map[string]int64{"articles": 5, "settings": 3}
	for name, tc := range map[string]struct {
		after map[string]int64
		want  string
	}{
		"unchanged":           {after: map[string]int64{"articles": 5, "settings": 3}},
		"settings plus two":   {after: map[string]int64{"articles": 5, "settings": 5, "candidates": 0}},
		"settings plus one":   {after: map[string]int64{"articles": 5, "settings": 4}},
		"settings plus three": {after: map[string]int64{"articles": 5, "settings": 6}, want: "table settings gained unexpected rows: 3 -> 6"},
		"articles plus one":   {after: map[string]int64{"articles": 6, "settings": 3}, want: "table articles gained unexpected rows: 5 -> 6"},
		"articles lost a row": {after: map[string]int64{"articles": 4, "settings": 3}, want: "table articles lost rows: 5 -> 4"},
		"articles table gone": {after: map[string]int64{"settings": 3}, want: "table articles is missing"},
	} {
		t.Run(name, func(t *testing.T) {
			err := compareCounts(before, tc.after)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("compareCounts() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("compareCounts() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAdoptionRefusesWhenRowCountsDifferFromTheBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "technews.db")
	db, err := database.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY); INSERT INTO notes VALUES (1), (2)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	descriptors := []migrate.Descriptor{{Version: 1, Name: "notes", SQL: `CREATE TABLE notes (id INTEGER PRIMARY KEY);`}}
	reference, err := BuildReference(ctx, descriptors, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The backup had one row, the database now has two: a writer got in
	// between. Adoption must refuse inside its transaction.
	_, err = adoptAndCheck(ctx, path, descriptors, reference, map[string]int64{"notes": 1})
	if err == nil || !strings.Contains(err.Error(), "is another process using it?") {
		t.Fatalf("adoptAndCheck() error = %v", err)
	}
	check, err := database.OpenExisting(ctx, path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	managed, err := hasLedger(ctx, check)
	if err != nil || managed {
		t.Fatalf("refused adoption left a ledger: %v %v", managed, err)
	}

	// With the right counts it adopts.
	if _, err := adoptAndCheck(ctx, path, descriptors, reference, map[string]int64{"notes": 2}); err != nil {
		t.Fatalf("adoptAndCheck() with matching counts: %v", err)
	}
}
