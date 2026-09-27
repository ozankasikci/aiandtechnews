package illustration_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration"
	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/illustration/styles"
)

var styleNames = []string{"collage", "graphic", "midcentury", "photo"}

const validBrief = `{"scene":"A glowing helix unwinds like a map.","foreground":"A lantern over one bright rung.","background":"Night sky of nodes.","mood":"Curious","style":"graphic","style_reason":"Warm and human.","public_figure":null}`

func TestParseBriefAcceptsCleanAndFencedJSON(t *testing.T) {
	for name, raw := range map[string]string{
		"plain":        validBrief,
		"fenced":       "```json\n" + validBrief + "\n```",
		"bare fence":   "```\n" + validBrief + "\n```",
		"prose around": "Here is the brief:\n" + validBrief + "\nThanks.",
	} {
		brief, err := illustration.ParseBrief(raw, styleNames)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if brief.Style != "graphic" || brief.PublicFigure != nil || brief.WantsCollage() {
			t.Fatalf("%s: brief = %+v", name, brief)
		}
		if !strings.HasPrefix(brief.Text(), "Scene: A glowing helix") || !strings.Contains(brief.Text(), "Mood: Curious") {
			t.Fatalf("%s: text = %q", name, brief.Text())
		}
	}
}

func TestParseBriefPublicFigure(t *testing.T) {
	raw := strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":"Dario Amodei","visible_in_source":true}`, 1)
	raw = strings.Replace(raw, "A lantern over one bright rung.", "Dario Amodei holds a lantern.", 1)
	brief, err := illustration.ParseBrief(raw, styleNames)
	if err != nil {
		t.Fatal(err)
	}
	if !brief.WantsCollage() || brief.PublicFigure.Name != "Dario Amodei" {
		t.Fatalf("brief = %+v", brief)
	}
	if strings.Contains(brief.Foreground, "Dario") {
		t.Fatalf("the name must not reach the image prompt: %q", brief.Foreground)
	}

	notVisible := strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":"Sam Altman","visible_in_source":false}`, 1)
	if brief, err := illustration.ParseBrief(notVisible, styleNames); err != nil || brief.WantsCollage() || brief.PublicFigure == nil {
		t.Fatalf("not visible: %+v %v", brief, err)
	}
	unnamed := strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":" ","visible_in_source":true}`, 1)
	if brief, err := illustration.ParseBrief(unnamed, styleNames); err != nil || brief.PublicFigure != nil {
		t.Fatalf("unnamed: %+v %v", brief, err)
	}
}

func TestParseBriefRejectsInvalidAnswers(t *testing.T) {
	cases := map[string]string{
		"not json":         "I cannot help with that.",
		"missing scene":    strings.Replace(validBrief, `"scene":"A glowing helix unwinds like a map.",`, "", 1),
		"missing figure":   strings.Replace(validBrief, `,"public_figure":null`, "", 1),
		"empty mood":       strings.Replace(validBrief, `"mood":"Curious"`, `"mood":"  "`, 1),
		"unknown style":    strings.Replace(validBrief, `"style":"graphic"`, `"style":"oil"`, 1),
		"unknown field":    strings.Replace(validBrief, `"mood":"Curious"`, `"mood":"Curious","lighting":"red"`, 1),
		"wrong type":       strings.Replace(validBrief, `"mood":"Curious"`, `"mood":3`, 1),
		"figure no flag":   strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":"X"}`, 1),
		"figure bad flag":  strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":"X","visible_in_source":"yes"}`, 1),
		"figure extra key": strings.Replace(validBrief, `"public_figure":null`, `"public_figure":{"name":"X","visible_in_source":true,"role":"CEO"}`, 1),
		"too long":         strings.Replace(validBrief, "Curious", strings.Repeat("a", 700), 1),
	}
	for name, raw := range cases {
		if _, err := illustration.ParseBrief(raw, styleNames); !errors.Is(err, illustration.ErrInvalidBrief) {
			t.Errorf("%s: err = %v, want ErrInvalidBrief", name, err)
		}
	}
}

func TestParseBriefNormalizesStyleCase(t *testing.T) {
	brief, err := illustration.ParseBrief(strings.Replace(validBrief, `"style":"graphic"`, `"style":" Photo "`, 1), styleNames)
	if err != nil || brief.Style != "photo" {
		t.Fatalf("brief = %+v err = %v", brief, err)
	}
}

func TestAnalyzePromptOffersEveryStyleAndForbidsBrands(t *testing.T) {
	catalog := styles.MustLoad()
	prompt := illustration.BuildAnalyzePrompt("Headline X", "Summary Y", "https://cdn.test/ceo-jane-doe.png", catalog, true, nil, nil, "", nil, nil)
	for _, want := range []string{"Headline: Headline X", "Summary: Summary Y", `"graphic"`, "public_figure", "visible_in_source",
		"Never ask for logos", "flags, national emblems or coats of arms", "showing literally what happened", "Never replace the story with a visual metaphor", "Never set it for private individuals", "attached image", "ceo-jane-doe.png", "do not need to recognize the face"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("analyze prompt lacks %q", want)
		}
	}
	if noImage := illustration.BuildAnalyzePrompt("H", "S", "", catalog, false, nil, nil, "", nil, nil); !strings.Contains(noImage, "No source image is available") {
		t.Error("text-only prompt should say there is no image")
	}
}

func TestBriefTextAddsTheFraming(t *testing.T) {
	brief := illustration.Brief{Scene: "s", Foreground: "f", Background: "b", Mood: "m", Shot: illustration.ShotWide}
	if text := brief.Text(); !strings.Contains(text, "Framing: a very wide establishing view") {
		t.Fatalf("text = %q", text)
	}
	if text := (illustration.Brief{Scene: "s", Shot: "dutch-angle"}).Text(); strings.Contains(text, "Framing") {
		t.Fatalf("unknown shot framed: %q", text)
	}
}
