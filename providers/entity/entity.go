// Package entity resolves a Korean company from any identifier: name, DART
// corp_code, corporate registration number (법인등록번호), business
// registration number (사업자등록번호), stock code or ISIN. It has no API of
// its own; it combines the opendart and datagokr tools, because each source
// alone gives wrong answers:
//   - DART's company list keeps stock codes after delisting, lacks them for
//     new listings and preferred shares, and keeps some old names
//     (000040 is KR모터스 there, (주)다이나맥 in its profile).
//   - Thousands of names are shared: two 카카오 file with DART, one closed
//     in 2014.
//   - The FSC registry knows former names, but a search by a former name
//     returns only the old records.
package entity

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/YoungsoonLee/korea-data-mcp/hub"
	"github.com/YoungsoonLee/korea-data-mcp/normalize"
	"github.com/YoungsoonLee/korea-data-mcp/providers/datagokr"
	"github.com/YoungsoonLee/korea-data-mcp/providers/opendart"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Info() hub.ProviderInfo {
	return hub.ProviderInfo{
		ID:          "entity",
		Title:       "Company resolution (기업 식별)",
		Description: "Resolve a Korean company from a name or any of its numbers to all of its identifiers, current and former names, listing and business status, using the opendart and datagokr tools.",
	}
}

func (p *Provider) Register(r *hub.Registrar) error {
	res := &resolver{call: r.Call}
	hub.AddTool(r, &mcp.Tool{
		Name: "entity_resolve_company",
		Description: "Identify a Korean company from a name, DART corp_code (8 digits), corporate registration number (법인등록번호, 13 digits), " +
			"business registration number (사업자등록번호, 10 digits), stock code (6 characters, preferred shares too) or ISIN. " +
			"Returns every identifier the other tools need (corp_code for opendart, crno and business number for datagokr), the current name and former names, " +
			"whether it is listed now, and whether the business is active or closed (National Tax Service). " +
			"Use it first when a company is named loosely or by a number: many companies share a name (two 카카오, one closed in 2014), " +
			"and DART keeps stock codes of delisted companies. A name returns every company with that exact name, best match first. " +
			"Works with OPENDART_API_KEY, DATAGOKR_SERVICE_KEY or both; with one, the other's fields are missing and incomplete says so.",
	}, res.resolve)
	return nil
}

type caller func(ctx context.Context, name string, args, out any) error

type resolver struct{ call caller }

type resolveInput struct {
	Query string `json:"query" jsonschema:"company name (삼성전자, 카카오, 대우조선해양), corp_code, crno, business number, stock code or ISIN; hyphens allowed"`
}

type Company struct {
	EntityID       string                `json:"entity_id" jsonschema:"stable ID: crno:<법인등록번호> when known, else dart:<corp_code> or bizno:<사업자등록번호>"`
	Name           string                `json:"name" jsonschema:"current legal name"`
	NameEn         string                `json:"name_en,omitempty"`
	MatchedName    string                `json:"matched_name,omitempty" jsonschema:"the name the query matched, when it is not the current one"`
	FormerNames    []datagokr.FormerName `json:"former_names,omitempty" jsonschema:"oldest first, from records since about 2020"`
	CorpCode       string                `json:"corp_code,omitempty" jsonschema:"DART 고유번호 for opendart tools; empty if the company does not file with DART"`
	Crno           string                `json:"crno,omitempty" jsonschema:"법인등록번호 for datagokr_get_corporation"`
	BusinessNumber string                `json:"business_number,omitempty" jsonschema:"사업자등록번호"`
	StockCode      string                `json:"stock_code,omitempty" jsonschema:"common share; kept after delisting"`
	StockName      string                `json:"stock_name,omitempty"`
	Listed         bool                  `json:"listed" jsonschema:"listed on KOSPI, KOSDAQ or KONEX now"`
	Market         string                `json:"market,omitempty" jsonschema:"KOSPI, KOSDAQ, KONEX, or OTHER (unlisted or delisted)"`
	BusinessStatus string                `json:"business_status,omitempty" jsonschema:"active, suspended, closed or unregistered (National Tax Service); empty if not checked"`
	ClosedDate     string                `json:"closed_date,omitempty"`
	CEO            string                `json:"ceo,omitempty"`
}

