package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/realphoto"
)

func TestRenderReportShowsChosenAndRejectedPhotos(t *testing.T) {
	rows := []reportRow{
		{
			Slug: "sierra-raises", Title: "Sierra raises <$1.75 billion>", Current: "https://img.test/features/sierra.webp",
			Found: true, Image: "sierra-raises.png", Credit: "Photo: TechCrunch / CC BY 2.0, via Wikimedia Commons",
			CreditURL: "https://commons.wikimedia.org/wiki/File:Bret.jpg", StandIn: "Bret Taylor", Quality: 9,
			Reason: "Wikimedia Commons photo of stand-in Bret Taylor (person), score 9",
			Candidates: []realphoto.Candidate{
				{Source: "commons", PageURL: "https://commons.wikimedia.org/wiki/File:Bret.jpg", Usable: true, Verdict: &realphoto.Verdict{Quality: 9}},
				{Source: "commons", PageURL: "https://commons.wikimedia.org/wiki/File:Old.jpg", Reason: "visibly dated"},
			},
		},
		{Slug: "a-study", Title: "A study", Current: "https://img.test/features/study.webp", Reason: "no photographable subject: a study"},
	}
	var out bytes.Buffer
	if err := renderReport(&out, rows); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		"1 of 2 articles got a photo",
		"Sierra raises &lt;$1.75 billion&gt;",
		`src="sierra-raises.png"`,
		"Photo: TechCrunch / CC BY 2.0, via Wikimedia Commons",
		"Stand-in: Bret Taylor",
		"visibly dated",
		"no photographable subject: a study",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
