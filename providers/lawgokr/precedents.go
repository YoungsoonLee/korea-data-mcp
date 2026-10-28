package lawgokr

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Precedents (판례) come from three sources. Quirks handled here:
//   - About 90% of recent records come from the National Tax Service's system
//     (국세법령정보시스템), and their text is not available through the API
//     ("일치하는 판례가 없습니다"). Searches default to court records, which
//     always have text.
//   - Tax-system records have no court name; the court is inside the case
//     number (서울행정법원-2025-구합-53785).
//   - A malformed date range or a case number with spaces is "no results".
//   - Text uses <br/> for line breaks, and empty fields can be "null".
const precURL = "https://www.law.go.kr/LSW/precInfoP.do?precSeq="

// sources maps the source option to datSrcNm.
var sources = map[string]string{
	"court":        "대법원", // the Supreme Court's database of rulings at every level
	"local_tax":    "지방세법령정보시스템",
	"national_tax": "국세법령정보시스템",
}

var sourceIDs = map[string]string{"대법원": "court", "지방세법령정보시스템": "local_tax", "국세법령정보시스템": "national_tax"}

func registerPrecedents(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "lawgokr_search_precedents",
		Description: "Search Korean court rulings (판례), newest first, by words in the case name (e.g. 부당해고, 손해배상), " +
			"by full text with in_text, by case number (2020다296604), by court and by decision date. " +
			"Returns ids for lawgokr_get_precedent. By default only court records, which all have full text; " +
			"source national_tax adds the National Tax Service's records, whose text is only on the law.go.kr website.",
	}, c.searchPrecedents)

	hub.AddTool(r, &mcp.Tool{
		Name: "lawgokr_get_precedent",
		Description: "Get a Korean court ruling by id or case number: issues (판시사항), holding (판결요지), cited statutes and cases, " +
			"and with include_full_text the whole decision (it can be over 30,000 characters).",
	}, c.getPrecedent)
}

type searchPrecedentsInput struct {
	Query      string `json:"query,omitempty" jsonschema:"words in the case name, or in the text with in_text; a case number such as 2020다296604 also works"`
	InText     bool   `json:"in_text,omitempty" jsonschema:"search the full text instead of case names"`
	CaseNumber string `json:"case_number,omitempty" jsonschema:"exact case number, e.g. 2020다296604"`
	Court      string `json:"court,omitempty" jsonschema:"supreme (대법원), lower (all other courts), or a court name such as 서울고등법원"`
	From       string `json:"from,omitempty" jsonschema:"decided on or after, YYYY-MM-DD"`
	To         string `json:"to,omitempty" jsonschema:"decided on or before, YYYY-MM-DD"`
	Source     string `json:"source,omitempty" jsonschema:"court (default), local_tax (지방세 rulings) or national_tax (국세 rulings, no text through the API)"`
	hub.PageParams
}

type PrecedentSummary struct {
	ID            string `json:"id" jsonschema:"판례일련번호, for lawgokr_get_precedent"`
	CaseName      string `json:"case_name"`
	CaseNumber    string `json:"case_number"`
	Court         string `json:"court,omitempty"`
	Decided       string `json:"decided,omitempty" jsonschema:"decision date"`
	CaseType      string `json:"case_type,omitempty" jsonschema:"민사, 형사, 일반행정, 세무, 가사, 특허, etc."`
	JudgmentType  string `json:"judgment_type,omitempty" jsonschema:"판결, 결정, 전원합의체 판결, etc."`
	Source        string `json:"source,omitempty" jsonschema:"court, local_tax or national_tax"`
	TextAvailable bool   `json:"text_available" jsonschema:"false for national_tax records: read them at url"`
	URL           string `json:"url"`
}

type precSearch struct {
	TotalCnt int       `xml:"totalCnt"`
	Precs    []precRow `xml:"prec"`
}

type precRow struct {
	ID       string `xml:"판례일련번호"`
	Name     string `xml:"사건명"`
	Number   string `xml:"사건번호"`
	Date     string `xml:"선고일자"`
	Court    string `xml:"법원명"`
	CaseType string `xml:"사건종류명"`
	Judgment string `xml:"판결유형"`
	Source   string `xml:"데이터출처명"`
}

var (
	caseNumberPattern = regexp.MustCompile(`^\d{2,4}[가-힣]{1,4}\d+$`)
	// Tax-system case numbers carry the court: 서울행정법원-2025-구합-53785.
	taxCaseNumber = regexp.MustCompile(`^(.+?)-(\d{2,4})-([가-힣]+)-(\d+)$`)
)