type Resolution struct {
	Query      string    `json:"query"`
	MatchedBy  string    `json:"matched_by" jsonschema:"corp_code, crno, business_number, stock_code, isin or name"`
	Companies  []Company `json:"companies" jsonschema:"best match first: exact name, active business, listed"`
	Note       string    `json:"note,omitempty"`
	Incomplete []string  `json:"incomplete,omitempty" jsonschema:"sources that could not be checked and why, such as a missing API key"`
}

var (
	digitsPattern    = regexp.MustCompile(`^\d+$`)
	stockCodePattern = regexp.MustCompile(`^[0-9][0-9A-Z]{5}$`)
	isinPattern      = regexp.MustCompile(`^[A-Z]{2}[0-9A-Z]{9}[0-9]$`)
)

const maxCompanies = 10

func (rs *resolver) resolve(ctx context.Context, in resolveInput) (Resolution, error) {
	q := strings.TrimSpace(in.Query)
	if normalize.CompanyName(q) == "" {
		return Resolution{}, hub.Errorf(hub.CodeInvalidArgument, "query is required")
	}
	x := &run{ctx: ctx, call: rs.call}
	out := Resolution{Query: q, Companies: []Company{}}
	id := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(q))
	var (
		cands []*cand
		err   error
	)
	switch {
	case digitsPattern.MatchString(id) && len(id) == 8:
		out.MatchedBy = "corp_code"
		cands, err = x.seedCorpCode(id)
	case digitsPattern.MatchString(id) && len(id) == 13:
		out.MatchedBy = "crno"
		cands = []*cand{{Company: Company{Crno: id}}}
	case digitsPattern.MatchString(id) && len(id) == 10:
		out.MatchedBy = "business_number"
		cands, err = x.seedBusinessNumber(id)
	case isinPattern.MatchString(id):
		out.MatchedBy = "isin"
		if code := x.isinCode(id); code != "" {
			cands, out.Note, err = x.seedStockCode(code)
		}
	case stockCodePattern.MatchString(id) && strings.ContainsAny(id, "0123456789"):
		out.MatchedBy = "stock_code"
		cands, out.Note, err = x.seedStockCode(id)
	default:
		out.MatchedBy = "name"
		cands, out.Note, err = x.seedName(q)
	}
	if err != nil {
		return Resolution{}, err
	}

	x.enrich(cands)
	if out.MatchedBy != "name" {
		// A number that no source knows matches nothing.
		cands = slices.DeleteFunc(cands, func(c *cand) bool { return !c.found })
	}
	if len(cands) == 0 {
		return Resolution{}, x.notFound(q)
	}
	slices.SortStableFunc(cands, compareCands)
	seen := map[string]bool{}
	for _, c := range cands {
		// A search by a former name and one by the current name can find
		// the same company.
		if c.EntityID = entityID(c.Company); seen[c.EntityID] {
			continue
		}
		seen[c.EntityID] = true
		out.Companies = append(out.Companies, c.Company)
	}
	if out.MatchedBy == "business_number" && out.Companies[0].Name == "" {
		out.Note = "Only the National Tax Service knows this number, without a name: a sole proprietor's, or a corporation missing from the FSC registry."
	}
	if out.MatchedBy == "name" && len(out.Companies) > 1 && cands[1].exact {
		out.Note = strings.TrimSpace(out.Note + " " + fmt.Sprintf("%d companies have this name; check corp_code, business_status and listed before using one.", countExact(cands)))
	}
	out.Incomplete = x.incomplete()
	return out, nil
}

// cand is a company being resolved.
type cand struct {
	Company
	exact bool // the query is its name
	found bool // some source knows it
	order int  // position in the search results
}

func entityID(c Company) string {
	switch {
	case c.Crno != "":
		return "crno:" + c.Crno
	case c.CorpCode != "":
		return "dart:" + c.CorpCode
	}
	return "bizno:" + c.BusinessNumber
}

func compareCands(a, b *cand) int {
	return cmp.Or(
		compareBool(a.exact, b.exact),
		compareBool(a.BusinessStatus != "closed" && a.BusinessStatus != "unregistered", b.BusinessStatus != "closed" && b.BusinessStatus != "unregistered"),
		compareBool(a.Listed, b.Listed),
		compareBool(a.StockCode != "", b.StockCode != ""),
		compareBool(a.CorpCode != "", b.CorpCode != ""),
		cmp.Compare(a.order, b.order),
	)
}

