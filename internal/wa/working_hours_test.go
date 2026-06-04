package wa

import (
	"testing"
	"time"
)

func TestDesiredMute(t *testing.T) {
	// Base enabled config: Sun-Thu 09:00-18:00
	baseCfg := WorkingHoursConfig{
		Enabled:     true,
		Start:       "09:00",
		End:         "18:00",
		WorkingDays: []int{0, 1, 2, 3, 4}, // Sun=0, Mon=1, Tue=2, Wed=3, Thu=4
	}

	loc := time.Local

	// 2026-06-07 is a Sunday (weekday=0)
	// 2026-06-05 is a Friday (weekday=5)

	tests := []struct {
		name string
		now  time.Time
		cfg  WorkingHoursConfig
		want bool
	}{
		{
			name: "Sunday 12:00 inside window -> true",
			now:  time.Date(2026, 6, 7, 12, 0, 0, 0, loc),
			cfg:  baseCfg,
			want: true,
		},
		{
			name: "Sunday 08:59 before start -> false",
			now:  time.Date(2026, 6, 7, 8, 59, 0, 0, loc),
			cfg:  baseCfg,
			want: false,
		},
		{
			name: "Sunday 18:00 at end (half-open) -> false",
			now:  time.Date(2026, 6, 7, 18, 0, 0, 0, loc),
			cfg:  baseCfg,
			want: false,
		},
		{
			name: "Sunday 09:00 at start -> true",
			now:  time.Date(2026, 6, 7, 9, 0, 0, 0, loc),
			cfg:  baseCfg,
			want: true,
		},
		{
			name: "Sunday 17:59 just before end -> true",
			now:  time.Date(2026, 6, 7, 17, 59, 0, 0, loc),
			cfg:  baseCfg,
			want: true,
		},
		{
			name: "Friday 12:00 weekend day (5 not in list) -> false",
			now:  time.Date(2026, 6, 5, 12, 0, 0, 0, loc),
			cfg:  baseCfg,
			want: false,
		},
		{
			name: "Enabled=false -> false",
			now:  time.Date(2026, 6, 7, 12, 0, 0, 0, loc),
			cfg: WorkingHoursConfig{
				Enabled:     false,
				Start:       "09:00",
				End:         "18:00",
				WorkingDays: []int{0, 1, 2, 3, 4},
			},
			want: false,
		},
		{
			name: "Invalid window Start>=End on Sunday 12:00 -> false",
			now:  time.Date(2026, 6, 7, 12, 0, 0, 0, loc),
			cfg: WorkingHoursConfig{
				Enabled:     true,
				Start:       "18:00",
				End:         "09:00",
				WorkingDays: []int{0, 1, 2, 3, 4},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify weekday assumption in test setup
			got := DesiredMute(tt.now, tt.cfg)
			if got != tt.want {
				t.Errorf("DesiredMute(%v, cfg) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestDesiredMuteDateWeekdays(t *testing.T) {
	// Sanity-check that our chosen dates have the expected weekdays.
	sunday := time.Date(2026, 6, 7, 0, 0, 0, 0, time.Local)
	friday := time.Date(2026, 6, 5, 0, 0, 0, 0, time.Local)

	if sunday.Weekday() != time.Sunday {
		t.Errorf("2026-06-07 expected Sunday, got %v", sunday.Weekday())
	}
	if friday.Weekday() != time.Friday {
		t.Errorf("2026-06-05 expected Friday, got %v", friday.Weekday())
	}
}
