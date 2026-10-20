// Package normalize converts the formats Korean APIs commonly return into the
// mcp-hub output conventions: ISO 8601 dates, numeric amounts, plain text.
package normalize

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Date converts "20251019", "2025.10.19", "2025.10.19.", "2025/10/19" or
// "2025-10-19" to "2025-10-19". Unparseable input is returned trimmed, so no
// data is lost; empty or placeholder input ("-") becomes "".
func Date(s string) string {
	s = strings.TrimSpace(s)
	if isPlaceholder(s) {
		return ""
	}
	d := strings.TrimSuffix(s, ".")
	d = strings.NewReplacer(".", "-", "/", "-", " ", "").Replace(d)
	if len(d) == 8 && !strings.Contains(d, "-") {
		d = d[:4] + "-" + d[4:6] + "-" + d[6:]
	}
	if t, err := time.Parse("2006-1-2", d); err == nil {
		return t.Format("2006-01-02")
	}
	return s
}

// Int parses amounts like "1,234,567", "-1,234", "+12" or "(1,234)" (negative,
// accounting style). It returns nil for empty, placeholder or unparseable
// input so outputs use null rather than a misleading 0.
func Int(s string) *int64 {
	s = strings.TrimSpace(s)
	if isPlaceholder(s) {
		return nil
	}
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg, s = true, s[1:len(s)-1]
	}
	s = strings.NewReplacer(",", "", " ", "", "+", "").Replace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	if neg {
		n = -n
	}
	return &n
}

var (
	tagPattern   = regexp.MustCompile(`<[^>]*>`)
	spacePattern = regexp.MustCompile(`\s+`)
)

// Text strips HTML tags (Naver search wraps matches in <b>), unescapes
// entities, and collapses whitespace.
func Text(s string) string {
	s = tagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spacePattern.ReplaceAllString(s, " "))
}

func isPlaceholder(s string) bool {
	return s == "" || s == "-" || s == "--"
}
