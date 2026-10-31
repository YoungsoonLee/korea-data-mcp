# OpenDART test fixtures

These files are **real OpenDART responses** captured on 2026-10-25 and 2026-10-30, mostly for Samsung Electronics (`00126380`). They contain no API key. Some are trimmed to keep them small.

| File | Endpoint | Notes |
|---|---|---|
| `CORPCODE.xml` | `corpCode.xml` (served zipped by the test) | 5 entries picked from the full list (~120,000 entries) |
| `company.json` | `company.json` | As captured |
| `list.json` | `list.json` | First 2 of 5 items; `total_count` left as returned |
| `fnlttSinglAcnt.json` | `fnlttSinglAcnt.json` | 2024 annual report, as captured |
| `fnlttSinglAcnt_half.json` | `fnlttSinglAcnt.json` | 2025 half-year report, consolidated 자산총계 and 매출액 only |
| `alotMatter.json` | `alotMatter.json` | 2025 half-year report: year-to-date next to full prior years |
| `alotMatter_2015.json` | `alotMatter.json` | 2015 annual report, before the 50:1 split; EPS label without `(연결)` |
| `hyslrSttus.json` | `hyslrSttus.json` | 2024 annual report, as captured |
| `hyslrSttus_tym.json` | `hyslrSttus.json` | TYM, 2024: `김   식` with layout spaces, a `합계` row |
| `hyslrSttus_ditto.json` | `hyslrSttus.json` | 삼환기업, 2017: `"` (ditto) rows for preferred shares |

When re-capturing, never save the request URL: it contains `crtfc_key`.
