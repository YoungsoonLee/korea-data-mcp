package datagokr

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/YoungsoonLee/mcp-hub/hub"
)

// The district table (regions_data.go) is generated from the official
// legal-dong code list, because 시군구 codes change and past data moves with
// them: 광주광역시 (29) and 전라남도 (46) became 전남광주통합특별시 (12), and
// 화성시 (41590) was split into four 구, and 인천's 중구, 동구 and 서구 became
// 제물포구, 영종구, 서해구 and 검단구. The old codes return no data, even for
// past months.
//go:generate go run ./internal/genregions -out regions_data.go

type district struct {
	code   string // 5-digit 시군구 code (LAWD_CD)
	name   string // name within the province: 강남구, 성남시 분당구, 화성시
	parent string // for a 구 inside a 시, the 시's code
}

// sidoAliases are the short and former names people use for provinces.
var sidoAliases = map[string][]string{
	"11": {"서울", "서울시"},
	"12": {"광주", "광주광역시", "전남", "전라남도", "전남광주"},
	"26": {"부산", "부산시"},
	"27": {"대구", "대구시"},
	"28": {"인천", "인천시"},
	"30": {"대전", "대전시"},
	"31": {"울산", "울산시"},
	"36": {"세종", "세종시"},
	"41": {"경기"},
	"43": {"충북"},
	"44": {"충남"},
	"47": {"경북"},
	"48": {"경남"},
	"50": {"제주", "제주도"},
	"51": {"강원", "강원도"},
	"52": {"전북", "전라북도"},
}

// formerDistricts are 시군구 that no longer exist, keyed by former code and
// by name without spaces, with what replaced them.
var formerDistricts = func() map[string]string {
	m := map[string]string{}
	for _, f := range []struct{ code, name, msg string }{
		{"28110", "중구", "인천 중구 (28110) became 제물포구 (28125) and 영종구 (28155)"},
		{"28140", "동구", "인천 동구 (28140) became part of 제물포구 (28125)"},
		{"28260", "서구", "인천 서구 (28260) became 서해구 (28275) and 검단구 (28290)"},
	} {
		m[f.code], m["인천"+f.name], m["인천광역시"+f.name] = f.msg, f.msg, f.msg
	}
	return m
}()

type regionIndex struct {
	byKey    map[string][]int    // name key without spaces -> districts
	children map[string][]string // 시 code -> its 구 codes
	byCode   map[string]int
	bySido   map[string][]int // province name key -> its districts
}

var regions = sync.OnceValue(func() *regionIndex {
	ix := &regionIndex{byKey: map[string][]int{}, children: map[string][]string{}, byCode: map[string]int{}, bySido: map[string][]int{}}
	add := func(key string, i int) {
		key = strings.ReplaceAll(key, " ", "")
		if !slices.Contains(ix.byKey[key], i) {
			ix.byKey[key] = append(ix.byKey[key], i)
		}
	}
	for i, d := range districts {
		ix.byCode[d.code] = i
		if d.parent != "" {
			ix.children[d.parent] = append(ix.children[d.parent], d.code)
		}
		sido := d.code[:2]
		provinces := append([]string{sidoNames[sido]}, sidoAliases[sido]...)
		names := []string{d.name}
		if city, gu, ok := strings.Cut(d.name, " "); ok {
			names = append(names, gu, strings.TrimSuffix(city, "시")+" "+gu) // 분당구, 성남 분당구
		}
		for _, n := range names {
			add(n, i)
			for _, p := range provinces {
				if p != "" {
					add(p+" "+n, i)
				}
			}
		}
		for _, p := range provinces {
			if p != "" {
				ix.bySido[p] = append(ix.bySido[p], i)
			}
		}
	}
	return ix
})

// label is the full district name, e.g. 서울특별시 강남구.
func (d district) label() string {
	if p := sidoNames[d.code[:2]]; p != "" {
		return p + " " + d.name
	}
	return d.name // 세종특별자치시 has no province row
}

// resolveRegion turns a district name or code into a label and the 5-digit
// codes to query. A 시 that has 구 expands to all of them.
func resolveRegion(s string) (string, []string, error) {
	ix := regions()
	key := strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	var hits []int
	if districtCodePattern.MatchString(key) {
		i, ok := ix.byCode[key]
		if !ok {
			return "", nil, hub.Errorf(hub.CodeInvalidArgument, "unknown district code %s", key).
				WithHint(cmp.Or(formerDistricts[key], "district codes change when regions are reorganized (광주·전남 are now 12xxx)") + "; pass the district name instead")
		}
		hits = []int{i}
	} else {
		hits = ix.byKey[key]
	}
	if len(hits) == 0 {
		if only := ix.bySido[key]; len(only) == 1 { // 세종
			hits = only
		} else if len(only) > 1 {
			return "", nil, hub.Errorf(hub.CodeInvalidArgument, "%q is a province; name a district (시군구) in it, e.g. %s", s, districts[only[0]].label())
		}
	}
	switch len(hits) {
	case 0:
		if msg, ok := formerDistricts[key]; ok {
			return "", nil, hub.Errorf(hub.CodeInvalidArgument, "%s no longer exists: %s", s, msg).
				WithHint("query the new districts one at a time")
		}
		return "", nil, hub.Errorf(hub.CodeInvalidArgument, "unknown region %q", s).
			WithHint("use a district (시군구) name such as 강남구, 서울 중구, 성남시 분당구 or 화성시, or a 5-digit district code")
	case 1:
	default:
		var opts []string
		for _, i := range hits {
			opts = append(opts, fmt.Sprintf("%s (%s)", districts[i].label(), districts[i].code))
		}
		return "", nil, hub.Errorf(hub.CodeInvalidArgument, "%q matches several districts: %s", s, strings.Join(opts, ", ")).
			WithHint("add the province, e.g. " + districts[hits[0]].label())
	}
	d := districts[hits[0]]
	if kids := ix.children[d.code]; len(kids) > 0 {
		return d.label(), kids, nil
	}
	return d.label(), []string{d.code}, nil
}

// districtName returns the name of a district code within its province.
func districtName(code string) string {
	if i, ok := regions().byCode[code]; ok {
		return districts[i].name
	}
	return ""
}
