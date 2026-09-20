package content

import "testing"

func TestParseNodeIntMatchesParseIntPrefixesAndSaturatesWithSign(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"-999999999999999999999", -maxInt()},
		{"+999999999999999999999", maxInt()},
		{"2abc", 2},
		{"1.9", 1},
		{"0x10", 16},
		{"Infinity", 12},
		{"", 12},
		{"0", 12},
	}
	for _, tt := range tests {
		if got := parseNodeInt(tt.value, 12); got != tt.want {
			t.Errorf("parseNodeInt(%q, 12) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestPageOffsetSaturatesWithoutIntOverflow(t *testing.T) {
	const maxInt64 = int64(^uint64(0) >> 1)
	page := maxInt()
	var want int64
	if int64(page-1) > maxInt64/50 {
		want = maxInt64
	} else {
		want = int64(page-1) * 50
	}
	if got := pageOffset(page, 50); got != want {
		t.Fatalf("pageOffset(maxInt, 50) = %d, want %d", got, want)
	}
	if got := pageOffset(2, 12); got != 12 {
		t.Fatalf("pageOffset(2, 12) = %d, want 12", got)
	}
}
