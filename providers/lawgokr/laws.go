package lawgokr

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	searchPageRows = 100 // the maximum display
	maxSearchRows  = 1000
	maxVersionRows = 1000
	// maxUpcomingProbes bounds the upcoming versions checked for a missing
	// article.
	maxUpcomingProbes = 5
	undeterminedEf    = "99990101" // promulgated, effective date not set yet
)

func registerLaws(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "lawgokr_search_laws",
		Description: "Search Korean statutes in force (현행 법령: 법률, 대통령령, 부령 and others) by name or official abbreviation, " +
			"e.g. 민법, 근로기준법, 중대재해처벌법, 개인정보보호법. Exact matches and acts (법률) come first. " +
			"Set in_text to search article text instead of names. Returns law_id for lawgokr_get_law.",
	}, c.searchLaws)

	hub.AddTool(r, &mcp.Tool{
		Name: "lawgokr_get_law",
		Description: "Read a Korean statute as in force on a date (default today), all articles or one article (e.g. 제50조, 43의2). " +
			"Amendments promulgated but not yet in effect are not included; they are listed in upcoming with their effective dates, " +
			"and as_of shows the text on any date, past or future. An article that only an upcoming amendment adds is returned with not_yet_in_force " +
			"and the date it takes effect. The law can be a name, an official abbreviation or a law_id.",
	}, c.getLaw)
}

type searchLawsInput struct {
	Query  string `json:"query" jsonschema:"law name or part of it, or text to find with in_text"`
	InText bool   `json:"in_text,omitempty" jsonschema:"search article text (본문) instead of law names"`
	hub.PageParams
}

type LawSummary struct {
	LawID              string `json:"law_id" jsonschema:"법령ID, stable across amendments"`
	Name               string `json:"name"`
	Abbreviation       string `json:"abbreviation,omitempty" jsonschema:"official short name (법령약칭)"`
	Kind               string `json:"kind" jsonschema:"법률 (act), 대통령령 (presidential decree), 총리령/부령 (ministerial ordinance), etc."`
	Ministry           string `json:"ministry,omitempty"`
	EffectiveDate      string `json:"effective_date" jsonschema:"start of the version in force"`
	PromulgationDate   string `json:"promulgation_date"`
	PromulgationNumber string `json:"promulgation_number,omitempty"`
	Amendment          string `json:"amendment,omitempty" jsonschema:"제정, 일부개정, 전부개정, 타법개정, etc."`
	URL                string `json:"url"`
}

type lawSearch struct {
	TotalCnt   int      `xml:"totalCnt"`
	ResultCode string   `xml:"resultCode"`
	ResultMsg  string   `xml:"resultMsg"`
	Laws       []lawRow `xml:"law"`
}

// lawRow is one law version in search results.
type lawRow struct {
	MST      string `xml:"법령일련번호"`
	Status   string `xml:"현행연혁코드"` // 현행, 연혁, 시행예정
	Name     string `xml:"법령명한글"`
	Abbr     string `xml:"법령약칭명"`
	ID       string `xml:"법령ID"`
	PromDate string `xml:"공포일자"`
	PromNo   string `xml:"공포번호"`
	Amend    string `xml:"제개정구분명"`
	Ministry string `xml:"소관부처명"`
	Kind     string `xml:"법령구분명"`
	EfDate   string `xml:"시행일자"`
}

func (r lawRow) summary() LawSummary {
	return LawSummary{
		LawID: strings.TrimSpace(r.ID), Name: strings.TrimSpace(r.Name), Abbreviation: strings.TrimSpace(r.Abbr),
		Kind: strings.TrimSpace(r.Kind), Ministry: strings.TrimSpace(r.Ministry),
		EffectiveDate: date(r.EfDate), PromulgationDate: date(r.PromDate), PromulgationNumber: strings.TrimSpace(r.PromNo),
		Amendment: strings.TrimSpace(r.Amend), URL: lawURL(strings.TrimSpace(r.Name), ""),
	}
}