// compareBool orders true before false.
func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}
	return 1
}

func countExact(cands []*cand) int {
	n := 0
	for _, c := range cands {
		if c.exact {
			n++
		}
	}
	return n
}

// run holds one resolution's calls and the sources that failed.
type run struct {
	ctx    context.Context
	call   caller
	mu     sync.Mutex
	failed map[string]*hub.Error // by provider
	worked map[string]bool
}

// do calls a tool and records why it failed, unless it found nothing.
func (x *run) do(name string, args, out any) error {
	err := x.call(x.ctx, name, args, out)
	p := strings.SplitN(name, "_", 2)[0]
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.failed == nil {
		x.failed, x.worked = map[string]*hub.Error{}, map[string]bool{}
	}
	if e := hub.AsError(err); e != nil && e.Code != hub.CodeNotFound {
		if _, ok := x.failed[p]; !ok {
			x.failed[p] = e
		}
	} else {
		x.worked[p] = true
	}
	return err
}

func (x *run) incomplete() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []string
	for p, e := range x.failed {
		msg := e.Message
		if !strings.HasPrefix(msg, p) {
			msg = p + ": " + msg
		}
		out = append(out, msg)
	}
	slices.Sort(out)
	return out
}

// notFound explains why nothing matched: every source that answered found
// nothing, or none could answer.
func (x *run) notFound(q string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.worked) == 0 && len(x.failed) > 0 {
		var missing []string
		for _, e := range x.failed {
			if e.Code != hub.CodeUnauthenticated {
				return e
			}
			missing = append(missing, e.Message)
		}
		slices.Sort(missing)
		return hub.Errorf(hub.CodeUnauthenticated, "no source could be checked: %s", strings.Join(missing, "; ")).
			WithHint("set OPENDART_API_KEY or DATAGOKR_SERVICE_KEY; see hub_list_providers")
	}
	e := hub.Errorf(hub.CodeNotFound, "no company matches %q", q).WithHint("check the number, or try the company's name")
	if len(x.failed) > 0 {
		e.Hint += "; not checked: "
		for p := range x.failed {
			e.Hint += p + " "
		}
		e.Hint = strings.TrimSpace(e.Hint)
	}
	return e
}

// ---- seeds ----

func (x *run) seedCorpCode(code string) ([]*cand, error) {
	return []*cand{{Company: Company{CorpCode: code}}}, nil
}

func (x *run) seedBusinessNumber(no string) ([]*cand, error) {
	var p datagokr.CorporationProfile
	if err := x.do("datagokr_get_corporation", map[string]any{"business_number": no}, &p); err == nil {
		c := &cand{found: true}
		c.fromFSC(p)
		c.BusinessNumber = no
		return []*cand{c}, nil
	}
	// Sole proprietors and corporations missing from the FSC registry are
	// known to the National Tax Service only.
	return []*cand{{Company: Company{BusinessNumber: no}}}, nil
}

// isinCode returns the common share code of the company an ISIN belongs to.
// A Korean share's ISIN holds the first 5 characters of its code, then the
// share class: 삼성전자우 (005935) is KR7005931001. This held for all 2,854
// KR7 ISINs listed on 2026-11-11. Foreign companies listed in Korea have
// their own ISINs (소마젠 KR8840150005, 엘브이엠씨홀딩스 KYG5307W1015),
// looked up in KRX prices.
func (x *run) isinCode(isin string) string {
	if strings.HasPrefix(isin, "KR7") {
		return isin[3:8] + "0"
	}
	var sp datagokr.StockPrices
	if x.do("datagokr_get_stock_prices", map[string]any{"stock": isin, "page_size": 1}, &sp) != nil {
		return ""
	}
	return sp.Stock.Code
}

