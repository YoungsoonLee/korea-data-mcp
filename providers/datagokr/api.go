// Package datagokr provides tools for Korean government open data served
// through the Public Data Portal (공공데이터포털, data.go.kr).
//
// One service key (DATAGOKR_SERVICE_KEY) works for every API the account has
// applied for, and the APIs share a response envelope. They still differ in
// the details, so this file normalizes them once for every tool. The quirks
// below were observed on the live APIs; docs/QUIRKS.md lists them all.
//
//   - Success is resultCode "00" for some services and "000" for others.
//   - Errors come in two shapes: an OpenAPI_ServiceResponse document (with
//     HTTP 403 for key problems, HTTP 200 for others such as a malformed
//     numOfRows), or a non-success resultCode inside a normal response.
//   - In JSON a single result is an object instead of an array, and no
//     results is "" in some services and {"item":[]} in others. XML has none
//     of these problems, so every request asks for XML.
package datagokr

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/YoungsoonLee/mcp-hub/httpx"
	"github.com/YoungsoonLee/mcp-hub/hub"
)

const (
	defaultBaseURL = "https://apis.data.go.kr"
	envKey         = "DATAGOKR_SERVICE_KEY"
)

type client struct {
	base string
	key  string
	http *httpx.Client
	now  func() time.Time
}

func newClient(base, key string) *client {
	return &client{
		base: base,
		key:  key,
		// Public datasets change at most daily; only normal responses are
		// cached, since errors also arrive as HTTP 200.
		http: httpx.New(
			httpx.WithCache(10*time.Minute, 1000),
			httpx.WithCacheCheck(cacheable),
			httpx.WithErrorBody(func(_ int, body []byte) error { return serviceError(body) }),
		),
	}
}

// envelope is the standard data.go.kr response. Items holds the <item>
// elements, whose fields differ per API.
type envelope[T any] struct {
	Header struct {
		ResultCode string `xml:"resultCode"`
		ResultMsg  string `xml:"resultMsg"`
	} `xml:"header"`
	Body struct {
		Items      []T `xml:"items>item"`
		NumOfRows  int `xml:"numOfRows"`
		PageNo     int `xml:"pageNo"`
		TotalCount int `xml:"totalCount"`
	} `xml:"body"`
}

// getList calls path (relative to the base URL) and returns one page of items
// and the total count. "No data" is an empty list, not an error.
func getList[T any](ctx context.Context, c *client, path string, q url.Values, page, size int) ([]T, int, error) {
	q.Set("serviceKey", c.key)
	q.Set("pageNo", strconv.Itoa(page))
	q.Set("numOfRows", strconv.Itoa(size))
	body, err := c.http.Get(ctx, c.base+path, q, nil)
	if err != nil {
		return nil, 0, err
	}
	return decodeList[T](body)
}

func decodeList[T any](body []byte) ([]T, int, error) {
	if err := serviceError(body); err != nil {
		return nil, 0, err
	}
	var env envelope[T]
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, 0, hub.Errorf(hub.CodeUpstream, "invalid XML from data.go.kr").Wrap(err)
	}
	if err := resultError(env.Header.ResultCode, env.Header.ResultMsg); err != nil {
		return nil, 0, err
	}
	items := env.Body.Items
	if items == nil {
		items = []T{}
	}
	return items, env.Body.TotalCount, nil
}

// cacheable reports whether body is a normal or "no data" response.
func cacheable(body []byte) bool {
	if serviceError(body) != nil {
		return false
	}
	var env envelope[struct{}]
	if xml.Unmarshal(body, &env) != nil {
		return false
	}
	return resultError(env.Header.ResultCode, env.Header.ResultMsg) == nil
}

// serviceHeader is the body of an OpenAPI_ServiceResponse error document.
type serviceHeader struct {
	ErrMsg           string `xml:"errMsg" json:"errMsg"`
	ReturnAuthMsg    string `xml:"returnAuthMsg" json:"returnAuthMsg"`
	ReturnReasonCode string `xml:"returnReasonCode" json:"returnReasonCode"`
}

// serviceError decodes an OpenAPI_ServiceResponse error document, in either
// XML or JSON (the gateway answers in the format that was requested). It
// returns nil if body is not one.
func serviceError(body []byte) error {
	if !bytes.Contains(body, []byte("OpenAPI_ServiceResponse")) {
		return nil
	}
	var h serviceHeader
	if t := bytes.TrimSpace(body); len(t) > 0 && t[0] == '{' {
		var doc struct {
			R struct {
				H serviceHeader `json:"cmmMsgHeader"`
			} `json:"OpenAPI_ServiceResponse"`
		}
		if json.Unmarshal(t, &doc) != nil {
			return nil
		}
		h = doc.R.H
	} else {
		var doc struct {
			H serviceHeader `xml:"cmmMsgHeader"`
		}
		if xml.Unmarshal(t, &doc) != nil {
			return nil
		}
		h = doc.H
	}
	msg := h.ErrMsg
	if h.ReturnAuthMsg != "" && h.ReturnAuthMsg != h.ErrMsg {
		msg += " (" + h.ReturnAuthMsg + ")"
	}
	return resultError(h.ReturnReasonCode, msg)
}

type codeInfo struct {
	code hub.Code
	hint string
}

// resultCodes maps data.go.kr's common result codes (공통 에러 코드 in each
// API's guide), keyed without leading zeros because services use both "03"
// and "003".
var resultCodes = map[string]codeInfo{
	"1":  {hub.CodeUpstream, ""},    // APPLICATION_ERROR
	"2":  {hub.CodeUnavailable, ""}, // DB_ERROR
	"4":  {hub.CodeUnavailable, ""}, // HTTP_ERROR, also returned for a malformed numOfRows/pageNo
	"5":  {hub.CodeUnavailable, ""}, // SERVICETIME_OUT
	"10": {hub.CodeInvalidArgument, ""},
	"11": {hub.CodeInvalidArgument, ""},
	"12": {hub.CodeUpstream, "this data.go.kr API has been discontinued or moved"},
	"20": {hub.CodePermission, "this API is not approved for the key; apply for it (활용신청) on its data.go.kr page"},
	"21": {hub.CodePermission, "the service key is temporarily disabled; check My Page on data.go.kr"},
	"22": {hub.CodeRateLimited, "daily call limit reached (10,000 per API on a development account); retry tomorrow or request an operating account on data.go.kr"},
	"30": {hub.CodeUnauthenticated, "the key is not registered for this API: apply for it (활용신청) on its data.go.kr page, set " + envKey + " to the Decoding key (not the Encoding key), and allow a few hours after approval for the key to activate"},
	"31": {hub.CodeUnauthenticated, "the service key's usage period has expired; extend it on My Page on data.go.kr"},
	"32": {hub.CodePermission, "this server's IP address is not registered for the key on data.go.kr"},
	"33": {hub.CodeUnauthenticated, ""},
	"99": {hub.CodeUpstream, ""},
}

// resultError maps a result code to a hub error. Success ("00", "000") and
// "no data" ("03") return nil.
func resultError(code, msg string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return hub.Errorf(hub.CodeUpstream, "data.go.kr response has no result code")
	}
	n := strings.TrimLeft(code, "0")
	if n == "" || n == "3" {
		return nil
	}
	info, ok := resultCodes[n]
	if !ok {
		info = codeInfo{code: hub.CodeUpstream}
	}
	e := hub.Errorf(info.code, "data.go.kr: %s", msg).WithUpstreamCode(code)
	if info.hint != "" {
		e = e.WithHint(info.hint)
	}
	return e
}