func (c *client) searchLaws(ctx context.Context, in searchLawsInput) (hub.List[LawSummary], error) {
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return hub.List[LawSummary]{}, err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return hub.List[LawSummary]{}, hub.Errorf(hub.CodeInvalidArgument, "query is required")
	}
	out := hub.List[LawSummary]{Items: []LawSummary{}}

	if in.InText { // upstream order and paging
		var res lawSearch
		q := url.Values{"target": {"eflaw"}, "nw": {"3"}, "search": {"2"}, "query": {query},
			"page": {strconv.Itoa(page)}, "display": {strconv.Itoa(size)}}
		if err := c.get(ctx, "lawSearch", q, &res); err != nil {
			return out, err
		}
		for _, r := range res.Laws {
			out.Items = append(out.Items, r.summary())
		}
		out.PageInfo = hub.NewPageInfo(page, size, res.TotalCnt)
		return out, nil
	}

	rows, err := c.findLaws(ctx, query)
	if err != nil {
		return out, err
	}
	out.PageInfo = hub.NewPageInfo(page, size, len(rows))
	for i := (page - 1) * size; i < len(rows) && i < page*size; i++ {
		out.Items = append(out.Items, rows[i].summary())
	}
	return out, nil
}

// findLaws returns the laws in force whose name or abbreviation matches
// query, ranked. Upstream sorts by name only (민법 is fourth, after 난민법),
// ignores abbreviations (중대재해처벌법 finds nothing), and ignores spaces.
func (c *client) findLaws(ctx context.Context, query string) ([]lawRow, error) {
	rows, err := c.searchNames(ctx, query)
	if err != nil {
		return nil, err
	}
	key := nameKey(query)
	if len(rows) == 0 && len([]rune(key)) > 2 {
		// Retry with the first two characters to reach abbreviations, and
		// keep only rows that really match.
		broad, err := c.searchNames(ctx, string([]rune(key)[:2]))
		if err != nil {
			return nil, err
		}
		for _, r := range broad {
			if strings.Contains(nameKey(r.Name), key) || strings.Contains(nameKey(r.Abbr), key) {
				rows = append(rows, r)
			}
		}
	}
	rank := func(r lawRow) int {
		name, abbr := nameKey(r.Name), nameKey(r.Abbr)
		switch {
		case name == key || abbr == key:
			return 0
		case strings.HasPrefix(name, key) || strings.HasPrefix(abbr, key):
			return 1
		}
		return 2
	}
	slices.SortStableFunc(rows, func(a, b lawRow) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind)), cmp.Compare(a.Name, b.Name))
	})
	return rows, nil
}

// searchNames fetches every law in force whose name contains query.
func (c *client) searchNames(ctx context.Context, query string) ([]lawRow, error) {
	var all []lawRow
	for page := 1; ; page++ {
		var res lawSearch
		q := url.Values{"target": {"eflaw"}, "nw": {"3"}, "query": {query},
			"page": {strconv.Itoa(page)}, "display": {strconv.Itoa(searchPageRows)}}
		if err := c.get(ctx, "lawSearch", q, &res); err != nil {
			return nil, err
		}
		if res.TotalCnt > maxSearchRows {
			return nil, hub.Errorf(hub.CodeInvalidArgument, "%q matches %d laws; use a more specific name", query, res.TotalCnt).
				WithHint("e.g. 근로기준법 rather than 근로, or set in_text to search article text")
		}
		all = append(all, res.Laws...)
		if len(res.Laws) < searchPageRows || len(all) >= res.TotalCnt {
			return all, nil
		}
	}
}

func nameKey(s string) string { return strings.Join(strings.Fields(s), "") }

func kindOrder(kind string) int {
	switch strings.TrimSpace(kind) {
	case "헌법":
		return 0
	case "법률":
		return 1
	case "대통령령":
		return 2
	}
	if strings.HasSuffix(kind, "령") { // 총리령, 부령
		return 3
	}
	return 4
}

