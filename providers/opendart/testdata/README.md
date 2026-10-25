# OpenDART test fixtures

These files are **real OpenDART responses** captured on 2026-10-25 (Samsung Electronics, `00126380`). They contain no API key. Some are trimmed to keep them small.

| File | Endpoint | Notes |
|---|---|---|
| `CORPCODE.xml` | `corpCode.xml` (served zipped by the test) | 5 entries picked from the full list (~120,000 entries) |
| `company.json` | `company.json` | As captured |
| `list.json` | `list.json` | First 2 of 5 items; `total_count` left as returned |
| `fnlttSinglAcnt.json` | `fnlttSinglAcnt.json` | 2024 annual report, as captured |
| `fnlttSinglAcnt_half.json` | `fnlttSinglAcnt.json` | 2025 half-year report, consolidated 자산총계 and 매출액 only |

When re-capturing, never save the request URL: it contains `crtfc_key`.
