package content

import "testing"

func TestParseNodeNumberMatchesParseIntAndJavaScriptNumber(t *testing.T) {
	tests := []struct {
		value string
		want  float64
	}{
		{"9223372036854775808", 9223372036854775808},
		{"999999999999999999999", 1e21},
		{"-999999999999999999999", -1e21},
		{"+42", 42},
		{"2abc", 2},
		{"1.9", 1},
		{"0x10", 16},
		{"Infinity", 12},
		{" 	17 trailing", 17},
		{"", 12},
		{"0", 12},
	}
	for _, tt := range tests {
		if got := parseNodeNumber(tt.value, 12); got != tt.want {
			t.Errorf("parseNodeNumber(%q, 12) = %g, want %g", tt.value, got, tt.want)
		}
	}
}

func TestPageOffsetPreservesJavaScriptNumberSemantics(t *testing.T) {
	if got := pageOffset(9223372036854775808, 12); got != 1.1068046444225731e20 {
		t.Fatalf("pageOffset(9223372036854775808, 12) = %g", got)
	}
	if got := pageOffset(2, 12); got != 12 {
		t.Fatalf("pageOffset(2, 12) = %g, want 12", got)
	}
}