func (r precRow) summary() PrecedentSummary {
	s := PrecedentSummary{
		ID: field(r.ID), CaseName: field(r.Name), CaseNumber: field(r.Number), Court: field(r.Court),
		Decided: date(strings.ReplaceAll(field(r.Date), ".", "")), CaseType: field(r.CaseType), JudgmentType: field(r.Judgment),
		Source: sourceIDs[field(r.Source)],
	}
	if m := taxCaseNumber.FindStringSubmatch(s.CaseNumber); m != nil {
		s.CaseNumber = m[2] + m[3] + m[4]
		if s.Court == "" {
			s.Court = m[1]
		}
	}
	s.TextAvailable = s.Source != "national_tax"
	s.URL = precURL + s.ID
	return s
}

func (c *client) searchPrecedents(ctx context.Context, in searchPrecedentsInput) (hub.List[PrecedentSummary], error) {
	out := hub.List[PrecedentSummary]{Items: []PrecedentSummary{}}
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return out, err
	}
	q := url.Values{"target": {"prec"}, "page": {strconv.Itoa(page)}, "display": {strconv.Itoa(size)}}

	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "court"
	}
	if source != "all" {
		name, ok := sources[source]
		if !ok {
			return out, hub.Errorf(hub.CodeInvalidArgument, "source must be court, local_tax, national_tax or all, got %q", in.Source)
		}
		q.Set("datSrcNm", name)
	}

	query := strings.TrimSpace(in.Query)
	number := strings.ReplaceAll(strings.TrimSpace(in.CaseNumber), " ", "")
	if number == "" && !in.InText && caseNumberPattern.MatchString(strings.ReplaceAll(query, " ", "")) {
		number, query = strings.ReplaceAll(query, " ", ""), ""
	}
	if number != "" {
		if !caseNumberPattern.MatchString(number) {
			return out, hub.Errorf(hub.CodeInvalidArgument, "case_number must look like 2020다296604, got %q", in.CaseNumber)
		}
		q.Set("nb", number)
	}
	if query != "" {
		q.Set("query", query)
		if in.InText {
			q.Set("search", "2")
		}
	}

	switch court := strings.TrimSpace(in.Court); court {
	case "":
	case "supreme", "대법원":
		q.Set("org", "400201")
	case "lower":
		q.Set("org", "400202")
	default:
		q.Set("curt", court)
	}

	if in.From != "" || in.To != "" {
		from, err := c.parseDate("from", cmp.Or(strings.TrimSpace(in.From), "1900-01-01"))
		if err != nil {
			return out, err
		}
		to, err := c.parseDate("to", cmp.Or(strings.TrimSpace(in.To), c.now().In(kst).Format(time.DateOnly)))
		if err != nil {
			return out, err
		}
		if from.After(to) {
			return out, hub.Errorf(hub.CodeInvalidArgument, "from (%s) is after to (%s)", in.From, in.To)
		}
		q.Set("prncYd", from.Format("20060102")+"~"+to.Format("20060102"))
	}
	if query == "" && number == "" && !q.Has("prncYd") {
		return out, hub.Errorf(hub.CodeInvalidArgument, "give query, case_number, or a from/to date range")
	}

	var res precSearch
	if err := c.get(ctx, "lawSearch", q, &res); err != nil {
		return out, err
	}
	for _, r := range res.Precs {
		out.Items = append(out.Items, r.summary())
	}
	out.PageInfo = hub.NewPageInfo(page, size, res.TotalCnt)
	return out, nil
}

func (c *client) parseDate(name, s string) (time.Time, error) {
	t, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(s), kst)
	if err != nil {
		return time.Time{}, hub.Errorf(hub.CodeInvalidArgument, "%s must be a date, YYYY-MM-DD, got %q", name, s)
	}
	return t, nil
}

type getPrecedentInput struct {
	ID              string `json:"id,omitempty" jsonschema:"precedent id from lawgokr_search_precedents"`
	CaseNumber      string `json:"case_number,omitempty" jsonschema:"case number, e.g. 2020다296604, instead of id"`
	IncludeFullText bool   `json:"include_full_text,omitempty" jsonschema:"include the whole decision text"`
}

