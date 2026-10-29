# What korea-data-mcp handles for you

**English** | [한국어](QUIRKS.ko.md)

Korean public APIs work, but each one has its own surprises. An agent that calls them directly has to discover every one of these by failing, and some of them do not fail at all: they return a plausible but wrong answer.

Everything below was **observed on the live APIs** (last checked 2026-10-28), and the real responses are kept in each provider's `testdata/` so the handling is tested.

Status: ✅ handled · 🚧 handled when the tool that needs it ships

## Wrong answers without an error

These are the most dangerous ones: nothing fails, the agent simply gets the wrong number.

| Source | What the API does | What korea-data-mcp does | |
|---|---|---|---|
| OpenDART | In half-year and quarterly reports, income statement items are labeled with the year-to-date range (`2025.01.01 ~ 2025.06.30`) but `thstrm_amount` holds **only the latest quarter**. The year-to-date figure is in a separate `thstrm_add_amount`. | `current_amount` always matches `current_period`; the quarter is in `current_quarter_amount`. | ✅ |
| data.go.kr | An invalid parameter is not an error. `DEAL_YMD=2025` (instead of `202509`) or `solMonth=13` returns **success with zero results**, which looks like "no data". | Every tool validates its inputs and returns `INVALID_ARGUMENT` with the expected format. | ✅ |
| data.go.kr (holidays) | Years without data (before 2004, or more than two years ahead) also return **success with zero results**, so "no holidays in 2030" looks like a fact. | The year range is checked, and the error message states it. | ✅ |
| data.go.kr (holidays) | The "national holidays" (국경일) operation returns **all 22 public holidays** of 2026, the same as the "public holidays" operation, not the five national holidays. | That operation is not exposed. Public holidays come from the public holidays operation only. | ✅ |
| data.go.kr (apartment trades) | Prices are strings in **units of 10,000 KRW** (`"84,500"` is ₩845,000,000). | Amounts are plain KRW integers. | ✅ |
| data.go.kr (apartment trades) | **Cancelled deals stay in the data**, marked only by `cdealType: "O"`: 12 of 233 Gangnam trades in 2025-09, including a ₩7.5 billion deal re-signed the same day for ₩7.25 billion. Counting them skews prices. | Excluded by default and never counted in the price summary; `include_cancelled` lists them with the cancellation date. | ✅ |
| data.go.kr (apartment trades) | When a city is reorganized, **past data moves to the new district codes**. 화성시's well-known code `41590` now returns zero trades even for 2021; the data is under its four new 구 codes. | A district table generated from the official code list. Districts are given by name, and a city name expands to all its districts: `화성시` (or `41590`) queries 41591, 41593, 41595 and 41597. | ✅ |
| data.go.kr (apartment trades) | 광주광역시 (`29xxx`) and 전라남도 (`46xxx`) merged into 전남광주통합특별시 (`12xxx`), and 인천's 중구, 동구 and 서구 became 제물포구, 영종구, 서해구 and 검단구. The old codes return zero trades, even for 2025. | `광주 북구`, `광주광역시 광산구` and `전남 목포시` resolve to the new codes. An old code or name is an error that names its replacement, not an empty result. | ✅ |
| data.go.kr (apartment trades) | Deals are reported up to 30 days after the contract, and cancellations months later, so the latest month looks quiet (4 trades in Jongno for 2026-10 as of the 25th). | Stated in the tool description so the agent does not read it as a market drop. | ✅ |
| data.go.kr (company info) | The registry keeps **history**: Samsung Electronics has 19 records, returned in no particular order. Older ones name a former CEO, partial ones have no market and a salary of 0. Taking the first record can give years-old data. | Only the latest record is used, and `as_of` shows its date (some companies were last updated in 2020). | ✅ |
| data.go.kr (company info) | Unknown employee counts and salaries are `0`, not empty. | `0` is treated as unknown and omitted, never shown as "0 employees". | ✅ |
| data.go.kr (stock prices) | Querying by name (`itmsNm`) matches the name **on each day**, so a renamed company's history is cut at the rename: `POSCO홀딩스` returns 1,127 of 1,671 days. | Prices are always queried by code; days before a rename carry their old `name`. | ✅ |
| data.go.kr (stock prices) | The exact-code parameter `srtnCd` is **silently ignored** and returns every stock (4.5 million rows). `likeSrtnCd` is a substring match (`00593` also matches 삼성전자우). | A full 6-character code with `likeSrtnCd`, which is then exact. | ✅ |
| data.go.kr (stock prices) | Name search is case- and space-sensitive: `naver` and `삼성 전자` return nothing. | Names are matched against the latest day's listing, ignoring case and spaces; an ambiguous name lists the candidates with codes. | ✅ |
| data.go.kr (stock prices) | On a day without trades (e.g. a trading halt), open, high and low are **0** while the close is the last price, so "low: ₩0" appears. | `no_trades: true`, and open/high/low are omitted. | ✅ |
| data.go.kr (stock prices) | `endBasDt` is exclusive and an invalid date (`2026-10-23` instead of `20261023`) is success with zero results. A preferred share's ISIN does not contain its code (삼성전자우: `KR7005931001`, code `005935`). | `from`/`to` are inclusive ISO dates and validated; ISINs are looked up, not sliced. | ✅ |
| data.go.kr (business status) | A business number written with hyphens (`124-81-00998`, the usual format) is reported as **not registered with the NTS**, even for Samsung Electronics. | Hyphens and spaces are removed before the lookup; anything that is not 10 digits is rejected with the offending value. | ✅ |
| OpenDART | Net income appears twice with identical values (once in the income statement, once at the top of comprehensive income). | Exact duplicates are removed. | ✅ |
| law.go.kr | The "current law" text (`target=law`) **includes amendments that are promulgated but not in effect yet**, every article labeled with the current effective date: the Labor Standards Act shows 제44조의4 "in effect since 2026-10-08", but it takes effect on 2027-01-01, and 제13조 and 제101조 already show text from 2026-12-08. | The text is always read by effective date (`target=eflaw`) for the version in force on `as_of` (default today); later amendments are listed in `upcoming`. An article that is not in force yet is returned marked `not_yet_in_force`, with the date it takes effect. | ✅ |
| law.go.kr | Searching `민법` returns 난민법, 난민법 시행규칙 and 난민법 시행령 **before 민법**: results are sorted by name only. | Exact matches first, then prefixes, then acts before decrees and ordinances. | ✅ |
| law.go.kr (precedents) | A case number with spaces (`2020 다 296604`) or a date range as `2026-01-01~2026-12-31` is **success with zero results**. | Spaces are removed; dates are taken as `YYYY-MM-DD` and validated. | ✅ |

