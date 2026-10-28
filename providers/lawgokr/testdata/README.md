# law.go.kr test fixtures

These files are **real law.go.kr responses** (XML) captured on 2026-10-28. Every link in a response contains the caller's OC; it was replaced with `OC_REDACTED` before saving. When re-capturing, mask the OC the same way and never save the request URL.

| File | Request | Shows |
|---|---|---|
| `search_minbeop.xml` | `lawSearch` eflaw, nw=3, query 민법 | 6 laws sorted by name: 난민법 and its decrees before 민법 |
| `search_none.xml` | Same, query 중대재해처벌법 | An official abbreviation finds nothing |
| `search_prefix.xml` | Same, query 중대 | 11 laws, including 중대재해 처벌 등에 관한 법률 with its abbreviation |
| `search_text.xml` | Same, search=2 (text), query 최저임금, 2 rows | 81 laws mention 최저임금 |
| `versions_labor.xml` | `lawSearch` eflaw, LID 001872 (근로기준법) | 54 versions: current, past, and five upcoming (two on 2027-01-01) |
| `versions_serious.xml` | Same, LID 013993 (중대재해처벌법) | One version |
| `law_labor_50.xml` | `lawService` eflaw, MST 285279, efYd 20261008, JO 005000 | 제50조 with its chapter heading |
| `law_labor_35.xml` | Same, JO 003500 | A deleted article (`제35조 삭제 <2019.1.15>`) |
| `law_labor_44_4_missing.xml` | Same, JO 004404 | 제44조의4 is not in force yet: the law without articles |
| `law_labor_44_4_2027.xml` | Same, efYd 20270101, JO 004404 | 제44조의4 from 2027-01-01 |
| `law_serious_full.xml` | `lawService` eflaw, MST 228817, efYd 20220127 | A whole law: 16 articles, 4 chapters, 호 and 목 |
| `error_no_law.xml` | `lawService` with MST 1 | `<Law>` with plain text, HTTP 200 |
| `error_bad_oc.xml` | Any call with an unregistered OC | `<Response>` "사용자 정보 검증에 실패하였습니다", HTTP 200 |
