# What mcp-hub handles for you

**English** | [한국어](QUIRKS.ko.md)

Korean public APIs work, but each one has its own surprises. An agent that calls them directly has to discover every one of these by failing, and some of them do not fail at all: they return a plausible but wrong answer.

Everything below was **observed on the live APIs** (last checked 2026-10-25), and the real responses are kept in each provider's `testdata/` so the handling is tested.

Status: ✅ handled · 🚧 handled when the tool that needs it ships

## Wrong answers without an error

These are the most dangerous ones: nothing fails, the agent simply gets the wrong number.

| Source | What the API does | What mcp-hub does | |
|---|---|---|---|
| OpenDART | In half-year and quarterly reports, income statement items are labeled with the year-to-date range (`2025.01.01 ~ 2025.06.30`) but `thstrm_amount` holds **only the latest quarter**. The year-to-date figure is in a separate `thstrm_add_amount`. | `current_amount` always matches `current_period`; the quarter is in `current_quarter_amount`. | ✅ |
| data.go.kr | An invalid parameter is not an error. `DEAL_YMD=2025` (instead of `202509`) or `solMonth=13` returns **success with zero results**, which looks like "no data". | Every tool validates its inputs and returns `INVALID_ARGUMENT` with the expected format. | ✅ holidays / 🚧 others |
| data.go.kr (holidays) | Years without data (before 2004, or more than two years ahead) also return **success with zero results**, so "no holidays in 2030" looks like a fact. | The year range is checked, and the error message states it. | ✅ |
| data.go.kr (holidays) | The "national holidays" (국경일) operation returns **all 22 public holidays** of 2026, the same as the "public holidays" operation, not the five national holidays. | That operation is not exposed. Public holidays come from the public holidays operation only. | ✅ |
| data.go.kr (apartment trades) | Prices are strings in **units of 10,000 KRW** (`"84,500"` is ₩845,000,000). | Amounts are plain KRW integers. | 🚧 |
| data.go.kr (company info) | The registry keeps **history**: Samsung Electronics has 19 records, returned in no particular order. Older ones name a former CEO, partial ones have no market and a salary of 0. Taking the first record can give years-old data. | Only the latest record is used, and `as_of` shows its date (some companies were last updated in 2020). | ✅ |
| data.go.kr (company info) | Unknown employee counts and salaries are `0`, not empty. | `0` is treated as unknown and omitted, never shown as "0 employees". | ✅ |
| OpenDART | Net income appears twice with identical values (once in the income statement, once at the top of comprehensive income). | Exact duplicates are removed. | ✅ |

## Errors that look like something else

| Source | What the API does | What mcp-hub does | |
|---|---|---|---|
| OpenDART | Errors (bad key, quota exceeded, no data) come back as **HTTP 200** with a `status` field. | Mapped to standard error codes, with the original in `upstream_code`. Error responses are never cached. | ✅ |
| data.go.kr | An unregistered key is **HTTP 403**, which looks like a permission problem. | `UNAUTHENTICATED` with a hint: apply for the API, use the Decoding key, wait for activation. | ✅ |
| data.go.kr | The same error document (`OpenAPI_ServiceResponse`) can also arrive with **HTTP 200**, e.g. for a malformed `numOfRows`. | Detected in any status, in XML or JSON. | ✅ |
| data.go.kr | Success is `resultCode` `00` in some services and `000` in others; "no data" is `03` or `003`. | All normalized: success and "no data" both return a list (possibly empty). | ✅ |
| data.go.kr | A newly approved key returns `SERVICE_KEY_IS_NOT_REGISTERED_ERROR` for up to a few hours. | The hint says so, so the agent tells the user to wait instead of guessing. | ✅ |

## Formats that differ per API

| Source | What the API does | What mcp-hub does | |
|---|---|---|---|
| data.go.kr | In JSON, one result is an **object**, several are an **array**, and none is `""` in one service and `{"item":[]}` in another. Numbers are strings in some fields and numbers in others (`"locdate": 20261225`). | mcp-hub requests XML, which has none of these problems, and outputs typed JSON. | ✅ |
| data.go.kr | Some services are XML only (apartment trades), others JSON or XML, and the National Tax Service API is on a different host (`api.odcloud.kr`) with POST requests. | One provider, one output format. | ✅ / 🚧 |
| All | Dates come as `20251114`, `2024.12.31 현재`, `25.11.28`, `75/06/11`. | ISO 8601 (`2025-11-14`). | ✅ OpenDART, company info / 🚧 others |
| All | Amounts come as `"1,234"`, `"-"` (no value), or `"(1,234)"` (negative). | Integers, or `null` when there is no value (never a misleading `0`). | ✅ |
| data.go.kr | Empty fields are `""` or a single space `" "`, and addresses have double spaces. Some dates are not dates at all (a founding date of `99`). | Whitespace is cleaned; empty and invalid values are omitted. | ✅ |
| All | Pagination is `pageNo`/`numOfRows`, `page_no`/`page_count`, or none. | Always `page`/`page_size` in, `page_info` out. | ✅ |

## Missing features

| Source | What the API does | What mcp-hub does | |
|---|---|---|---|
| OpenDART | **No company search.** Every call needs an 8-digit `corp_code`, available only in a 3.6 MB zip of ~120,000 companies. | Downloads and indexes the list (refreshed daily). Search by Korean or English name, ignoring spacing and `(주)`; listed companies first. | ✅ |
| OpenDART | Stock codes (`005930`) are not accepted. | Any `corp_code` input also accepts a stock code. | ✅ |
| data.go.kr (company info) | Company name search is a substring match with no ranking: `삼성전자` returns `삼성전자잠실판매` first, as one of 108 records for 37 companies. `삼성 전자` or `(주)삼성전자` finds nothing useful. | Spacing and corporate forms are stripped before searching, records are merged per company, and the exact match comes first, then listed companies. | ✅ |
| data.go.kr (company info) | A broad name like `삼성` matches 3,158 records (5.8 MB). | Rejected with a hint to use a more specific name, instead of returning an arbitrary slice. | ✅ |

## Connection problems

| Source | What the API does | What mcp-hub does | |
|---|---|---|---|
| OpenDART | The server only offers TLS 1.2 RSA key exchange ciphers, which Go has disabled by default since 1.22. Go clients fail with `tls: handshake failure` while curl works. | Enables those ciphers for OpenDART only. | ✅ |
| data.go.kr | The portal issues an "Encoding" and a "Decoding" key. The Encoding key is already URL-encoded, so most HTTP clients encode it twice and authentication fails. | Documented: use the Decoding key. The error hint says the same. | ✅ |

## Found another one?

Please open an issue with the API, the request (without your key), and the response. Each quirk becomes a test with the real response.
