package datagokr

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/YoungsoonLee/mcp-hub/hub"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 국세청_사업자등록정보 진위확인 및 상태조회 서비스
// https://www.data.go.kr/data/15081808/openapi.do
//
// Unlike the other data.go.kr APIs this one is on api.odcloud.kr, takes a
// POST with a JSON body, and has its own error format. Its quirks:
//   - A number with hyphens (124-81-00998) is reported as not registered,
//     even when it is, so numbers are normalized before sending.
//   - "Not registered" is reported in the tax_type field of a normal result.
//   - An empty request is HTTP 411, more than 100 numbers HTTP 413, a bad key
//     HTTP 401 with {"code":-4,"msg":...}.
const (
	defaultODCloudURL = "https://api.odcloud.kr/api"
	ntsStatusPath     = "/nts-businessman/v1/status"
	maxBusinessNos    = 100
)

func registerBusiness(r *hub.Registrar, c *client) {
	hub.AddTool(r, &mcp.Tool{
		Name: "datagokr_check_business_status",
		Description: "Check Korean business registration numbers (사업자등록번호) with the National Tax Service (국세청): " +
			"whether each is registered, active, suspended (휴업) or closed (폐업) with the closing date, and its VAT type. " +
			"Up to 100 numbers per call; hyphens are fine. Use it to vet a counterparty or confirm a business still operates. " +
			"NTS data is updated about every 30 minutes; new businesses can take a day or two to appear.",
	}, c.checkBusinessStatus)
}

type checkBusinessStatusInput struct {
	BusinessNumbers []string `json:"business_numbers" jsonschema:"10-digit business registration numbers, e.g. 124-81-00998 or 1248100998"`
}

type BusinessStatus struct {
	BizRegNo          string `json:"business_registration_no"`
	Registered        bool   `json:"registered" jsonschema:"false if the NTS has no business with this number"`
	Status            string `json:"status,omitempty" jsonschema:"active (계속사업자), suspended (휴업자) or closed (폐업자)"`
	StatusKo          string `json:"status_ko,omitempty"`
	ClosedDate        string `json:"closed_date,omitempty" jsonschema:"YYYY-MM-DD, when closed"`
	TaxType           string `json:"tax_type,omitempty" jsonschema:"general_vat, simplified_vat, simplified_vat_invoicing, special_vat or vat_exempt; omitted for other entity types (see tax_type_ko)"`
	TaxTypeKo         string `json:"tax_type_ko,omitempty"`
	TaxTypeChangedOn  string `json:"tax_type_changed_date,omitempty"`
	PreviousTaxTypeKo string `json:"previous_tax_type_ko,omitempty"`
	InvoiceApplyDate  string `json:"invoice_apply_date,omitempty" jsonschema:"date from which the business issues tax invoices (세금계산서)"`
}

type ntsStatusResponse struct {
	StatusCode string `json:"status_code"`
	Data       []struct {
		BNo             string `json:"b_no"`
		BStt            string `json:"b_stt"`
		BSttCd          string `json:"b_stt_cd"`
		TaxType         string `json:"tax_type"`
		TaxTypeCd       string `json:"tax_type_cd"`
		EndDt           string `json:"end_dt"`
		TaxTypeChangeDt string `json:"tax_type_change_dt"`
		InvoiceApplyDt  string `json:"invoice_apply_dt"`
		RbfTaxType      string `json:"rbf_tax_type"`
		RbfTaxTypeCd    string `json:"rbf_tax_type_cd"`
	} `json:"data"`
}

var businessStatuses = map[string]string{"01": "active", "02": "suspended", "03": "closed"}

var taxTypes = map[string]string{
	"01": "general_vat",              // 부가가치세 일반과세자
	"02": "simplified_vat",           // 부가가치세 간이과세자
	"03": "special_vat",              // 부가가치세 과세특례자
	"04": "vat_exempt",               // 부가가치세 면세사업자
	"07": "simplified_vat_invoicing", // 부가가치세 간이과세자(세금계산서 발급사업자)
}

