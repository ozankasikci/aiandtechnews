package telegram

import (
	"fmt"
	"strings"
	"time"
)

// DefaultQuietHours is TELEGRAM_QUIET_HOURS when unset.
const DefaultQuietHours = "00:00-08:00"

// QuietHours is a daily window, in local wall-clock minutes, during which
// nothing is posted. Start is inclusive and End exclusive; a window whose
// End is not after Start wraps midnight ("22:00-07:00"). The zero value
// (and an empty setting) is no window at all.
type QuietHours struct {
	Start, End int // minutes after midnight
	enabled    bool
}

// ParseQuietHours reads "HH:MM-HH:MM" (24-hour clock). An empty value, "off"
// or "none" disables quiet hours; a window that starts and ends at the same
// minute is rejected, since it would be ambiguous (never or always).
func ParseQuietHours(value string) (QuietHours, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "off") || strings.EqualFold(value, "none") {
		return QuietHours{}, nil
	}
	from, to, ok := strings.Cut(value, "-")
	if !ok {
		return QuietHours{}, fmt.Errorf("quiet hours %q: want HH:MM-HH:MM", value)
	}
	start, err := parseClock(from)
	if err != nil {
		return QuietHours{}, fmt.Errorf("quiet hours %q: %w", value, err)
	}
	end, err := parseClock(to)
	if err != nil {
		return QuietHours{}, fmt.Errorf("quiet hours %q: %w", value, err)
	}
	if start == end {
		return QuietHours{}, fmt.Errorf("quiet hours %q: start and end are the same", value)
	}
	return QuietHours{Start: start, End: end, enabled: true}, nil
}

func parseClock(value string) (int, error) {
	clock, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid time %q, want HH:MM", strings.TrimSpace(value))
	}
	return clock.Hour()*60 + clock.Minute(), nil
}

// Enabled reports whether there is a window.
func (q QuietHours) Enabled() bool { return q.enabled }

// Contains reports whether t's wall clock (in t's location) is inside the
// window.
func (q QuietHours) Contains(t time.Time) bool {
	if !q.enabled {
		return false
	}
	minute := t.Hour()*60 + t.Minute()
	if q.Start < q.End {
		return minute >= q.Start && minute < q.End
	}
	return minute >= q.Start || minute < q.End
}

func (q QuietHours) String() string {
	if !q.enabled {
		return "off"
	}
	return fmt.Sprintf("%02d:%02d-%02d:%02d", q.Start/60, q.Start%60, q.End/60, q.End%60)
}
