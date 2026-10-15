package illustration_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"

	_ "modernc.org/sqlite"
)

func TestSettingsHistoryKeepsTheLatestChoices(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	history := illustration.NewSettingsHistory(db)
	if recent, err := history.Recent(ctx); err != nil || len(recent) != 0 {
		t.Fatalf("empty history = %v %v", recent, err)
	}
	for i := 0; i < 12; i++ {
		if err := history.Record(ctx, illustration.Choice{Style: "graphic", Palette: string(rune('a' + i)), Composition: "scene"}); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := history.Recent(ctx)
	if err != nil || len(recent) != 10 || recent[0].Palette != "c" || recent[9].Palette != "l" {
		t.Fatalf("recent = %+v %v", recent, err)
	}
}

func TestVarietyFrom(t *testing.T) {
	scene := func(style, palette string) illustration.Choice {
		return illustration.Choice{Style: style, Palette: palette, Composition: illustration.CompositionScene}
	}
	v := illustration.VarietyFrom([]illustration.Choice{scene("graphic", "a"), scene("midcentury", "b"), scene("graphic", "c"), scene("midcentury", "d")})
	if !slices.Equal(v.AvoidStyles, []string{"graphic", "midcentury"}) || !slices.Equal(v.AvoidPalettes, []string{"b", "c", "d"}) || v.PreferComposition != illustration.CompositionSimple {
		t.Fatalf("variety = %+v", v)
	}
	mixed := illustration.VarietyFrom([]illustration.Choice{scene("graphic", "a"), {Style: "graphic", Palette: "b", Composition: illustration.CompositionSimple}})
	if mixed.PreferComposition != "" {
		t.Fatalf("mixed run should not prefer: %+v", mixed)
	}
	if empty := illustration.VarietyFrom(nil); len(empty.AvoidStyles)+len(empty.AvoidPalettes) != 0 || empty.PreferComposition != "" {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestVarietyAvoidsTheLatestShots(t *testing.T) {
	shot := func(name string) illustration.Choice { return illustration.Choice{Style: "graphic", Shot: name} }
	v := illustration.VarietyFrom([]illustration.Choice{shot("wide"), shot("close-up"), {Style: "graphic"}, shot("split"), shot("inset")})
	if !slices.Equal(v.AvoidShots, []string{"split", "inset"}) {
		t.Fatalf("avoid shots = %v", v.AvoidShots)
	}
}

func TestVarietyCarriesRecentScenes(t *testing.T) {
	v := illustration.VarietyFrom([]illustration.Choice{{Style: "graphic", Scene: "a woman at a desk"}, {Style: "neon"}, {Style: "graphic", Scene: "a burning data centre"}})
	if !slices.Equal(v.RecentScenes, []string{"a woman at a desk", "a burning data centre"}) {
		t.Fatalf("recent scenes = %v", v.RecentScenes)
	}
}

func TestSceneNoteIsBounded(t *testing.T) {
	note := illustration.SceneNote(illustration.Brief{Foreground: strings.Repeat("word ", 100), Background: "a hall"})
	if len(note) > 240 || !strings.HasPrefix(note, "word word") {
		t.Fatalf("note = %q (%d)", note, len(note))
	}
}

func TestRecentScenesRule(t *testing.T) {
	if rule := illustration.RecentScenesRule(nil); rule != "" {
		t.Fatalf("rule without scenes = %q", rule)
	}
	rule := illustration.RecentScenesRule([]string{"a woman at a desk"})
	if !strings.Contains(rule, "- a woman at a desk\n") || !strings.Contains(rule, "clearly different") {
		t.Fatalf("rule = %q", rule)
	}
}

func TestSettingsHistoryAtKeepsItsOwnKey(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	covers := illustration.NewSettingsHistory(db)
	inline := illustration.NewSettingsHistoryAt(db, illustration.InlineHistoryKey)
	if err := covers.Record(ctx, illustration.Choice{Style: "graphic", Scene: "a cover"}); err != nil {
		t.Fatal(err)
	}
	if err := inline.Record(ctx, illustration.Choice{Style: "neon", Scene: "an inline image"}); err != nil {
		t.Fatal(err)
	}
	gotCovers, _ := covers.Recent(ctx)
	gotInline, _ := inline.Recent(ctx)
	if len(gotCovers) != 1 || gotCovers[0].Scene != "a cover" || len(gotInline) != 1 || gotInline[0].Scene != "an inline image" {
		t.Fatalf("covers = %+v, inline = %+v", gotCovers, gotInline)
	}
}
