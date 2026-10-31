# data.go.kr test fixtures

These files are **real data.go.kr responses** captured between 2026-10-25 and 2026-10-30. They contain no service key.

| File | Request | Shows |
|---|---|---|
| `holidays_2026.xml` | 특일 정보 `getRestDeInfo`, all of 2026 | 22 public holidays, including substitute holidays and an election day |
| `solar_terms_2026.xml` | 특일 정보 `get24DivisionsInfo`, all of 2026 | 24 solar terms, `isHoliday` N |
| `corp_history_samsung.xml` | 기업기본정보 `getCorpOutline_V2`, crno 1301110006246 | 19 history records for one company, unordered, some partial |
| `corp_search_samsung_electronics.xml` | Same, corpNm 삼성전자, 1,000 rows | 108 records for 37 companies; 삼성전자잠실판매 first |
| `nts_status_mixed.json` | 국세청 상태조회, 1248100998 and 1234567890 | One active business, one not registered (message in `tax_type`) |
| `nts_status_hyphenated.json` | Same, `124-81-00998` | A real number reported as not registered because of the hyphens |
| `nts_error_bad_key.json` | Same, unregistered key | HTTP 401, `{"code":-4,...}` |
| `nts_error_too_large.json` | Same, 101 numbers | HTTP 413, `TOO_LARGE_REQUEST` |
| `holidays_many.xml` | 특일 정보 `getRestDeInfo`, 2026-10 | Several items, result code `00` |
| `holidays_single.xml` | Same, 2026-12 | One item (an object, not an array, in JSON) |
| `holidays_none.xml` | Same, 2026-04 | No items (`"items": ""` in JSON) |
| `apt_trades_gangnam_202509.xml` | 아파트 매매 실거래가, 강남구 (11680), 2025-09, all rows | 233 trades, 12 cancelled (`cdealType` O) |
| `apt_rents_jongno_202509.xml` | 아파트 전월세 실거래가, 종로구 (11110), 2025-09, all rows | 217 contracts: 64 jeonse, 153 monthly; 103 new, 97 renewals, 17 unknown |
| `apt_trades.xml` | 아파트 매매 실거래가, 종로구 (11110), 2025-09, 3 rows | Result code `000`, prices in 10,000 KRW |
| `apt_trades_none.xml` | Same, 2030-01 | No items; an invalid `DEAL_YMD` returns exactly the same |
| `stock_head.xml` | 주식시세 `getStockPriceInfo_V2`, no filters, 1 row (2026-10-26) | The newest day comes first |
| `stock_listed_20261026.xml` | Same, basDt 20261026, all 2,871 rows, **trimmed to 12** | Names and codes for name matching, including a code with a letter (`0001A0`) and 삼성전자우's ISIN |
| `stock_samsung_202609.xml` | Same, likeSrtnCd 005930, 2026-09-01 to 2026-10-01 (exclusive) | 20 trading days, newest first |
| `stock_halted.xml` | Same, likeSrtnCd 000300, 2026-10-19 to 2026-10-26 | A halted stock: open/high/low 0, volume 0 |
| `stock_posco_rename.xml` | Same, likeSrtnCd 005490, 2022-03-10 to 2022-03-21 | The rename from POSCO to POSCO홀딩스 on 2022-03-17 |
| `error_key_not_registered.xml` | Any API with an unregistered key, XML | HTTP 403, `returnReasonCode` 30 |
| `error_key_not_registered.json` | Same, JSON | HTTP 403, `returnReasonCode` 30 |
| `error_http_200.json` | 기업기본정보 with `numOfRows=abc` | Error document with HTTP 200, `returnReasonCode` 04 |

When re-capturing, never save the request URL: it contains `serviceKey`.