// seedStockCode finds the company of a stock code. DART lists common shares
// only, and a new listing can lack its code there for weeks; KRX prices
// then give the name.
func (x *run) seedStockCode(code string) ([]*cand, string, error) {
	var note string
	if code[5] != '0' {
		note = fmt.Sprintf("%s is not a common share; the company's common share code is usually %s0.", code, code[:5])
	}
	var p opendart.CompanyProfile
	if err := x.do("opendart_get_company", map[string]any{"corp_code": code}, &p); err == nil {
		c := &cand{found: true}
		c.fromDART(p)
		return []*cand{c}, note, nil
	}
	var sp datagokr.StockPrices
	if err := x.do("datagokr_get_stock_prices", map[string]any{"stock": code, "page_size": 1}, &sp); err != nil || sp.Stock.Name == "" {
		return nil, note, nil
	}
	var dart hub.List[opendart.Company]
	if x.do("opendart_search_companies", map[string]any{"query": sp.Stock.Name}, &dart) == nil {
		var exact []opendart.Company
		for _, d := range dart.Items {
			if sameName(d.CorpName, sp.Stock.Name) {
				exact = append(exact, d)
			}
		}
		if len(exact) == 1 {
			c := &cand{Company: Company{CorpCode: exact[0].CorpCode}}
			return []*cand{c}, strings.TrimSpace(note + " DART's company list does not have this stock code yet; matched by the listed name " + sp.Stock.Name + "."), nil
		}
	}
	c := &cand{found: true, Company: Company{Name: sp.Stock.Name, StockName: sp.Stock.Name, StockCode: code, Listed: true, Market: sp.Stock.Market}}
	return []*cand{c}, strings.TrimSpace(note + " Found in KRX prices only; search the name with opendart_search_companies later for its corp_code."), nil
}

// seedName collects the companies DART and the FSC registry find for a name.
// When some have exactly that name, only they are kept.
func (x *run) seedName(q string) ([]*cand, string, error) {
	var (
		dart hub.List[opendart.Company]
		fsc  hub.List[datagokr.Corporation]
		wg   sync.WaitGroup
	)
	wg.Go(func() { x.do("opendart_search_companies", map[string]any{"query": q, "page_size": 20}, &dart) })
	wg.Go(func() { x.do("datagokr_search_corporations", map[string]any{"query": q, "page_size": 20}, &fsc) })
	wg.Wait()

	var cands []*cand
	byCode := map[string]*cand{}
	for _, d := range dart.Items {
		c := &cand{found: true, exact: sameName(d.CorpName, q), order: len(cands), Company: Company{
			Name: d.CorpName, CorpCode: d.CorpCode, StockCode: d.StockCode, Listed: d.Listed, Market: d.Market,
		}}
		cands = append(cands, c)
		byCode[d.CorpCode] = c
	}
	for _, f := range fsc.Items {
		exact := sameName(f.Name, q)
		if c := byCode[f.DartCorpCode]; f.DartCorpCode != "" && c != nil {
			c.Crno, c.exact = f.Crno, c.exact || exact
			if !sameName(c.Name, f.Name) && exact {
				c.MatchedName = f.Name
			}
			continue
		}
		c := &cand{found: true, exact: exact, order: len(cands), Company: Company{
			Name: f.Name, NameEn: f.NameEn, CorpCode: f.DartCorpCode, Crno: f.Crno, BusinessNumber: f.BizRegNo, Market: f.Market, CEO: f.CEO,
		}}
		c.Listed = c.Market == "KOSPI" || c.Market == "KOSDAQ" || c.Market == "KONEX"
		if exact {
			c.MatchedName = f.Name // replaced by the current name if it changed
		}
		cands = append(cands, c)
	}

	var note string
	if exact := slices.DeleteFunc(slices.Clone(cands), func(c *cand) bool { return !c.exact }); len(exact) > 0 {
		cands = exact
	} else if len(cands) > 0 {
		note = "No company has exactly this name; these are the closest."
		cands = cands[:min(len(cands), 5)]
	}
	if len(cands) > maxCompanies {
		note = fmt.Sprintf("%d companies have this name; the first %d are checked.", len(cands), maxCompanies)
		cands = cands[:maxCompanies]
	}
	return cands, note, nil
}

func sameName(a, b string) bool {
	return a != "" && normalize.CompanyKey(a) == normalize.CompanyKey(b)
}

// ---- enrichment ----