var bizNoPattern = regexp.MustCompile(`^\d{10}$`)

func (c *client) checkBusinessStatus(ctx context.Context, in checkBusinessStatusInput) (hub.List[BusinessStatus], error) {
	n := len(in.BusinessNumbers)
	if n == 0 || n > maxBusinessNos {
		return hub.List[BusinessStatus]{}, hub.Errorf(hub.CodeInvalidArgument, "business_numbers must have 1 to %d numbers, got %d", maxBusinessNos, n)
	}
	nos := make([]string, 0, n)
	var bad []string
	for _, s := range in.BusinessNumbers {
		no := strings.NewReplacer("-", "", " ", "").Replace(s)
		if !bizNoPattern.MatchString(no) {
			bad = append(bad, s)
		}
		nos = append(nos, no)
	}
	if len(bad) > 0 {
		return hub.List[BusinessStatus]{}, hub.Errorf(hub.CodeInvalidArgument,
			"business registration numbers must have 10 digits: %s", strings.Join(bad, ", "))
	}

	var r ntsStatusResponse
	q := url.Values{"serviceKey": {c.key}}
	if err := c.odcloud.PostJSON(ctx, c.odcloudBase+ntsStatusPath, q, map[string][]string{"b_no": nos}, &r); err != nil {
		return hub.List[BusinessStatus]{}, err
	}
	if r.StatusCode != "OK" {
		return hub.List[BusinessStatus]{}, hub.Errorf(hub.CodeUpstream, "NTS status lookup failed: %s", r.StatusCode).WithUpstreamCode(r.StatusCode)
	}

	byNo := map[string]BusinessStatus{}
	for _, d := range r.Data {
		s := BusinessStatus{BizRegNo: d.BNo}
		if d.BSttCd != "" {
			s.Registered = true
			s.Status = businessStatuses[d.BSttCd]
			s.StatusKo = text(d.BStt)
			s.ClosedDate = date(d.EndDt)
			s.TaxType = taxTypes[d.TaxTypeCd]
			s.TaxTypeKo = text(d.TaxType)
			s.TaxTypeChangedOn = date(d.TaxTypeChangeDt)
			s.InvoiceApplyDate = date(d.InvoiceApplyDt)
			if d.RbfTaxTypeCd != "" && d.RbfTaxTypeCd != "99" { // 99: 해당없음
				s.PreviousTaxTypeKo = text(d.RbfTaxType)
			}
		}
		byNo[d.BNo] = s
	}
	out := hub.List[BusinessStatus]{Items: []BusinessStatus{}, PageInfo: hub.NewPageInfo(1, n, n)}
	for _, no := range nos {
		s, ok := byNo[no]
		if !ok {
			s = BusinessStatus{BizRegNo: no}
		}
		out.Items = append(out.Items, s)
	}
	return out, nil
}

// odcloudError decodes the error bodies of api.odcloud.kr:
// {"code":-4,"msg":"등록되지 않은 인증키 입니다."} or {"status_code":"TOO_LARGE_REQUEST"}.
func odcloudError(status int, body []byte) error {
	var e struct {
		Code       int    `json:"code"`
		Msg        string `json:"msg"`
		StatusCode string `json:"status_code"`
	}
	if json.Unmarshal(body, &e) != nil {
		return nil
	}
	msg := cmp.Or(e.Msg, e.StatusCode)
	if msg == "" {
		return nil
	}
	upstream := e.StatusCode
	if e.Code != 0 {
		upstream = strconv.Itoa(e.Code)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return hub.Errorf(hub.CodeUnauthenticated, "data.go.kr: %s", msg).WithUpstreamCode(upstream).WithHint(resultCodes["30"].hint)
	case http.StatusBadRequest, http.StatusLengthRequired, http.StatusRequestEntityTooLarge:
		return hub.Errorf(hub.CodeInvalidArgument, "data.go.kr: %s", msg).WithUpstreamCode(upstream)
	}
	return nil
}
