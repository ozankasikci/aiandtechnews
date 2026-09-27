package realphoto

import (
	"context"
	"slices"
	"testing"
)

func TestVerdictRejectsSmallSubjectsAndOtherBranding(t *testing.T) {
	good := Verdict{Match: MatchExact, Photographic: true, Prominent: true, Quality: MinQuality}
	if ok, why := good.usable(KindCommons); !ok {
		t.Fatalf("good verdict rejected: %s", why)
	}
	small := good
	small.Prominent = false
	if ok, _ := small.usable(KindCommons); ok {
		t.Fatal("a small, unfocused subject must be rejected")
	}
	branded := good
	branded.OtherBranding = true
	if ok, _ := branded.usable(KindOfficial); ok {
		t.Fatal("other brands' signs must be rejected")
	}
	weak := good
	weak.Quality = MinQuality - 1
	if ok, _ := weak.usable(KindCommons); ok {
		t.Fatal("quality below the minimum must be rejected")
	}
}

func TestParsePlanKeepsOnlyMakerPagesOnTheMakersDomain(t *testing.T) {
	plan, err := ParsePlan(`{"photographable":true,"subject":"NVIDIA RTX PRO 6000","queries":["RTX PRO 6000"],"maker":"NVIDIA","maker_domain":"www.nvidia.com",
		"official_pages":["https://www.nvidia.com/en-us/products/rtx-pro-6000/","https://www.engadget.com/nvidia-rtx","http://nvidia.com/insecure","https://nvidia.com.evil.test/x","https://nvidianews.nvidia.com/news/rtx"]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://www.nvidia.com/en-us/products/rtx-pro-6000/", "https://nvidianews.nvidia.com/news/rtx"}
	if plan.MakerDomain != "nvidia.com" || len(plan.OfficialPages) != 2 || plan.OfficialPages[0] != want[0] || plan.OfficialPages[1] != want[1] {
		t.Fatalf("plan = %+v", plan)
	}
	if onMakerDomain("https://engadget.com/x", "engadget.com") {
		t.Fatal("a news site can never be a maker's site")
	}
}

type fixedSearch struct{ answer string }

func (s fixedSearch) SearchText(context.Context, string) (string, error) { return s.answer, nil }

func TestMakerPagesComeFromSearchOnTheMakersDomain(t *testing.T) {
	f := NewFinder(nil, nil, nil, nil).WithSearch(fixedSearch{"Here you go:\nhttps://www.nvidia.com/en-us/products/rtx-pro-6000/.\nhttps://www.engadget.com/nvidia\nhttps://nvidianews.nvidia.com/news/rtx-pro"})
	plan := Plan{Subject: "RTX PRO 6000", Maker: "NVIDIA", MakerDomain: "nvidia.com", OfficialPages: []string{"https://www.nvidia.com/guess/"}}
	got := f.makerPages(context.Background(), plan)
	want := []string{"https://www.nvidia.com/en-us/products/rtx-pro-6000/", "https://nvidianews.nvidia.com/news/rtx-pro", "https://www.nvidia.com/guess/"}
	if !slices.Equal(got, want) {
		t.Fatalf("pages = %v", got)
	}
	plan.Kind = "person"
	if pages := f.makerPages(context.Background(), plan); len(pages) != 0 {
		t.Fatalf("a person's story must not look up maker pages: %v", pages)
	}
}