type getLawInput struct {
	Law     string `json:"law" jsonschema:"law name (근로기준법), official abbreviation (중대재해처벌법) or 6-digit law_id"`
	Article string `json:"article,omitempty" jsonschema:"one article, e.g. 50, 제50조, 43의2 or 제43조의2; omit for all articles"`
	AsOf    string `json:"as_of,omitempty" jsonschema:"date the text is in force, YYYY-MM-DD; default today"`
	hub.PageParams
}

type Law struct {
	LawSummary
	AsOf          string       `json:"as_of"`
	NotYetInForce bool         `json:"not_yet_in_force,omitempty" jsonschema:"true when the requested article is not in force on as_of: it is shown from the amendment that adds it, which takes effect on effective_date"`
	Upcoming      []Upcoming   `json:"upcoming,omitempty" jsonschema:"amendments promulgated by as_of that take effect later; pass their effective_date as as_of to read them"`
	Articles      []Article    `json:"articles"`
	PageInfo      hub.PageInfo `json:"page_info"`
}

type Upcoming struct {
	EffectiveDate    string `json:"effective_date,omitempty" jsonschema:"empty when not set yet"`
	PromulgationDate string `json:"promulgation_date"`
	Amendment        string `json:"amendment,omitempty"`
}

type Article struct {
	Number  string `json:"number" jsonschema:"e.g. 제43조의2"`
	Title   string `json:"title,omitempty"`
	Chapter string `json:"chapter,omitempty" jsonschema:"the chapter (장) or section it belongs to"`
	Text    string `json:"text" jsonschema:"full text with paragraphs (항), subparagraphs (호) and items (목) on separate lines"`
	Deleted bool   `json:"deleted,omitempty"`
	URL     string `json:"url"`
}

var (
	lawIDPattern   = regexp.MustCompile(`^\d{6}$`)
	articlePattern = regexp.MustCompile(`^(?:제)?(\d{1,4})(?:조)?(?:의(\d{1,2}))?$`)
	deletedPattern = regexp.MustCompile(`^제\d+조(?:의\d+)?\s*삭제`)
)

func (c *client) getLaw(ctx context.Context, in getLawInput) (Law, error) {
	page, size, err := in.Resolve(20, 100)
	if err != nil {
		return Law{}, err
	}
	asOf, err := c.parseAsOf(in.AsOf)
	if err != nil {
		return Law{}, err
	}
	jo := ""
	if in.Article != "" {
		m := articlePattern.FindStringSubmatch(strings.ReplaceAll(in.Article, " ", ""))
		if m == nil {
			return Law{}, hub.Errorf(hub.CodeInvalidArgument, "article must look like 50, 제50조 or 제43조의2, got %q", in.Article)
		}
		n, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(cmp.Or(m[2], "0"))
		jo = fmt.Sprintf("%04d%02d", n, b)
	}

	id, err := c.resolveLaw(ctx, in.Law)
	if err != nil {
		return Law{}, err
	}
	version, upcoming, err := c.pickVersion(ctx, id, asOf)
	if err != nil {
		return Law{}, err
	}

	q := url.Values{"target": {"eflaw"}, "MST": {version.MST}, "efYd": {version.EfDate}}
	if jo != "" {
		q.Set("JO", jo)
	}
	var doc lawDoc
	if err := c.get(ctx, "lawService", q, &doc); err != nil {
		return Law{}, err
	}

	out := Law{LawSummary: version.summary(), AsOf: asOf.Format(time.DateOnly), Upcoming: upcoming, Articles: []Article{}}
	articles := doc.articles(out.Name)
	if jo != "" && len(articles) == 0 {
		if out, articles, err = c.upcomingArticle(ctx, id, jo, in.Article, out); err != nil {
			return Law{}, err
		}
	}
	out.PageInfo = hub.NewPageInfo(page, size, len(articles))
	for i := (page - 1) * size; i < len(articles) && i < page*size; i++ {
		out.Articles = append(out.Articles, articles[i])
	}
	return out, nil
}

