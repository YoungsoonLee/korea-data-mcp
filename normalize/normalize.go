// Package normalize converts the formats Korean APIs commonly return into the
// korea-data-mcp output conventions: ISO 8601 dates, numeric amounts, plain text.
package normalize

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var shortYearDate = regexp.MustCompile(`^(\d{2})[./-](\d{1,2})[./-](\d{1,2})\.?$`)

// Date converts "20251019", "2025.10.19", "2025.10.19.", "2025/10/19" or
// "2025-10-19" to "2025-10-19". Two-digit years ("75/06/11", "25.11.28") are
// read as the latest such year not after the current one. Unparseable input
// is returned trimmed, so no data is lost; empty or placeholder input ("-")
// becomes "".
func Date(s string) string {
	s = strings.TrimSpace(s)
	if isPlaceholder(s) {
		return ""
	}
	if m := shortYearDate.FindStringSubmatch(s); m != nil {
		yy, _ := strconv.Atoi(m[1])
		y := 2000 + yy
		if y > time.Now().Year() {
			y -= 100
		}
		return Date(strconv.Itoa(y) + "-" + m[2] + "-" + m[3])
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

var companyForm = strings.NewReplacer(" ", "", ",", "", ".", "",
	"(주)", "", "㈜", "", "주식회사", "",
	"(유)", "", "(유한)", "", "유한회사", "", "(합자)", "", "(자)", "", "(합명)", "")

// StripCompanyForm removes spacing, punctuation and corporate-form markers
// ((주), ㈜, 주식회사, (유), 유한회사, (합자), ...), so "삼성 전자(주)"
// becomes "삼성전자".
func StripCompanyForm(s string) string {
	return companyForm.Replace(strings.TrimSpace(s))
}

// CompanyName is StripCompanyForm lowercased, for comparing company names.
func CompanyName(s string) string {
	return strings.ToLower(StripCompanyForm(s))
}

func isPlaceholder(s string) bool {
	return s == "" || s == "-" || s == "--"
}
