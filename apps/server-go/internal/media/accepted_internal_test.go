package media

import "testing"

func TestAcceptedImageComparesExtensionsAsASCIIOnly(t *testing.T) {
	for _, tt := range []struct {
		mime, name string
		want       bool
	}{
		{"image/gif", "a.GIF", true},
		{"image/jpeg", "a.JpEg", true},
		{"image/gif", "a.GİF", false},  // U+0130 lowercases to "i" in Go
		{"image/png", "a.pnǵ", false},  // non-ASCII lookalike
		{"image/jpeg", "a.ｊｐｇ", false}, // fullwidth letters
		{"image/webp", "a.WEBP​", false},
	} {
		if got := acceptedImage(tt.mime, tt.name); got != tt.want {
			t.Errorf("acceptedImage(%q, %q) = %v, want %v", tt.mime, tt.name, got, tt.want)
		}
	}
}
