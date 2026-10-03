package telegram_test

import (
	"testing"
	"time"

	"github.com/ozankasikci/aiandtechnews/apps/server-go/internal/telegram"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 10, 3, hour, minute, 0, 0, time.UTC)
}

func TestParseQuietHours(t *testing.T) {
	quiet, err := telegram.ParseQuietHours(" 00:00-08:00 ")
	if err != nil || !quiet.Enabled() || quiet.String() != "00:00-08:00" {
		t.Fatalf("quiet = %v err = %v", quiet, err)
	}
	for clock, want := range map[time.Time]bool{
		at(0, 0): true, at(7, 59): true, at(8, 0): false, at(12, 0): false, at(23, 59): false,
	} {
		if quiet.Contains(clock) != want {
			t.Errorf("Contains(%s) = %t", clock.Format("15:04"), !want)
		}
	}
	for _, off := range []string{"", "  ", "off", "OFF", "none"} {
		if quiet, err := telegram.ParseQuietHours(off); err != nil || quiet.Enabled() || quiet.Contains(at(3, 0)) {
			t.Errorf("ParseQuietHours(%q) = %v, %v", off, quiet, err)
		}
	}
	for _, bad := range []string{"08:00", "8-9", "24:00-08:00", "00:00-08:60", "08:00-08:00", "ab:cd-08:00"} {
		if _, err := telegram.ParseQuietHours(bad); err == nil {
			t.Errorf("ParseQuietHours(%q) error = nil", bad)
		}
	}
}

func TestQuietHoursWrapMidnight(t *testing.T) {
	quiet, err := telegram.ParseQuietHours("22:30-06:15")
	if err != nil {
		t.Fatal(err)
	}
	for clock, want := range map[time.Time]bool{
		at(22, 29): false, at(22, 30): true, at(23, 59): true, at(0, 0): true, at(6, 14): true, at(6, 15): false, at(12, 0): false,
	} {
		if quiet.Contains(clock) != want {
			t.Errorf("Contains(%s) = %t", clock.Format("15:04"), !want)
		}
	}
}

func TestQuietHoursUseTheTimesZone(t *testing.T) {
	quiet, _ := telegram.ParseQuietHours("00:00-08:00")
	istanbul := time.FixedZone("TRT", 3*60*60)
	// 22:00 UTC is 01:00 in Istanbul.
	if !quiet.Contains(at(22, 0).In(istanbul)) || quiet.Contains(at(22, 0)) {
		t.Fatal("quiet hours ignore the zone")
	}
}
