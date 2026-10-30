# Mini evaluation results

Ten questions from [questions.yaml](questions.yaml) (8 with a known API trap, 2 controls), asked to an agent with the raw APIs and to an agent with korea-data-mcp. Answers were checked against the raw APIs beforehand, independently of korea-data-mcp. Run on 2026-10-30.

**Short version:** a capable model with the raw APIs answered 9 of 10 correctly, but only after running into the traps and working around them. With korea-data-mcp it answered 10 of 10, with one call per question.

## Setup

| | Raw APIs | korea-data-mcp |
|---|---|---|
| Tools | A shell wrapper that adds the API key, plus a corp-code lookup for OpenDART | The 15 korea-data-mcp tools |
| Documentation | A list of endpoints and parameter names (no response fields, no warnings) | Tool descriptions only |
| Agents | A fresh agent per question | A fresh agent per question |

The raw-API side was given a few things a typical pass-through integration would not have: the corp-code lookup, which avoids a 3.6 MB zip download, and the legal-dong code API. The questions do not mention any trap.

## Run 2: Claude Sonnet, one agent per question

| # | Question | Correct | Raw APIs | korea-data-mcp |
|---|---|---|---|---|
| q01 | 삼성전자 2025 H1 revenue | 153.7T | ✅ (1 call) | ✅ (1) |
| q02 | SK하이닉스 2025 Q1–Q3 operating income | 28.0T | ✅ (2) | ✅ (1) |
| q03 | 삼성전자 2024 revenue (control) | 300.9T | ✅ (2) | ✅ (1) |
| q04 | 강남구 apartment sales, 2025-09 | 221 | ✅ (1) | ✅ (1) |
| q05 | 화성시 apartment sales, 2025-06 | 1,428 | ✅ (~8, after the city code returned 0) | ✅ (1) |
| q06 | 삼성전자우 close, 2026-10-23 | 167,900 | ✅ (1) | ✅ (1) |
| q07 | Business status of 124-81-00998 | 계속사업자 | ✅ (1) | ✅ (1) |
| q08 | 근로기준법 제44조의4 in force on 2026-10-29? | No, from 2027-01-01 | ✅ (2) | ✅ (1) |
| q09 | 삼성전자 close, 2026-10-23 (control) | 215,500 | ✅ (2, the code filter was ignored) | ✅ (1) |
| q10 | 광주 북구 apartment sales, 2025-03 | 492 | ❌ UNKNOWN (~14) | ✅ (1) |
| | **Correct** | | **9 / 10** | **10 / 10** |
| | **API or tool calls** | | **~34** | **10** |

**q10 on the raw APIs:** after the old code `29170` returned 0, the agent found the merged region in the legal-dong code API but took the wrong district code (`12230` instead of `12300`, which is 북구). It concluded that the API had no data. It answered "unknown" rather than a wrong number.

## Run 1: Claude Opus, one agent for all ten questions

The same questions, with hints in the apartment questions ("leave out cancelled deals") and a reference that named response fields such as `thstrm_add_amount` and `cdealType`. Both sides answered 10 of 10. The raw-API agent made about 35 calls and took 284 s, against 11 calls and 33 s with korea-data-mcp.

## What this shows

- **Strong models can work around the traps** when they are careful and have time, so the difference is not always a wrong answer. Run 1 is the evidence.
- **The traps still cost something.** The raw-API agents hit them along the way: an old region code that returns 0, a code filter (`srtnCd`) that is ignored, a quarter-only amount next to a year-to-date label. Each one meant extra calls, and in q10 the workaround failed.
- **korea-data-mcp answered every question with one call** and never needed a workaround.

## Limits

- Ten questions, one run each; this is a small sample.
- The questions were chosen around known traps, with two controls.
- Trade counts can change when late reports or cancellations are filed; the answers were checked on 2026-10-30.
