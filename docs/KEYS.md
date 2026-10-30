# Getting API keys

**English** | [한국어](KEYS.ko.md)

korea-data-mcp needs your own free key for each data source. Get **one** to start; each key unlocks its own tools, and the others simply stay hidden.

| Key | Unlocks | Time to get it | Effort |
|---|---|---|---|
| [OpenDART](#opendart-start-here) | Companies, disclosures, financial statements (4 tools) | Minutes | Easy |
| [data.go.kr](#datagokr) | Holidays, corporation info, business status, apartment trades, stock prices (6 tools) | Minutes to a few hours | Medium: one application per API |
| [law.go.kr](#lawgokr) | Statutes and court rulings (4 tools) | Minutes | Easy |

> **Outside Korea?** Signing up for these portals usually requires Korean identity verification (a Korean mobile number). If you can't, [join the waitlist for the hosted version](https://github.com/YoungsoonLee/korea-data-mcp/issues/new?template=hosted-access.yml), which needs no keys of your own.

The sites are in Korean; the Korean menu names are given below so you can find them.

## OpenDART (start here)

1. Sign up at [opendart.fss.or.kr](https://opendart.fss.or.kr).
2. Open **인증키 신청** (request a key) and submit the form. The key (40 characters) is issued right away.
3. Set it as `OPENDART_API_KEY`.

Then ask your agent "What was Samsung Electronics' revenue in the first half of 2025?".

The key allows about 20,000 calls a day, far more than an agent uses.

## data.go.kr

One key for the whole portal, but **each API must be applied for separately** (활용신청). Applications for these APIs are approved automatically.

1. Sign up at [data.go.kr](https://www.data.go.kr).
2. Open each API page below and click **활용신청** (apply for use). Fill in the purpose in a sentence (e.g. "AI agent data lookup") and submit. Apply only for the ones you want:

   | API | Tools |
   |---|---|
   | [한국천문연구원_특일 정보](https://www.data.go.kr/data/15012690/openapi.do) | `datagokr_list_holidays` |
   | [금융위원회_기업기본정보](https://www.data.go.kr/data/15043184/openapi.do) | `datagokr_search_corporations`, `datagokr_get_corporation` |
   | [국세청_사업자등록정보 진위확인 및 상태조회](https://www.data.go.kr/data/15081808/openapi.do) | `datagokr_check_business_status` |
   | [국토교통부_아파트 매매 실거래가 자료](https://www.data.go.kr/data/15126469/openapi.do) | `datagokr_list_apartment_trades` |
   | [금융위원회_주식시세정보](https://www.data.go.kr/data/15094808/openapi.do) | `datagokr_get_stock_prices` |

3. On **마이페이지** (My Page), open the approved API to see the key. Two keys are shown, **Encoding** and **Decoding**: copy the **Decoding** key into `DATAGOKR_SERVICE_KEY`.
4. A newly approved API can take from a few minutes to a few hours to start working. Until then it answers as if the key were wrong.

A development account allows 10,000 calls a day for each API.

## law.go.kr

1. Sign up at [open.law.go.kr](https://open.law.go.kr).
2. Apply for the Open API (**OPEN API 신청**). Select the statute (법령) and precedent (판례) APIs.
3. The application is approved right away.
4. On **마이페이지 → API인증키관리**, set your **OC**: a value you choose (up to 50 characters). Set it as `LAWGOKR_OC`.

Things to know:

- A wrong OC gives the error "사용자 정보 검증에 실패하였습니다". korea-data-mcp reports it as `UNAUTHENTICATED` with a hint on where to find the OC.
- law.go.kr puts the OC in every link it returns, so choose a value of its own; do not reuse another key or password. (korea-data-mcp never passes those links on.)

## Check your setup

Ask your agent "Which korea-data-mcp providers are enabled?". It calls `hub_list_providers`, which lists the enabled sources and the keys that are missing.

| Error | Likely cause |
|---|---|
| `UNAUTHENTICATED` from data.go.kr | The Encoding key instead of the Decoding key; the API not applied for; or approved too recently, wait a few hours |
| `PERMISSION_DENIED` from data.go.kr | That API is not applied for (활용신청) |
| `UNAUTHENTICATED` from law.go.kr | The OC does not match the one under 마이페이지 → API인증키관리 |
| `UNAUTHENTICATED` from OpenDART | The key is mistyped or not active |
| `RATE_LIMITED` | Daily limit reached; it resets the next day |
