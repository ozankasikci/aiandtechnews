package collector

import "testing"

func TestDecodeHTMLEntities(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "CDATA section is unwrapped",
			value: "<![CDATA[hello world]]>",
			want:  "hello world",
		},
		{
			name:  "hex numeric entity decodes an emoji",
			value: "grinning &#x1F600; face",
			want:  "grinning \U0001F600 face",
		},
		{
			name:  "out of range hex entity is left as-is",
			value: "bad &#xFFFFFFFF; entity",
			want:  "bad &#xFFFFFFFF; entity",
		},
		{
			name:  "named entity is matched case-insensitively",
			value: "Q&AMP;A",
			want:  "Q&A",
		},
		{
			name:  "whitespace collapses to single spaces",
			value: "  multiple   \n\t spaces  ",
			want:  "multiple spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeHTMLEntities(tt.value); got != tt.want {
				t.Errorf("DecodeHTMLEntities(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
