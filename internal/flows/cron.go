package flows

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	// Time zones work on servers without a zone database.
	_ "time/tzdata"
)

// Schedule is a parsed cron expression: minute, hour, day of month, month
// and day of week, each a set of allowed values.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	// domAny and dowAny record a "*" day field: with both days restricted,
	// a time matches when either does, as in classic cron.
	domAny, dowAny bool
}

var cronAliases = map[string]string{
	"@hourly":   "0 * * * *",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@weekly":   "0 0 * * 0",
	"@monthly":  "0 0 1 * *",
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
}

var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
var dayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// ParseCron reads a five-field cron expression such as "*/15 * * * *" or
// "30 8 * * mon-fri", or an alias such as @daily.
func ParseCron(expr string) (*Schedule, error) {
	e := strings.ToLower(strings.TrimSpace(expr))
	if a, ok := cronAliases[e]; ok {
		e = a
	}
	f := strings.Fields(e)
	if len(f) != 5 {
		return nil, fmt.Errorf("a schedule has five parts: minute hour day-of-month month day-of-week")
	}
	s := &Schedule{domAny: f[2] == "*" || f[2] == "?", dowAny: f[4] == "*" || f[4] == "?"}
	var err error
	if s.minute, err = cronField(f[0], 0, 59, nil, "minute"); err != nil {
		return nil, err
	}
	if s.hour, err = cronField(f[1], 0, 23, nil, "hour"); err != nil {
		return nil, err
	}
	if s.dom, err = cronField(f[2], 1, 31, nil, "day of the month"); err != nil {
		return nil, err
	}
	if s.month, err = cronField(f[3], 1, 12, monthNames, "month"); err != nil {
		return nil, err
	}
	if s.dow, err = cronField(f[4], 0, 7, dayNames, "day of the week"); err != nil {
		return nil, err
	}
	// 7 is Sunday too.
	if s.dow&(1<<7) != 0 {
		s.dow |= 1
	}
	return s, nil
}

func cronField(field string, lo, hi int, names []string, what string) (uint64, error) {
	var set uint64
	for _, part := range strings.Split(field, ",") {
		step := 1
		if r, st, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(st)
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("the %s step %q is not a positive number", what, st)
			}
			part, step = r, n
		}
		from, to := lo, hi
		switch {
		case part == "*" || part == "?":
		default:
			a, b, isRange := strings.Cut(part, "-")
			var err error
			if from, err = cronValue(a, lo, hi, names, what); err != nil {
				return 0, err
			}
			to = from
			if isRange {
				if to, err = cronValue(b, lo, hi, names, what); err != nil {
					return 0, err
				}
				if to < from {
					return 0, fmt.Errorf("the %s range %s runs backwards", what, part)
				}
			} else if step > 1 {
				to = hi
			}
		}
		for v := from; v <= to; v += step {
			set |= 1 << v
		}
	}
	return set, nil
}

func cronValue(s string, lo, hi int, names []string, what string) (int, error) {
	for i, n := range names {
		if s == n {
			if what == "month" {
				return i + 1, nil
			}
			return i, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("the %s %q is not between %d and %d", what, s, lo, hi)
	}
	return v, nil
}

// Match reports whether the schedule fires in the minute of t, read in
// t's location.
func (s *Schedule) Match(t time.Time) bool {
	if s.minute&(1<<t.Minute()) == 0 || s.hour&(1<<t.Hour()) == 0 || s.month&(1<<int(t.Month())) == 0 {
		return false
	}
	return s.dayMatches(t)
}

// Next returns the first minute after t when the schedule fires, within
// five years.
func (s *Schedule) Next(t time.Time) (time.Time, bool) {
	t = t.Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(5, 0, 0)
	for t.Before(end) {
		if s.month&(1<<int(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if s.hour&(1<<t.Hour()) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if s.Match(t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

func (s *Schedule) dayMatches(t time.Time) bool {
	dom := s.dom&(1<<t.Day()) != 0
	dow := s.dow&(1<<int(t.Weekday())) != 0
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	}
	return dom || dow
}
