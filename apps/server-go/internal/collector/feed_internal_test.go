package collector

import (
	"testing"
	"time"
)

func TestParseFeedDate(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Time
		ok    bool
	}{
		{
			name:  "named zone EDT",
			value: "Tue, 23 Sep 2026 10:00:00 EDT",
			want:  time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "named zone PST",
			value: "Tue, 23 Sep 2026 10:00:00 PST",
			want:  time.Date(2026, 9, 23, 18, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "named zone GMT",
			value: "Tue, 23 Sep 2026 10:00:00 GMT",
			want:  time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "named zone UT",
			value: "Tue, 23 Sep 2026 10:00:00 UT",
			want:  time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "numeric offset without seconds",
			value: "Tue, 23 Sep 2026 10:00 +0000",
			want:  time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "no weekday with named zone",
			value: "23 Sep 2026 10:00:00 GMT",
			want:  time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "ISO 8601 with compact numeric offset",
			value: "2026-09-23T10:00:00+0000",
			want:  time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "RFC3339 with offset",
			value: "2026-09-23T10:00:00+05:00",
			want:  time.Date(2026, 9, 23, 5, 0, 0, 0, time.UTC),
			ok:    true,
		},
		{
			name:  "garbage",
			value: "not a date",
			ok:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseFeedDate(tt.value)
			if ok != tt.ok {
				t.Fatalf("parseFeedDate(%q) ok = %v, want %v", tt.value, ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if !got.Equal(tt.want) {
				t.Fatalf("parseFeedDate(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
