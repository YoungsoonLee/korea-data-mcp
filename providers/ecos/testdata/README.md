# ECOS test fixtures

These files are **real ECOS responses** captured on 2026-10-31. Most were fetched with ECOS's public `sample` key, which returns at most 10 rows; `unemployment_annual.json` was fetched with a personal key. They contain no API key.

| File | Service and arguments | Notes |
|---|---|---|
| `base_rate_daily.json` | `StatisticSearch` 722Y001 D 20241008–20241015 0101000 | The 2024-10-11 cut from 3.5% to 3.25%; one row per calendar day |
| `usd_krw_monthly_mixed.json` | `StatisticSearch` 731Y004 M 202501–202504 0000001 | Only the first item given: monthly average and month-end interleaved |
| `cpi_annual.json` | `StatisticSearch` 901Y009 A 2016–2025 0 | Index, 2020=100 |
| `gdp_annual.json` | `StatisticSearch` 200Y101 A 2016–2025 10101 | Unit 십억원 |
| `unemployment_annual.json` | `StatisticSearch` 901Y027 A 2019–2025 I61BC | 원계열 values and 계절조정 rows of nulls |
| `error_no_data.json` | `StatisticSearch` 722Y001 M 203001–203002 | `INFO-200` |
| `error_bad_key.json` | `StatisticSearch` with an invalid key | `INFO-100` |
| `items_731Y004.json` | `StatisticItemList` 731Y004 | First 10 items |
| `tables_kr.json`, `tables_en.json` | `StatisticTableList` kr, en | First 100 of 844 entries, ten pages joined |

When re-capturing, never save the request URL: it contains the key.
