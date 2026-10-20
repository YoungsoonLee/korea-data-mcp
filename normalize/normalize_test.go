package normalize

import "testing"

func TestDate(t *testing.T) {
	for in, want := range map[string]string{
		"20251019":    "2025-10-19",
		"2025.10.19":  "2025-10-19",
		"2025.10.19.": "2025-10-19",
		"2025. 1. 5.": "2025-01-05",
		"2025/10/19":  "2025-10-19",
		"2025-10-19":  "2025-10-19",
		" - ":         "",
		"":            "",
		"2025년 10월":   "2025년 10월",
		"20251399":    "20251399",
	} {
		if got := Date(in); got != want {
			t.Errorf("Date(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInt(t *testing.T) {
	for in, want := range map[string]*int64{
		"1,234,567": ptr(1234567),
		"-1,234":    ptr(-1234),
		"(1,234)":   ptr(-1234),
		"+12":       ptr(12),
		"-":         nil,
		"":          nil,
		"N/A":       nil,
	} {
		got := Int(in)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("Int(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestText(t *testing.T) {
	in := "<b>삼성전자</b>, &quot;AI 반도체&quot;\n  투자 확대"
	if got := Text(in); got != `삼성전자, "AI 반도체" 투자 확대` {
		t.Errorf("got %q", got)
	}
}

func ptr(n int64) *int64 { return &n }
