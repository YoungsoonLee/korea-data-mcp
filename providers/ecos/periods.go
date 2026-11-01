package ecos

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
)

// freq is an ECOS cycle code: D, M, Q, S (half year) or A.
type freq string

const (
	daily      freq = "D"
	monthly    freq = "M"
	quarterly  freq = "Q"
	semiannual freq = "S"
	annual     freq = "A"
)

var freqNames = map[freq]string{daily: "daily", monthly: "monthly", quarterly: "quarterly", semiannual: "semiannual", annual: "annual"}

func (f freq) String() string { return freqNames[f] }

func parseFreq(s string) (freq, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for f, name := range freqNames {
		if s == name || s == strings.ToLower(string(f)) {
			return f, nil
		}
	}
	return "", hub.Errorf(hub.CodeInvalidArgument, "frequency must be one of daily, monthly, quarterly, semiannual, annual; got %q", s)
}

// defaultSpan is how far back a series goes when from is not given.
func defaultSpan(f freq, to time.Time) time.Time {
	switch f {
	case daily:
		return to.AddDate(0, 0, -30)
	case monthly:
		return to.AddDate(0, -23, 0)
	case quarterly:
		return to.AddDate(0, -33, 0)
	case semiannual:
		return to.AddDate(-4, -6, 0)
	}
	return to.AddDate(-9, 0, 0)
}

var periodPattern = regexp.MustCompile(`^(\d{4})(?:-?(\d{2})(?:-?(\d{2}))?|-?([QqHhSs])([1-4]))?$`)

// parsePeriod reads 2025-10-01, 20251001, 2025-10, 202510, 2025-Q3, 2025Q3,
// 2025-H1, 2025S1 or 2025 and returns the first or last day it covers.
func parsePeriod(s string, end bool) (time.Time, error) {
	m := periodPattern.FindStringSubmatch(strings.TrimSpace(s))
	bad := hub.Errorf(hub.CodeInvalidArgument, "period %q must look like 2025-10-01, 2025-10, 2025-Q3, 2025-H1 or 2025", s)
	if m == nil {
		return time.Time{}, bad
	}
	y, _ := strconv.Atoi(m[1])
	first, months := 1, 12
	switch {
	case m[4] != "":
		n, _ := strconv.Atoi(m[5])
		if strings.EqualFold(m[4], "q") {
			first, months = 3*(n-1)+1, 3
		} else if n <= 2 {
			first, months = 6*(n-1)+1, 6
		} else {
			return time.Time{}, bad
		}
	case m[2] != "":
		first, _ = strconv.Atoi(m[2])
		months = 1
		if first < 1 || first > 12 {
			return time.Time{}, bad
		}
	}
	t := time.Date(y, time.Month(first), 1, 0, 0, 0, 0, kst)
	if m[3] != "" {
		d, _ := strconv.Atoi(m[3])
		t = t.AddDate(0, 0, d-1)
		if t.Month() != time.Month(first) {
			return time.Time{}, bad
		}
		return t, nil
	}
	if end {
		t = t.AddDate(0, months, -1)
	}
	return t, nil
}

// ecosTime formats t as the ECOS period containing it: 20251001, 202510,
// 2025Q4, 2025S2 or 2025.
func ecosTime(t time.Time, f freq) string {
	y, m := t.Year(), int(t.Month())
	switch f {
	case daily:
		return t.Format("20060102")
	case monthly:
		return t.Format("200601")
	case quarterly:
		return fmt.Sprintf("%dQ%d", y, (m+2)/3)
	case semiannual:
		return fmt.Sprintf("%dS%d", y, (m+5)/6)
	}
	return strconv.Itoa(y)
}

// outPeriod turns an ECOS TIME into the output form: 2025-10-01, 2025-10,
// 2025-Q4, 2025-H2 or 2025.
func outPeriod(s string, f freq) string {
	switch {
	case f == daily && len(s) == 8:
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	case f == monthly && len(s) == 6:
		return s[:4] + "-" + s[4:]
	case f == quarterly && len(s) == 6:
		return s[:4] + "-" + s[4:]
	case f == semiannual && len(s) == 6:
		return s[:4] + "-H" + s[5:]
	}
	return s
}

// yearEarlier returns the output period one year before p, or "" for daily
// data and unknown forms.
func yearEarlier(p string, f freq) string {
	if f == daily || len(p) < 4 {
		return ""
	}
	y, err := strconv.Atoi(p[:4])
	if err != nil {
		return ""
	}
	return strconv.Itoa(y-1) + p[4:]
}
