# data.go.kr test fixtures

These files are **real data.go.kr responses** captured on 2026-10-25. They contain no service key.

| File | Request | Shows |
|---|---|---|
| `holidays_2026.xml` | 특일 정보 `getRestDeInfo`, all of 2026 | 22 public holidays, including substitute holidays and an election day |
| `solar_terms_2026.xml` | 특일 정보 `get24DivisionsInfo`, all of 2026 | 24 solar terms, `isHoliday` N |
| `corp_history_samsung.xml` | 기업기본정보 `getCorpOutline_V2`, crno 1301110006246 | 19 history records for one company, unordered, some partial |
| `corp_search_samsung_electronics.xml` | Same, corpNm 삼성전자, 1,000 rows | 108 records for 37 companies; 삼성전자잠실판매 first |
| `holidays_many.xml` | 특일 정보 `getRestDeInfo`, 2026-10 | Several items, result code `00` |
| `holidays_single.xml` | Same, 2026-12 | One item (an object, not an array, in JSON) |
| `holidays_none.xml` | Same, 2026-04 | No items (`"items": ""` in JSON) |
| `apt_trades.xml` | 아파트 매매 실거래가, 종로구 (11110), 2025-09, 3 rows | Result code `000`, prices in 10,000 KRW |
| `apt_trades_none.xml` | Same, 2030-01 | No items; an invalid `DEAL_YMD` returns exactly the same |
| `error_key_not_registered.xml` | Any API with an unregistered key, XML | HTTP 403, `returnReasonCode` 30 |
| `error_key_not_registered.json` | Same, JSON | HTTP 403, `returnReasonCode` 30 |
| `error_http_200.json` | 기업기본정보 with `numOfRows=abc` | Error document with HTTP 200, `returnReasonCode` 04 |

When re-capturing, never save the request URL: it contains `serviceKey`.
