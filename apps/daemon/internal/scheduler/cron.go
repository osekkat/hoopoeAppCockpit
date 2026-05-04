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
	if err := validateDayMonthCombination(dom, month); err != nil {
		return cronExpr{}, fmt.Errorf("%w: %v", ErrInvalidDefinition, err)
	}
	return cronExpr{minute: minute, hour: hour, dayOfMonth: dom, month: month, dayOfWeek: dow}, nil
}

// validateDayMonthCombination rejects expressions like '* * 31 2 *'
// (Feb 31, never matches) or '* * 31 4,6,9,11 *' (31st of months
// without a 31st day). Without this guard, cronExpr.Next walks the
// full 5-year deadline (~2.6M minute steps) on every recompute,
// holding r.mu and spiking CPU. Day-of-month and month are validated
// together because each is structurally valid in isolation.
func validateDayMonthCombination(dom, month cronField) error {
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
	return c.minute.allowed[t.Minute()] &&
		c.hour.allowed[t.Hour()] &&
		c.dayOfMonth.allowed[t.Day()] &&
		c.month.allowed[int(t.Month())] &&
		c.dayOfWeek.allowed[int(t.Weekday())]
}
