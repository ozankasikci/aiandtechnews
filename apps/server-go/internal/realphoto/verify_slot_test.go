package realphoto_test

import (
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestParseVerdictReadsDatedAndFocus(t *testing.T) {
	raw := `{"match":"exact","photographic":true,"watermark":false,"text_heavy":false,"marketing_banner":false,"misleading":false,"subject_prominent":true,"other_branding":false,"dated":true,"focus_x":0.3,"focus_y":1.7,"quality":9,"alt":"A.","notes":"n"}`
	got, err := realphoto.ParseVerdict(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Dated || got.FocusX != 0.3 || got.FocusY != 1 {
		t.Fatalf("verdict = %+v, want dated, focus 0.3 and 1 (clamped)", got)
	}
}

func TestParseVerdictDefaultsTheFocusToTheCentre(t *testing.T) {
	got, err := realphoto.ParseVerdict(verdict(8, "A robot."))
	if err != nil {
		t.Fatal(err)
	}
	if got.Dated || got.FocusX != 0.5 || got.FocusY != 0.5 {
		t.Fatalf("verdict = %+v, want not dated and a centred focus", got)
	}
}

func TestVerifyPromptForNamesTheSlot(t *testing.T) {
	plan := realphoto.Plan{Subject: "Bret Taylor", Maker: "Sierra"}
	featured := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindCommons, realphoto.SlotFeatured)
	inline := realphoto.VerifyPromptFor(plan, "Headline", realphoto.KindCommons, realphoto.SlotInline)
	if !strings.Contains(featured, "lead picture") || strings.Contains(featured, "in-body picture") {
		t.Fatalf("featured prompt = %q", featured)
	}
	if !strings.Contains(inline, "in-body picture") {
		t.Fatalf("inline prompt = %q", inline)
	}
	for _, field := range []string{"dated:", "focus_x, focus_y:", `"dated":false`, `"focus_x":0.5`} {
		if !strings.Contains(featured, field) {
			t.Errorf("prompt lacks %q", field)
		}
	}
	if realphoto.VerifyPrompt(plan, "Headline", realphoto.KindCommons) != inline {
		t.Fatal("VerifyPrompt must stay the in-body prompt")
	}
}
