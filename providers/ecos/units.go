package ecos

import (
	"strconv"
	"strings"
)

// unitScale maps ECOS unit names to an output unit and the power of ten that
// converts a value into it: 834925.1 십억원 is 834,925,100,000,000 KRW.
var unitScale = map[string]struct {
	unit string
	exp  int
}{
	"원":    {"KRW", 0},
	"천원":   {"KRW", 3},
	"만원":   {"KRW", 4},
	"백만원":  {"KRW", 6},
	"억원":   {"KRW", 8},
	"십억원":  {"KRW", 9},
	"조원":   {"KRW", 12},
	"달러":   {"USD", 0},
	"천달러":  {"USD", 3},
	"백만달러": {"USD", 6},
	"억달러":  {"USD", 8},
	"십억달러": {"USD", 9},
	"명":    {"persons", 0},
	"천명":   {"persons", 3},
	"만명":   {"persons", 4},
	"주":    {"shares", 0},
	"천주":   {"shares", 3},
	"만주":   {"shares", 4},
	"%":    {"%", 0},
	"연%":   {"% per year", 0},
	"%p":   {"percentage points", 0},
}

// convertUnit returns the output unit and scale for an ECOS unit name.
// Index bases ("2020=100") become "index (2020=100)"; anything unknown is
// passed through unchanged.
func convertUnit(name string) (unit string, exp int) {
	name = strings.TrimSpace(name)
	if u, ok := unitScale[name]; ok {
		return u.unit, u.exp
	}
	if strings.Contains(name, "=100") || strings.Contains(name, "=1000") {
		return "index (" + name + ")", 0
	}
	return name, 0
}

// value parses an ECOS DATA_VALUE and scales it by 10^exp. Empty and
// placeholder values are nil.
func value(s string, exp int) *float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" || s == "-" {
		return nil
	}
	if exp != 0 {
		s += "e" + strconv.Itoa(exp)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}