type Precedent struct {
	PrecedentSummary
	Issues         string `json:"issues,omitempty" jsonschema:"판시사항: the legal questions decided"`
	Holding        string `json:"holding,omitempty" jsonschema:"판결요지: the court's conclusions"`
	CitedStatutes  string `json:"cited_statutes,omitempty" jsonschema:"참조조문"`
	CitedCases     string `json:"cited_cases,omitempty" jsonschema:"참조판례"`
	FullText       string `json:"full_text,omitempty" jsonschema:"판례내용, with include_full_text"`
	FullTextLength int    `json:"full_text_length" jsonschema:"characters in the full text"`
}

type precDoc struct {
	ID       string `xml:"판례정보일련번호"`
	Name     string `xml:"사건명"`
	Number   string `xml:"사건번호"`
	Date     string `xml:"선고일자"`
	Court    string `xml:"법원명"`
	CaseType string `xml:"사건종류명"`
	Judgment string `xml:"판결유형"`
	Issues   string `xml:"판시사항"`
	Holding  string `xml:"판결요지"`
	Statutes string `xml:"참조조문"`
	Cases    string `xml:"참조판례"`
	Text     string `xml:"판례내용"`
}

var precIDPattern = regexp.MustCompile(`^\d{1,9}$`)

func (c *client) getPrecedent(ctx context.Context, in getPrecedentInput) (Precedent, error) {
	id := strings.TrimSpace(in.ID)
	switch {
	case id != "":
		if !precIDPattern.MatchString(id) {
			return Precedent{}, hub.Errorf(hub.CodeInvalidArgument, "id must be a number from lawgokr_search_precedents, got %q", in.ID).
				WithHint("for a case number such as 2020다296604, use case_number")
		}
	case strings.TrimSpace(in.CaseNumber) != "":
		found, err := c.searchPrecedents(ctx, searchPrecedentsInput{CaseNumber: in.CaseNumber, Source: "all"})
		if err != nil {
			return Precedent{}, err
		}
		switch len(found.Items) {
		case 0:
			return Precedent{}, hub.Errorf(hub.CodeNotFound, "no ruling with case number %s", in.CaseNumber).
				WithHint("check the number; lawgokr_search_precedents can search by case name")
		case 1:
			id = found.Items[0].ID
		default:
			var opts []string
			for _, p := range found.Items {
				opts = append(opts, fmt.Sprintf("%s %s %s (id %s)", p.Court, p.Decided, p.JudgmentType, p.ID))
			}
			return Precedent{}, hub.Errorf(hub.CodeInvalidArgument, "case number %s has %d rulings: %s", in.CaseNumber, len(found.Items), strings.Join(opts, "; ")).
				WithHint("pass the id of the one you want")
		}
	default:
		return Precedent{}, hub.Errorf(hub.CodeInvalidArgument, "give id or case_number")
	}

	var doc precDoc
	if err := c.get(ctx, "lawService", url.Values{"target": {"prec"}, "ID": {id}}, &doc); err != nil {
		if e := hub.AsError(err); e.Code == hub.CodeNotFound {
			return Precedent{}, e.WithHint("National Tax Service rulings (source national_tax) have no text in the API; read it at " + precURL + id)
		}
		return Precedent{}, err
	}
	sum := precRow{ID: doc.ID, Name: doc.Name, Number: doc.Number, Date: doc.Date, Court: doc.Court, CaseType: doc.CaseType, Judgment: doc.Judgment}.summary()
	sum.TextAvailable = true
	p := Precedent{
		PrecedentSummary: sum,
		Issues:           cleanText(doc.Issues),
		Holding:          cleanText(doc.Holding),
		CitedStatutes:    cleanText(doc.Statutes),
		CitedCases:       cleanText(doc.Cases),
	}
	full := cleanText(doc.Text)
	p.FullTextLength = len([]rune(full))
	if in.IncludeFullText {
		p.FullText = full
	}
	return p, nil
}

var (
	brTag      = regexp.MustCompile(`(?i)<br\s*/?>\n?`)
	htmlTag    = regexp.MustCompile(`</?[a-zA-Z][^>]*>`) // ASCII tag names only: keeps <개정 2020.5.26>
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// cleanText turns <br/> into line breaks, drops other HTML tags and trims
// each line (lines after <br/> start with a space).
func cleanText(s string) string {
	s = field(s)
	s = htmlTag.ReplaceAllString(brTag.ReplaceAllString(s, "\n"), "")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// field trims a value and drops the literal "null" some records contain.
func field(s string) string {
	s = strings.TrimSpace(s)
	if s == "null" {
		return ""
	}
	return s
}
