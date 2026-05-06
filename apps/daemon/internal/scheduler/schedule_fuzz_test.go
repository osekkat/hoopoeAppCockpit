package scheduler

import (
	"errors"
	"testing"
	"time"
)

// FuzzParseSchedule asserts ParseSchedule never panics, that valid outputs
// round-trip through Schedule.String + Validate, and that cron expressions
// produced from fuzz input keep cronExpr.Next bounded — the
// validateDayMonthCombination guard rejects expressions like '* * 31 2 *'
// that would otherwise walk the full 5-year deadline (~2.6M minute steps).
func FuzzParseSchedule(f *testing.F) {
	seeds := []string{
		"",
		"on demand",
		"on_demand",
		"every 5m",
		"every 1h",
		"every 7d",
		"every 30s",
		"every 250ms",
		"on event: vps_commit_created",
		"on event: bead_status_changed",
		"* * * * *",
		"0 12 * * *",
		"*/15 * * * *",
		"0 9-17 * * 1-5",
		"0 0 1 1 *",
		"0 12 15 * 1",
		// Invalid / edge inputs that must not panic and must surface as errors:
		"every",
		"every 0s",
		"every -5m",
		"on event:",
		"every garbage",
		"6 6 6 6 6 6",
		"* * 31 2 *",
		"* * 31 4,6,9,11 *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * * 13 *",
		"* * * * 7",
		"*/0 * * * *",
		"5-3 * * * *",
		"a b c d e",
		" \t\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	deadline := time.Date(2026, time.May, 7, 0, 0, 0, 0, time.UTC)

	f.Fuzz(func(t *testing.T, raw string) {
		schedule, err := ParseSchedule(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("ParseSchedule(%q) returned non-domain error %v", raw, err)
			}
			if schedule != (Schedule{}) {
				t.Fatalf("ParseSchedule(%q) returned %+v with error %v — must zero on error", raw, schedule, err)
			}
			return
		}

		if err := schedule.Validate(); err != nil {
			t.Fatalf("ParseSchedule(%q) succeeded but Validate failed: %v (schedule=%+v)", raw, err, schedule)
		}

		switch schedule.Type {
		case ScheduleOnDemand, ScheduleInterval, ScheduleEvent, ScheduleCron:
		default:
			t.Fatalf("ParseSchedule(%q) produced unknown type %q", raw, schedule.Type)
		}

		// Round-trip: String → ParseSchedule → equal type + content.
		if schedule.Type != ScheduleOnDemand || (schedule.Type == ScheduleOnDemand && schedule.String() != "") {
			rendered := schedule.String()
			roundTrip, rtErr := ParseSchedule(rendered)
			if rtErr != nil {
				t.Fatalf("round-trip ParseSchedule(%q → %q) failed: %v", raw, rendered, rtErr)
			}
			if roundTrip.Type != schedule.Type {
				t.Fatalf("round-trip type drift: %q → %q produced %q (orig %q)", raw, rendered, roundTrip.Type, schedule.Type)
			}
			switch schedule.Type {
			case ScheduleInterval:
				if roundTrip.Interval != schedule.Interval {
					t.Fatalf("round-trip interval drift: %v → %v", schedule.Interval, roundTrip.Interval)
				}
			case ScheduleEvent:
				if roundTrip.Event != schedule.Event {
					t.Fatalf("round-trip event drift: %q → %q", schedule.Event, roundTrip.Event)
				}
			case ScheduleCron:
				if roundTrip.Cron != schedule.Cron {
					t.Fatalf("round-trip cron drift: %q → %q", schedule.Cron, roundTrip.Cron)
				}
			}
		}

		// NextAfter must remain bounded for any schedule the parser accepts.
		// For cron, this is the load-bearing assertion of hp-qq4h: without
		// validateDayMonthCombination an impossible expression would walk
		// ~2.6M minute steps. Wall-budget here is generous (200ms) but bounds
		// the search; on a healthy host a single Next() returns in < 1ms.
		started := time.Now()
		next, nextErr := schedule.NextAfter(deadline)
		elapsed := time.Since(started)
		if nextErr != nil {
			t.Fatalf("NextAfter for accepted schedule %+v failed: %v", schedule, nextErr)
		}
		if elapsed > 200*time.Millisecond {
			t.Fatalf("NextAfter took %s for schedule %+v (raw %q) — cron walk should be bounded", elapsed, schedule, raw)
		}
		switch schedule.Type {
		case ScheduleEvent, ScheduleOnDemand:
			if !next.IsZero() {
				t.Fatalf("NextAfter for %s schedule should be zero, got %v", schedule.Type, next)
			}
		case ScheduleInterval:
			if !next.After(deadline) {
				t.Fatalf("NextAfter for interval schedule %+v should advance past %v, got %v", schedule, deadline, next)
			}
		case ScheduleCron:
			if !next.IsZero() && !next.After(deadline) {
				t.Fatalf("NextAfter for cron schedule %+v should be zero or > deadline, got %v", schedule, next)
			}
		}
	})
}
