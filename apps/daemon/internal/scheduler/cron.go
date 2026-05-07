package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronExpr struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
	// dayOfMonthRestricted / dayOfWeekRestricted track whether the raw
	// cron token was anything other than "*". POSIX/Vixie cron specifies
	// that when BOTH day fields are restricted, the expression matches a
	// day satisfying EITHER field (UNION), not both (intersection). The
	// asterisk-wildcard form is treated as "no day-side restriction" so
	// '0 12 * * 1' (every Monday at 12:00) and '0 12 15 * *' (15th at
	// 12:00) keep their natural single-axis semantics. Step (`*/N`) and
	// explicit ranges/lists count as restricted, matching Vixie cron's
	// asterisk check on the source token (not on map fullness).
	dayOfMonthRestricted bool
	dayOfWeekRestricted  bool
}

type cronField struct {
	allowed map[int]bool
	min     int
	max     int
}

func parseCron(expr string) (cronExpr, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronExpr{}, fmt.Errorf("%w: cron expression must have five fields", ErrInvalidDefinition)
	}
	minute, err := parseCronField(parts[0], 0, 59)
	if err != nil {
		return cronExpr{}, fmt.Errorf("%w: minute: %v", ErrInvalidDefinition, err)
	}
	hour, err := parseCronField(parts[1], 0, 23)
	if err != nil {
		return cronExpr{}, fmt.Errorf("%w: hour: %v", ErrInvalidDefinition, err)
	}
	dom, err := parseCronField(parts[2], 1, 31)
	if err != nil {
		return cronExpr{}, fmt.Errorf("%w: day-of-month: %v", ErrInvalidDefinition, err)
	}
	month, err := parseCronField(parts[3], 1, 12)
	if err != nil {
		return cronExpr{}, fmt.Errorf("%w: month: %v", ErrInvalidDefinition, err)
	}
	dow, err := parseCronField(parts[4], 0, 6)
	if err != nil {
		return cronExpr{}, fmt.Errorf("%w: day-of-week: %v", ErrInvalidDefinition, err)
	}
	dayOfMonthRestricted := strings.TrimSpace(parts[2]) != "*"
	dayOfWeekRestricted := strings.TrimSpace(parts[4]) != "*"
	if err := validateDayMonthCombination(dom, month, dayOfWeekRestricted); err != nil {
		return cronExpr{}, fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	return cronExpr{
		minute:               minute,
		hour:                 hour,
		dayOfMonth:           dom,
		month:                month,
		dayOfWeek:            dow,
		dayOfMonthRestricted: dayOfMonthRestricted,
		dayOfWeekRestricted:  dayOfWeekRestricted,
	}, nil
}

// validateDayMonthCombination rejects expressions like '* * 31 2 *'
// (Feb 31, never matches) or '* * 31 4,6,9,11 *' (31st of months
// without a 31st day). Without this guard, cronExpr.Next walks the
// full 5-year deadline (~2.6M minute steps) on every recompute,
// holding r.mu and spiking CPU. Day-of-month and month are validated
// together because each is structurally valid in isolation.
//
// hp-kxy0: when day-of-week is restricted, POSIX/Vixie cron UNION
// semantics let the DOW axis supply matching days regardless of
// whether DOM × month is feasible on its own. e.g. '* * 31 2 1'
// matches every Monday in Feb (DOW axis) even though "Feb 31"
// (DOM × month axis) never matches. Skip the DOM × month
// infeasibility rejection in that case — the expression is
// reachable via the DOW axis. Only reject when DOW is unrestricted
// and DOM × month is the only matching axis; that's the case where
// cronExpr.Next would otherwise spin to the 5-year deadline.
func validateDayMonthCombination(dom, month cronField, dayOfWeekRestricted bool) error {
	if dayOfWeekRestricted {
		return nil
	}
	daysInMonth := []int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	for m := range month.allowed {
		if m < 1 || m > 12 {
			continue
		}
		maxDay := daysInMonth[m-1]
		for d := range dom.allowed {
			if d <= maxDay {
				return nil
			}
		}
	}
	return fmt.Errorf("no valid day-of-month / month combination (e.g. day=31 with months containing 30 or fewer days, day=30/31 with month=2)")
}

func parseCronField(raw string, min int, max int) (cronField, error) {
	field := cronField{allowed: make(map[int]bool), min: min, max: max}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return cronField{}, fmt.Errorf("empty field part")
		}
		step := 1
		if before, after, ok := strings.Cut(part, "/"); ok {
			part = before
			parsed, err := strconv.Atoi(after)
			if err != nil || parsed <= 0 {
				return cronField{}, fmt.Errorf("invalid step %q", after)
			}
			step = parsed
		}
		start, end, err := cronRange(part, min, max)
		if err != nil {
			return cronField{}, err
		}
		for value := start; value <= end; value += step {
			field.allowed[value] = true
		}
	}
	return field, nil
}

func cronRange(raw string, min int, max int) (int, int, error) {
	if raw == "*" {
		return min, max, nil
	}
	if startRaw, endRaw, ok := strings.Cut(raw, "-"); ok {
		start, err := parseCronNumber(startRaw, min, max)
		if err != nil {
			return 0, 0, err
		}
		end, err := parseCronNumber(endRaw, min, max)
		if err != nil {
			return 0, 0, err
		}
		if start > end {
			return 0, 0, fmt.Errorf("range start %d after end %d", start, end)
		}
		return start, end, nil
	}
	value, err := parseCronNumber(raw, min, max)
	if err != nil {
		return 0, 0, err
	}
	return value, value, nil
}

func parseCronNumber(raw string, min int, max int) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", raw)
	}
	if value < min || value > max {
		return 0, fmt.Errorf("value %d outside %d-%d", value, min, max)
	}
	return value, nil
}

func (c cronExpr) Next(after time.Time) time.Time {
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	deadline := candidate.AddDate(5, 0, 0)
	for !candidate.After(deadline) {
		if c.matches(candidate) {
			return candidate
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}
}

func (c cronExpr) matches(t time.Time) bool {
	if !c.minute.allowed[t.Minute()] {
		return false
	}
	if !c.hour.allowed[t.Hour()] {
		return false
	}
	if !c.month.allowed[int(t.Month())] {
		return false
	}
	domMatch := c.dayOfMonth.allowed[t.Day()]
	dowMatch := c.dayOfWeek.allowed[int(t.Weekday())]
	// POSIX/Vixie semantics: when both day fields are restricted (raw
	// token != "*"), the expression matches a day satisfying EITHER
	// field. Otherwise the unrestricted side is trivially true and the
	// match degenerates to the single restricted axis. Without this
	// branch '0 12 15 * 1' silently meant 'the 15th AND a Monday'
	// (~once a year) instead of 'every Monday OR the 15th' (~every
	// 4 days).
	if c.dayOfMonthRestricted && c.dayOfWeekRestricted {
		return domMatch || dowMatch
	}
	return domMatch && dowMatch
}
