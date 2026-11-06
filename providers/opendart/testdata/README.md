# OpenDART test fixtures

These files are **real OpenDART responses** captured on 2026-10-25, 2026-10-30, 2026-11-03 and 2026-11-05, mostly for Samsung Electronics (`00126380`). They contain no API key. Some are trimmed to keep them small.

| File | Endpoint | Notes |
|---|---|---|
| `CORPCODE.xml` | `corpCode.xml` (served zipped by the test) | 7 entries picked from the full list (~120,000 entries) |
| `company.json` | `company.json` | As captured |
| `list.json` | `list.json` | First 2 of 5 items; `total_count` left as returned |
| `fnlttSinglAcnt.json` | `fnlttSinglAcnt.json` | 2024 annual report, as captured |
| `fnlttSinglAcnt_half.json` | `fnlttSinglAcnt.json` | 2025 half-year report, consolidated 자산총계 and 매출액 only |
| `fnlttSinglAcnt_q3.json` | `fnlttSinglAcnt.json` | 2025 third-quarter report (captured 2026-11-01), consolidated 자산총계 and 매출액 only |
| `alotMatter.json` | `alotMatter.json` | 2025 half-year report: year-to-date next to full prior years |
| `alotMatter_2015.json` | `alotMatter.json` | 2015 annual report, before the 50:1 split; EPS label without `(연결)` |
| `hyslrSttus.json` | `hyslrSttus.json` | 2024 annual report, as captured |
| `hyslrSttus_tym.json` | `hyslrSttus.json` | TYM, 2024: `김   식` with layout spaces, a `합계` row |
| `hyslrSttus_ditto.json` | `hyslrSttus.json` | 삼환기업, 2017: `"` (ditto) rows for preferred shares |
| `tsstkAqDecsn.json` | `tsstkAqDecsn.json` | Samsung buybacks since 2024-11, unsorted; common and preferred amounts |
| `piicDecsn.json` | `piicDecsn.json` | 광진실업 rights offerings, some corrected months after the first filing |
| `cvbdIsDecsn.json` | `cvbdIsDecsn.json` | 파이온엑스 convertible bonds; a ₩21 reset floor |
| `tsstkAqTrctrCnsDecsn.json` | `tsstkAqTrctrCnsDecsn.json` | LG Chem's ₩300 billion buyback trust |
| `tsstkAqTrctrCcDecsn.json` | `tsstkAqTrctrCcDecsn.json` | 우성, a buyback trust ended at expiry |
| `tsstkDpDecsn.json` | `tsstkDpDecsn.json` | Kia treasury stock disposals to employees |
| `bdwtIsDecsn.json` | `bdwtIsDecsn.json` | 성호전자 bond with warrants |
| `exbdIsDecsn.json` | `exbdIsDecsn.json` | KG에코솔루션 exchangeable bonds, one for its own treasury shares |
| `crDecsn.json` | `crDecsn.json` | 프리티 capital reductions |
| `fricDecsn.json` | `fricDecsn.json` | 케이아이이 bonus issue |
| `cmpMgDecsn.json` | `cmpMgDecsn.json` | 샘표 merger into 샘표식품 |
| `cmpDvDecsn.json` | `cmpDvDecsn.json` | Hyundai Mobis spin-off of its lamp business (물적분할) |
| `majorstock.json` | `majorstock.json` | SK hynix 5% reports: a holder below 5%, a 0% report on a change of representative reporter |
| `list_D001.json` | `list.json` | SK hynix 5% filings since 2015 (`pblntf_detail_ty=D001`), including the National Pension Service's 2024-09 report |
| `otrCprInvstmntSttus.json` | `otrCprInvstmntSttus.json` | SK Square, 2025: 에스케이하이닉스㈜ and SK플래닛㈜, ten stakes sold during the year, preferred-share rows |
| `otrCprInvstmntSttus_attached.json` | `otrCprInvstmntSttus.json` | Samsung Electronics, 2020: only a `-` row and a `합계` row (the table is an attachment) |
| `otrCprInvstmntSttus_usd.json` | `otrCprInvstmntSttus.json` | 소마젠, 2025: USD amounts presented as KRW |
| `otrCprInvstmntSttus_units.json` | `otrCprInvstmntSttus.json` | 유성기업, 2025: 4 of 12 investees, with 백만엔 and 천RMB notes in the purpose cell, and the `합계` row |
| `elestock.json` | `elestock.json` | 19 of Samsung's 3,368 insider reports: 이재용, executives filed as 10%이상주주, positions with trailing spaces |

When re-capturing, never save the request URL: it contains `crtfc_key`.