// upcomingArticle handles an article missing from the text in force. It is
// often added by an amendment that is promulgated but not in effect yet
// (근로기준법 제44조의4), so the upcoming versions are checked, soonest
// first, and the first one that has it is returned, marked not_yet_in_force.
// "Not in force until 2027-01-01" answers the question; as an error it looked
// like a failure, and an agent retried every upcoming date in turn.
func (c *client) upcomingArticle(ctx context.Context, id, jo, article string, out Law) (Law, []Article, error) {
	var probed []string
	for _, u := range out.Upcoming {
		ef, err := time.ParseInLocation(time.DateOnly, u.EffectiveDate, kst)
		if err != nil || slices.Contains(probed, u.EffectiveDate) { // not set yet, or one of several amendments on that date
			continue
		}
		if probed = append(probed, u.EffectiveDate); len(probed) > maxUpcomingProbes {
			break
		}
		v, _, err := c.pickVersion(ctx, id, ef)
		if err != nil {
			return Law{}, nil, err
		}
		var doc lawDoc
		if err := c.get(ctx, "lawService", url.Values{"target": {"eflaw"}, "MST": {v.MST}, "efYd": {v.EfDate}, "JO": {jo}}, &doc); err != nil {
			return Law{}, nil, err
		}
		if articles := doc.articles(out.Name); len(articles) > 0 {
			out.LawSummary, out.NotYetInForce = v.summary(), true
			return out, articles, nil
		}
	}
	hint := "the article may have been renumbered or added by a later amendment; omit article to list them all"
	return Law{}, nil, hub.Errorf(hub.CodeNotFound, "%s has no article %s in force on %s", out.Name, article, out.AsOf).WithHint(hint)
}

func (c *client) parseAsOf(s string) (time.Time, error) {
	now := c.now().In(kst)
	if strings.TrimSpace(s) == "" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, kst), nil
	}
	t, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(s), kst)
	if err != nil || t.Year() < 1948 || t.Year() > 9000 {
		return time.Time{}, hub.Errorf(hub.CodeInvalidArgument, "as_of must be a date, YYYY-MM-DD, got %q", s)
	}
	return t, nil
}

// resolveLaw turns a law_id, name or abbreviation into a law_id. A name must
// match exactly (ignoring spaces) or be the only match.
func (c *client) resolveLaw(ctx context.Context, s string) (string, error) {
	s = strings.TrimSpace(s)
	if lawIDPattern.MatchString(s) {
		return s, nil
	}
	if s == "" {
		return "", hub.Errorf(hub.CodeInvalidArgument, "law is required: a name such as 근로기준법 or a 6-digit law_id")
	}
	rows, err := c.findLaws(ctx, s)
	if err != nil {
		return "", err
	}
	key := nameKey(s)
	for _, r := range rows {
		if nameKey(r.Name) == key || nameKey(r.Abbr) == key {
			return strings.TrimSpace(r.ID), nil
		}
	}
	switch len(rows) {
	case 0:
		return "", hub.Errorf(hub.CodeNotFound, "no law in force named %q", s).
			WithHint("use lawgokr_search_laws with part of the name, or its in_text option")
	case 1:
		return strings.TrimSpace(rows[0].ID), nil
	}
	var opts []string
	for _, r := range rows[:min(len(rows), 10)] {
		opts = append(opts, fmt.Sprintf("%s (%s)", strings.TrimSpace(r.Name), strings.TrimSpace(r.ID)))
	}
	return "", hub.Errorf(hub.CodeInvalidArgument, "%q matches %d laws: %s", s, len(rows), strings.Join(opts, ", ")).
		WithHint("pass the full name or the law_id")
}