## Errors that look like something else

| Source | What the API does | What korea-data-mcp does | |
|---|---|---|---|
| OpenDART | Errors (bad key, quota exceeded, no data) come back as **HTTP 200** with a `status` field. | Mapped to standard error codes, with the original in `upstream_code`. Error responses are never cached. | ✅ |
| data.go.kr | An unregistered key is **HTTP 403**, which looks like a permission problem. | `UNAUTHENTICATED` with a hint: apply for the API, use the Decoding key, wait for activation. | ✅ |
| data.go.kr | The same error document (`OpenAPI_ServiceResponse`) can also arrive with **HTTP 200**, e.g. for a malformed `numOfRows`. | Detected in any status, in XML or JSON. | ✅ |
| data.go.kr | Success is `resultCode` `00` in some services and `000` in others; "no data" is `03` or `003`. | All normalized: success and "no data" both return a list (possibly empty). | ✅ |
| data.go.kr (business status) | "Not registered" is not an error or a status: the message `국세청에 등록되지 않은 사업자등록번호입니다.` is put in the **tax type** field. | `registered: false`, with no tax type. | ✅ |
| data.go.kr (business status) | The National Tax Service API has its own error format and unusual HTTP statuses: an empty request is 411 (Length Required), over 100 numbers is 413, a bad key is 401 with `{"code":-4,...}`. The gateway also returns an occasional 504. | Mapped to the standard errors (the key error gets the same hint as the other APIs); 504 is retried. | ✅ |
| data.go.kr | A newly approved key returns `SERVICE_KEY_IS_NOT_REGISTERED_ERROR` for up to a few hours. | The hint says so, so the agent tells the user to wait instead of guessing. | ✅ |
| data.go.kr | A superseded endpoint (the stock price API before `_V2`) also answers **"key not registered" (30)**, not "no such service" (12), so an approved key looks broken. | Every tool calls the endpoint shown on the API's page today. | ✅ |
| law.go.kr | Errors are **HTTP 200** documents of different shapes: `<Response>` for a bad OC, `<Law>` with plain text for an unknown law. | `UNAUTHENTICATED` with a hint on where to find the OC; an unknown law is `NOT_FOUND`. | ✅ |
| law.go.kr | Asking for a law version by a date (`efYd`) that is not exactly one of its effective dates returns an **empty document** `{}`, and a missing article returns the law without articles. | The version is chosen from the law's history, and a missing article is `NOT_FOUND` with a hint when an upcoming amendment may add it. | ✅ |

