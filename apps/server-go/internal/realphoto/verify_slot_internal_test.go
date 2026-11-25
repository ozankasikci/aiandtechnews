package realphoto

import "testing"

func TestUsableForAppliesTheFeaturedRules(t *testing.T) {
	good := Verdict{Match: MatchExact, Photographic: true, Prominent: true, Quality: MinFeaturedQuality}
	if ok, why := good.usableFor(SlotFeatured, ""); !ok {
		t.Fatalf("good featured verdict rejected: %s", why)
	}
	seven := good
	seven.Quality = MinFeaturedQuality - 1
	if ok, _ := seven.usableFor(SlotFeatured, ""); ok {
		t.Fatal("quality 7 must be rejected for the featured slot")
	}
	if ok, why := seven.usableFor(SlotInline, ""); !ok {
		t.Fatalf("quality 7 must still pass in the body: %s", why)
	}
	dated := good
	dated.Dated = true
	if ok, _ := dated.usableFor(SlotFeatured, ""); ok {
		t.Fatal("a dated photo must be rejected for the featured slot")
	}
	if ok, why := dated.usableFor(SlotInline, ""); !ok {
		t.Fatalf("the body ignores dated: %s", why)
	}
	branded := good
	branded.OtherBranding = true
	if ok, why := branded.usableFor(SlotFeatured, KindPerson); !ok {
		t.Fatalf("a person in front of an event backdrop must pass: %s", why)
	}
	if ok, _ := branded.usableFor(SlotFeatured, "place"); ok {
		t.Fatal("other branding is only allowed for a person")
	}
	if ok, _ := branded.usableFor(SlotInline, KindPerson); ok {
		t.Fatal("the body keeps rejecting other branding")
	}
}