// pickVersion returns the version of law id in force on asOf: the latest
// effective date on or before asOf, and among versions with that date the
// latest promulgation. It also returns the versions already promulgated on
// asOf that take effect later.
func (c *client) pickVersion(ctx context.Context, id string, asOf time.Time) (lawRow, []Upcoming, error) {
	var all []lawRow
	for page := 1; len(all) < maxVersionRows; page++ {
		var res lawSearch
		q := url.Values{"target": {"eflaw"}, "LID": {id}, "page": {strconv.Itoa(page)}, "display": {strconv.Itoa(searchPageRows)}}
		if err := c.get(ctx, "lawSearch", q, &res); err != nil {
			return lawRow{}, nil, err
		}
		all = append(all, res.Laws...)
		if len(res.Laws) < searchPageRows || len(all) >= res.TotalCnt {
			break
		}
	}
	if len(all) == 0 {
		return lawRow{}, nil, hub.Errorf(hub.CodeNotFound, "no law with law_id %s", id).
			WithHint("find the law_id with lawgokr_search_laws")
	}
	day := asOf.Format("20060102")
	var best *lawRow
	var upcoming []Upcoming
	for i, r := range all {
		ef := strings.TrimSpace(r.EfDate)
		if ef > day {
			if strings.TrimSpace(r.PromDate) > day { // not even promulgated on asOf
				continue
			}
			u := Upcoming{EffectiveDate: date(ef), PromulgationDate: date(r.PromDate), Amendment: strings.TrimSpace(r.Amend)}
			if ef == undeterminedEf {
				u.EffectiveDate = ""
			}
			if !slices.Contains(upcoming, u) {
				upcoming = append(upcoming, u)
			}
			continue
		}
		if best == nil || ef > best.EfDate || ef == best.EfDate && r.PromDate > best.PromDate {
			best = &all[i]
		}
	}
	if best == nil {
		return lawRow{}, nil, hub.Errorf(hub.CodeNotFound, "%s was not in force on %s", strings.TrimSpace(all[0].Name), asOf.Format(time.DateOnly)).
			WithHint("its first version took effect on " + date(all[len(all)-1].EfDate))
	}
	// Soonest first; an undetermined date last.
	sortKey := func(u Upcoming) string { return cmp.Or(u.EffectiveDate, "9999") }
	slices.SortFunc(upcoming, func(a, b Upcoming) int {
		return cmp.Or(cmp.Compare(sortKey(a), sortKey(b)), cmp.Compare(a.PromulgationDate, b.PromulgationDate))
	})
	return *best, upcoming, nil
}

type lawDoc struct {
	Units []lawUnit `xml:"조문>조문단위"`
}

type lawUnit struct {
	Num     string    `xml:"조문번호"`
	Branch  string    `xml:"조문가지번호"`
	Kind    string    `xml:"조문여부"` // 조문 (article) or 전문 (chapter heading)
	Title   string    `xml:"조문제목"`
	Content string    `xml:"조문내용"`
	Paras   []lawPara `xml:"항"`
	Items   []lawItem `xml:"호"` // subparagraphs directly under the article
}

type lawPara struct {
	Content string    `xml:"항내용"`
	Items   []lawItem `xml:"호"`
}

type lawItem struct {
	Content string `xml:"호내용"`
	Subs    []struct {
		Content string `xml:"목내용"`
	} `xml:"목"`
}

// articles assembles the articles, each with its full text and the chapter
// heading before it.
func (d lawDoc) articles(lawName string) []Article {
	var out []Article
	chapter := ""
	for _, u := range d.Units {
		content := strings.TrimSpace(u.Content)
		if strings.TrimSpace(u.Kind) != "조문" {
			chapter = strings.Join(strings.Fields(content), " ")
			continue
		}
		number := "제" + strings.TrimSpace(u.Num) + "조"
		if b := strings.TrimSpace(u.Branch); b != "" && b != "0" {
			number += "의" + b
		}
		lines := []string{content}
		addItems := func(items []lawItem) {
			for _, it := range items {
				lines = append(lines, strings.TrimSpace(it.Content))
				for _, s := range it.Subs {
					lines = append(lines, strings.TrimSpace(s.Content))
				}
			}
		}
		addItems(u.Items)
		for _, p := range u.Paras {
			lines = append(lines, strings.TrimSpace(p.Content))
			addItems(p.Items)
		}
		lines = slices.DeleteFunc(lines, func(s string) bool { return s == "" })
		out = append(out, Article{
			Number:  number,
			Title:   strings.TrimSpace(u.Title),
			Chapter: chapter,
			Text:    strings.Join(lines, "\n"),
			Deleted: deletedPattern.MatchString(content),
			URL:     lawURL(lawName, number),
		})
	}
	return out
}