## Formats that differ per API

| Source | What the API does | What korea-data-mcp does | |
|---|---|---|---|
| data.go.kr | In JSON, one result is an **object**, several are an **array**, and none is `""` in one service and `{"item":[]}` in another. Numbers are strings in some fields and numbers in others (`"locdate": 20261225`). | korea-data-mcp requests XML, which has none of these problems, and outputs typed JSON. | ✅ |
| data.go.kr | Some services are XML only (apartment trades), others JSON or XML, and the National Tax Service API is on a different host (`api.odcloud.kr`) with POST requests. | One provider, one output format. | ✅ |
| All | Dates come as `20251114`, `2024.12.31 현재`, `25.11.28`, `75/06/11`. | ISO 8601 (`2025-11-14`). | ✅ |
| All | Amounts come as `"1,234"`, `"-"` (no value), or `"(1,234)"` (negative). | Integers, or `null` when there is no value (never a misleading `0`). | ✅ |
| data.go.kr | Empty fields are `""` or a single space `" "`, and addresses have double spaces. Some dates are not dates at all (a founding date of `99`). | Whitespace is cleaned; empty and invalid values are omitted. | ✅ |
| All | Pagination is `pageNo`/`numOfRows`, `page_no`/`page_count`, or none. | Always `page`/`page_size` in, `page_info` out. | ✅ |
| law.go.kr (precedents) | National Tax Service records have no court name; the court is inside the case number (`서울행정법원-2025-구합-53785`). Text uses `<br/>`, some fields are the string `"null"`, and dates are `2025.10.16` in search and `20251016` in details. | `court` is filled from the case number, which becomes `2025구합53785`; text has plain line breaks; dates are ISO. | ✅ |

## Missing features

| Source | What the API does | What korea-data-mcp does | |
|---|---|---|---|
| OpenDART | **No company search.** Every call needs an 8-digit `corp_code`, available only in a 3.6 MB zip of ~120,000 companies. | Downloads and indexes the list (refreshed daily). Search by Korean or English name, ignoring spacing and `(주)`; listed companies first. | ✅ |
| OpenDART | Stock codes (`005930`) are not accepted. | Any `corp_code` input also accepts a stock code. | ✅ |
| data.go.kr (company info) | Company name search is a substring match with no ranking: `삼성전자` returns `삼성전자잠실판매` first, as one of 108 records for 37 companies. `삼성 전자` or `(주)삼성전자` finds nothing useful. | Spacing and corporate forms are stripped before searching, records are merged per company, and the exact match comes first, then listed companies. | ✅ |
| data.go.kr (company info) | A broad name like `삼성` matches 3,158 records (5.8 MB). | Rejected with a hint to use a more specific name, instead of returning an arbitrary slice. | ✅ |
| law.go.kr | Official abbreviations are not searched: `중대재해처벌법` finds nothing, although it is the official short name of 「중대재해 처벌 등에 관한 법률」. | Names and abbreviations are matched here, ignoring spaces. | ✅ |
| law.go.kr | Every link in a response contains the caller's **OC** (`lawService.do?OC=…`). | Links are never passed on; tools return public law.go.kr URLs. | ✅ |
| law.go.kr (precedents) | About **90% of recent rulings** in search results come from the National Tax Service's system (국세법령정보시스템), and fetching any of them returns "no such precedent": their text is only on the website. | Searches default to court records, which all have text. Tax records are opt-in, marked `text_available: false` with a public URL. | ✅ |

## Connection problems

| Source | What the API does | What korea-data-mcp does | |
|---|---|---|---|
| OpenDART | The server only offers TLS 1.2 RSA key exchange ciphers, which Go has disabled by default since 1.22. Go clients fail with `tls: handshake failure` while curl works. | Enables those ciphers for OpenDART only. | ✅ |
| data.go.kr | The portal issues an "Encoding" and a "Decoding" key. The Encoding key is already URL-encoded, so most HTTP clients encode it twice and authentication fails. | Documented: use the Decoding key. The error hint says the same. | ✅ |

## Found another one?

Please open an issue with the API, the request (without your key), and the response. Each quirk becomes a test with the real response.
