package realphoto

import "testing"

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
