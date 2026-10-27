package datagokr

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

func TestResolveRegion(t *testing.T) {
	for _, tc := range []struct {
		in, label string
		codes     []string
	}{
		{"강남구", "서울특별시 강남구", []string{"11680"}},
		{"서울 강남구", "서울특별시 강남구", []string{"11680"}},
		{" 서울특별시  강남구 ", "서울특별시 강남구", []string{"11680"}},
		{"11680", "서울특별시 강남구", []string{"11680"}},
		{"서울 중구", "서울특별시 중구", []string{"11140"}},
		{"분당구", "경기도 성남시 분당구", []string{"41135"}},
		{"성남 분당구", "경기도 성남시 분당구", []string{"41135"}},
		{"경기 성남시 분당구", "경기도 성남시 분당구", []string{"41135"}},
		{"수원시", "경기도 수원시", []string{"41111", "41113", "41115", "41117"}},
		{"41110", "경기도 수원시", []string{"41111", "41113", "41115", "41117"}},
		// 화성시 was split into four 구; 41590 itself returns nothing.
		{"화성시", "경기도 화성시", []string{"41591", "41593", "41595", "41597"}},
		{"41590", "경기도 화성시", []string{"41591", "41593", "41595", "41597"}},
		{"동탄구", "경기도 화성시 동탄구", []string{"41597"}},
		// 광주광역시 and 전라남도 merged into 전남광주통합특별시 (12).
		{"광주 북구", "전남광주통합특별시 북구", []string{"12300"}},
		{"광주광역시 광산구", "전남광주통합특별시 광산구", []string{"12330"}},
		{"전남 목포시", "전남광주통합특별시 목포시", []string{"12110"}},
		{"광주시", "경기도 광주시", []string{"41610"}}, // not the former 광주광역시
		{"인천 제물포구", "인천광역시 제물포구", []string{"28125"}},
		{"세종", "세종특별자치시", []string{"36110"}},
		{"세종시", "세종특별자치시", []string{"36110"}},
	} {
		label, codes, err := resolveRegion(tc.in)
		if err != nil || label != tc.label || !slices.Equal(codes, tc.codes) {
			t.Errorf("%q: got %q %v %v, want %q %v", tc.in, label, codes, err, tc.label, tc.codes)
		}
	}
}

func TestResolveRegionErrors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"중구", "서울특별시 중구 (11140)"}, // ambiguous: lists the candidates
		{"고성군", "강원특별자치도 고성군"},     // 강원 and 경남
		{"서울", "is a province"},
		{"광주", "is a province"},
		{"29110", "광주·전남"}, // former 광주 동구: hint names the change
		{"46110", "광주·전남"},
		{"28110", "제물포구 (28125) and 영종구 (28155)"}, // 인천, July 2026
		{"인천 중구", "no longer exists"},
		{"인천광역시 서구", "서해구 (28275) and 검단구 (28290)"},
		{"강남", "unknown region"},
		{"1168", "unknown region"},
		{"", "unknown region"},
	} {
		_, _, err := resolveRegion(tc.in)
		e := hub.AsError(err)
		if e == nil || e.Code != hub.CodeInvalidArgument || !strings.Contains(e.Message+" "+e.Hint, tc.want) {
			t.Errorf("%q: got %v", tc.in, err)
		}
	}
}

// Every 구 of a 시 must resolve by its own name too, and every district code
// must be in the table exactly once.
func TestRegionTable(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range districts {
		if seen[d.code] || !districtCodePattern.MatchString(d.code) {
			t.Errorf("bad or duplicate code %+v", d)
		}
		seen[d.code] = true
		if strings.Contains(d.name, " ") && d.parent == "" {
			t.Errorf("구 without its 시: %+v", d)
		}
		if _, codes, err := resolveRegion(d.label()); err != nil || len(codes) == 0 {
			t.Errorf("%s does not resolve: %v", d.label(), err)
		}
	}
	if len(districts) < 200 {
		t.Errorf("only %d districts", len(districts))
	}
}