// enrich completes each company from DART's profile and the FSC registry,
// then checks business numbers with the National Tax Service.
func (x *run) enrich(cands []*cand) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, c := range cands {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			x.enrichOne(c)
		})
	}
	wg.Wait()

	var nos []string
	for _, c := range cands {
		if validBusinessNumber(c.BusinessNumber) && !slices.Contains(nos, c.BusinessNumber) {
			nos = append(nos, c.BusinessNumber)
		}
	}
	if len(nos) == 0 {
		return
	}
	var st hub.List[datagokr.BusinessStatus]
	if x.do("datagokr_check_business_status", map[string]any{"business_numbers": nos}, &st) != nil {
		return
	}
	byNo := map[string]datagokr.BusinessStatus{}
	for _, s := range st.Items {
		byNo[s.BizRegNo] = s
	}
	for _, c := range cands {
		s, ok := byNo[c.BusinessNumber]
		switch {
		case !ok:
		case !s.Registered:
			c.BusinessStatus = "unregistered"
		default:
			c.BusinessStatus, c.ClosedDate = s.Status, s.ClosedDate
			c.found = true
		}
	}
}

func (x *run) enrichOne(c *cand) {
	fscDone := false
	fsc := func() {
		fscDone = true
		var p datagokr.CorporationProfile
		if x.do("datagokr_get_corporation", map[string]any{"crno": c.Crno}, &p) == nil {
			c.fromFSC(p)
			c.found = true
		}
	}
	if c.CorpCode == "" && c.Crno != "" {
		fsc()
	}
	if c.CorpCode != "" {
		var p opendart.CompanyProfile
		if x.do("opendart_get_company", map[string]any{"corp_code": c.CorpCode}, &p) == nil {
			c.fromDART(p)
			c.found = true
		}
	}
	if !fscDone && c.Crno != "" {
		fsc()
	}
	if c.MatchedName != "" && sameName(c.MatchedName, c.Name) {
		c.MatchedName = ""
	}
}

// fromDART fills a company from DART's profile, which has the current name
// and corp_cls; DART's company list can have an old name.
func (c *cand) fromDART(p opendart.CompanyProfile) {
	if c.Name != "" && !sameName(c.Name, p.CorpName) && c.MatchedName == "" && c.exact {
		c.MatchedName = c.Name
	}
	c.CorpCode, c.Name = p.CorpCode, p.CorpName
	c.NameEn = cmp.Or(p.CorpNameEng, c.NameEn)
	c.StockCode, c.StockName = cmp.Or(p.StockCode, c.StockCode), p.StockName
	c.Market = p.Market
	c.Listed = p.Market == "KOSPI" || p.Market == "KOSDAQ" || p.Market == "KONEX"
	c.CEO = cmp.Or(p.CEO, c.CEO)
	// Foreign companies listed in Korea have no crno, and junk such as
	// MC178082 as their business number.
	if validCrno(p.CorporateRegistration) {
		c.Crno = p.CorporateRegistration
	}
	if validBusinessNumber(p.BusinessRegistration) {
		c.BusinessNumber = p.BusinessRegistration
	}
}

// fromFSC fills what DART lacks from the FSC registry, and the former names.
func (c *cand) fromFSC(p datagokr.CorporationProfile) {
	c.Crno = p.Crno
	c.CorpCode = cmp.Or(c.CorpCode, p.DartCorpCode)
	c.Name = cmp.Or(c.Name, p.Name)
	if c.CorpCode == "" || c.Market == "" {
		c.Name = p.Name
		c.Market = p.Market
		c.Listed = p.Market == "KOSPI" || p.Market == "KOSDAQ" || p.Market == "KONEX"
	}
	c.NameEn = cmp.Or(c.NameEn, p.NameEn)
	c.BusinessNumber = cmp.Or(c.BusinessNumber, p.BizRegNo)
	c.CEO = cmp.Or(c.CEO, p.CEO)
	c.FormerNames = p.FormerNames
}

var (
	crnoPattern  = regexp.MustCompile(`^\d{13}$`)
	bizNoPattern = regexp.MustCompile(`^\d{10}$`)
)

// Real numbers fail the published checksums (삼양바이오팜's crno), so only
// the shape is checked.
func validCrno(s string) bool           { return crnoPattern.MatchString(s) }
func validBusinessNumber(s string) bool { return bizNoPattern.MatchString(s) }
