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
