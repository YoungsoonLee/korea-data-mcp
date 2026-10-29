# mcp-hub

[English](README.md) | **한국어**

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26-00ADD8.svg)](go.mod)

AI 에이전트가 한국 공공 데이터를 믿고 쓸 수 있게 해 주는 MCP 서버입니다. 기업 공시와 재무제표, 주가, 아파트 실거래가, 사업자 상태, 공휴일, 법령과 판례를 다룹니다. 무료 공식 데이터를 쓰되, 에이전트가 틀린 답을 내게 만드는 함정은 미리 처리해 둡니다. 한국이 첫 지역이고, 다음은 일본입니다.

## 이런 질문을 할 수 있습니다

mcp-hub를 Claude(또는 다른 MCP 클라이언트)에 연결하고 한국어나 영어로 물어보세요.

- "삼성전자 2025년 상반기 매출이랑 최근 공시 알려줘"
- "2025년 9월 분당구 아파트 매매 중앙값은?"
- "근로기준법 제44조의4 지금 시행 중이야?"
- "부당해고 관련 최근 대법원 판례 찾아서 가장 최근 판결 요지 정리해줘"
- "사업자번호 124-81-00998 아직 영업 중이야?"
- "NAVER 주가 지난달에 어떻게 움직였어?"
- "올해 남은 공휴일 알려줘"

세 번째 질문을 Claude Code에서 한 화면입니다. 공식 "현행 법령" 본문에는 이미 이 조문이 들어 있지만, mcp-hub는 2027-01-01부터 시행된다는 것을 알려줍니다(tool 결과는 일부만 표시).

![Claude Code가 lawgokr_get_law로 근로기준법 제44조의4 시행 여부에 답하는 화면](docs/images/law-not-yet-in-force.png)

| 데이터 소스 | 다루는 데이터 | 키 |
|---|---|---|
| [OpenDART](https://opendart.fss.or.kr) (전자공시) | 약 12만 개 회사의 기업개황, 공시, 재무제표 | `OPENDART_API_KEY` |
| [공공데이터포털](https://www.data.go.kr) | 공휴일, 기업 정보, 사업자 상태, 아파트 실거래가, 주가 | `DATAGOKR_SERVICE_KEY` |
| [국가법령정보](https://open.law.go.kr) | 원하는 날짜 기준으로 시행 중인 법령, 판례 | `LAWGOKR_OC` |

세 키 모두 무료이고, 하나만 있어도 시작할 수 있습니다.

## 왜 mcp-hub인가

한국 공공 API는 무료이고 공식적이지만, 직접 호출하면 함정이 많습니다. 일부는 실패조차 하지 않고 에이전트에게 틀린 답을 줍니다.

**예시:** "삼성전자 2025년 상반기 매출은?"이라는 질문에 OpenDART가 실제로 돌려주는 값입니다.

```json
{ "account_nm": "매출액", "thstrm_dt": "2025.01.01 ~ 2025.06.30",
  "thstrm_amount": "74,566,317,000,000", "thstrm_add_amount": "153,706,820,000,000" }
```

기간이 1월~6월로 되어 있어서 에이전트는 `thstrm_amount`를 읽고 **74.6조 원이라고 답합니다. 틀린 답입니다.** 이 값은 2분기 3개월치이고, 상반기 금액 153.7조 원은 다른 필드에 있습니다. mcp-hub는 이렇게 돌려줍니다.

```json
{ "name": "매출액", "statement_type": "income_statement",
  "current_period": "2025-01-01 ~ 2025-06-30",
  "current_amount": 153706820000000, "current_quarter_amount": 74566317000000 }
```

그래서 에이전트가 올바르게 답하고, 2분기 매출도 따로 짚어 줍니다.

![Claude Code가 opendart_get_financials로 삼성전자 2025년 상반기 매출에 답하는 화면](docs/images/samsung-half-revenue.png)

mcp-hub가 처리하는 함정을 몇 가지 더 들면 이렇습니다.

- **아직 법이 아닌 법.** law.go.kr에 현행 근로기준법을 요청하면 본문에 제44조의4가 2026-10-08부터 시행 중인 것처럼 들어 있습니다. 실제로는 **2027-01-01**부터 시행됩니다. 공식 "현행 법령" 본문에 공포만 되고 아직 시행되지 않은 개정이 섞여 있기 때문입니다. mcp-hub는 요청한 날짜(기본은 오늘)에 시행 중인 본문을 돌려주고, 앞으로 바뀔 내용은 `upcoming`에 알려줍니다.
- **없던 일이 된 거래.** 아파트 실거래가 데이터에는 해제된 계약이 표시만 된 채 남아 있습니다. 2025년 9월 강남구 거래 233건 중 12건이고, 75억 원짜리 거래도 있습니다. 이를 포함해 평균을 내면 틀립니다. mcp-hub는 이를 뺍니다.
- **조용히 멈추는 코드.** 2026년에 광주광역시와 전라남도가 통합되고, 화성시가 4개 구로 나뉘고, 인천의 구가 개편됐습니다. 대부분의 코드표에 아직 남아 있는 옛 시군구 코드로 조회하면 거래가 0건으로 나옵니다. **과거 달을 조회해도 마찬가지입니다.** mcp-hub에서는 에이전트가 `광주 북구`나 `화성시`라고만 하면 됩니다.

이 밖에도 HTTP 200으로 오는 에러, 최신 레코드에 섞인 옛 레코드, 하이픈을 넣으면 실패하는 사업자번호, 사명 변경으로 끊기는 주가 이력, Go에서 실패하는 TLS 핸드셰이크 등을 처리합니다. 모든 항목은 실제 API에서 확인했고, 실제 응답을 쓰는 테스트로 검증합니다. 전체 목록: **[mcp-hub가 대신 처리하는 것들](docs/QUIRKS.ko.md)**

## 빠른 시작

### 1. 설치

Go 1.26 이상이 필요합니다(Go 1.21 이상이면 맞는 툴체인을 자동으로 내려받습니다).

```sh
go install github.com/YoungsoonLee/mcp-hub/cmd/mcp-hub@latest
```

실행 파일은 `$(go env GOPATH)/bin/mcp-hub`에 설치됩니다. 소스에서 직접 빌드해도 됩니다.

```sh
git clone https://github.com/YoungsoonLee/mcp-hub && cd mcp-hub
go build -o mcp-hub ./cmd/mcp-hub
```

미리 빌드한 실행 파일도 곧 제공합니다.

### 2. 키 발급

**OpenDART**부터 시작하세요. [opendart.fss.or.kr](https://opendart.fss.or.kr)에 가입하고 인증키를 신청하면 바로 발급됩니다. 나머지 키는 [API 키](#api-키)에 설명되어 있습니다. 일부 키만 있어도 되고, 키가 없는 tool은 목록에 나오지 않을 뿐입니다.

### 3. 클라이언트에 연결

절대 경로를 쓰세요. 없는 키는 빼면 됩니다.

**Claude Code:**

```sh
claude mcp add mcp-hub \
  -e OPENDART_API_KEY=... \
  -e DATAGOKR_SERVICE_KEY=... \
  -e LAWGOKR_OC=... \
  -- /absolute/path/to/mcp-hub
```

**Claude Desktop:** `~/Library/Application Support/Claude/claude_desktop_config.json`(macOS) 또는 `%APPDATA%\Claude\claude_desktop_config.json`(Windows)을 수정하고 Claude Desktop을 다시 시작합니다.

```json
{
  "mcpServers": {
    "mcp-hub": {
      "command": "/absolute/path/to/mcp-hub",
      "env": {
        "OPENDART_API_KEY": "...",
        "DATAGOKR_SERVICE_KEY": "...",
        "LAWGOKR_OC": "..."
      }
    }
  }
}
```

다른 MCP 클라이언트도 같은 명령과 환경변수를 쓰면 됩니다.

키를 파일 하나로 관리하려면 [`.env.example`](.env.example)을 `.env`로 복사해 값을 채우고, 인자로 `-env-file /absolute/path/to/.env`를 넘기세요. 환경변수로 설정한 값이 파일보다 우선합니다.

### 4. 확인

에이전트에게 "mcp-hub에서 어떤 provider가 활성화돼 있어?"라고 물어보세요. `hub_list_providers`를 호출해서 활성화된 provider와 빠진 키를 알려줍니다. 그다음 [위의 질문](#이런-질문을-할-수-있습니다) 중 하나를 해 보세요.

## API 키

| 환경변수 | 데이터 소스 | 발급 방법 |
|---|---|---|
| `OPENDART_API_KEY` | OpenDART | [opendart.fss.or.kr](https://opendart.fss.or.kr) → 인증키 신청 |
| `DATAGOKR_SERVICE_KEY` | 공공데이터포털 | [data.go.kr](https://www.data.go.kr) → 회원가입 → API별 활용신청([Tool 목록](#tool-목록)의 링크) → 마이페이지 |
| `LAWGOKR_OC` | 국가법령정보 | [open.law.go.kr](https://open.law.go.kr) → OPEN API 신청(이 컴퓨터의 공인 IP 포함) → 마이페이지 → API인증키관리(OC) |

> **공공데이터포털**: 키가 Encoding과 Decoding 두 가지로 발급됩니다. **Decoding 키**를 넣으세요. Encoding 키를 넣으면 인코딩이 두 번 되어 `SERVICE_KEY_IS_NOT_REGISTERED_ERROR`가 납니다. 키 하나로 활용신청한 모든 API를 쓸 수 있습니다.

> **국가법령정보**: 신청할 때 **등록한 공인 IP에서만** 호출할 수 있습니다. IP가 바뀌면 open.law.go.kr에서 고쳐야 합니다. OC가 틀린 경우와 IP가 등록되지 않은 경우 모두 같은 에러("사용자 정보 검증에 실패하였습니다")가 납니다. law.go.kr은 돌려주는 모든 링크에 OC를 넣으므로, 다른 키를 OC로 재사용하지 마세요(mcp-hub는 그 링크를 넘기지 않습니다).

## Tool 목록

**OpenDART** (`OPENDART_API_KEY`)

| Tool | 기능 |
|---|---|
| `opendart_search_companies` | 한글·영문 회사명, 종목코드, 고유번호로 `corp_code` 찾기 |
| `opendart_get_company` | 기업개황: 대표자, 시장, 법인·사업자등록번호, 주소, 결산월 |
| `opendart_search_disclosures` | 회사, 기간, 공시 유형, 시장으로 공시 검색 |
| `opendart_get_financials` | 연도·보고서별 주요계정과 전기 비교 |

`corp_code`를 받는 OpenDART tool은 6자리 종목코드(예: `005930`)도 받습니다.

**공공데이터포털** (`DATAGOKR_SERVICE_KEY`). API마다 data.go.kr에서 따로 활용신청을 해야 합니다. 신청 링크는 아래 표에 있습니다.

| Tool | 기능 | 활용신청할 API |
|---|---|---|
| `datagokr_list_holidays` | 연도·월별 공휴일. 대체공휴일, 설날·추석, 선거일을 포함하고 영문명과 요일도 붙습니다. 기념일, 24절기, 잡절도 조회할 수 있습니다. 2004년부터 2년 뒤까지 | [한국천문연구원_특일 정보](https://www.data.go.kr/data/15012690/openapi.do) |
| `datagokr_search_corporations` | 상장 여부와 관계없이 회사명으로 법인 검색. 법인등록번호(`crno`)를 돌려주고, DART 공시 대상이면 opendart tool에 쓸 `dart_corp_code`도 돌려줍니다 | [금융위원회_기업기본정보](https://www.data.go.kr/data/15043184/openapi.do) |
| `datagokr_get_corporation` | 최신 기업 개요: 대표이사, 시장, 등록번호, 설립일·상장일, 주소, 업종, 직원 수, 평균 근속연수·급여, 감사인과 감사의견 | 위와 같음 |
| `datagokr_check_business_status` | 사업자등록번호 최대 100개를 국세청에 조회합니다. 등록 여부, 계속·휴업·폐업(폐업일 포함), 과세유형을 알려줍니다. 하이픈을 넣어도 됩니다 | [국세청_사업자등록정보 진위확인 및 상태조회](https://www.data.go.kr/data/15081808/openapi.do) |
| `datagokr_list_apartment_trades` | 시군구·월별 아파트 매매 실거래를 최신순으로 보여주고, 중앙값·최저·최고 가격 요약을 함께 줍니다. 금액은 원 단위이고, 해제된 계약은 기본으로 제외합니다. 아파트명과 법정동으로 거를 수 있습니다. 지역은 이름(`강남구`, `서울 중구`, `분당구`, `광주 북구`)이나 5자리 코드로 받습니다. `수원시`, `화성시`처럼 구가 있는 시를 넣으면 소속 구 전체를 조회합니다 | [국토교통부_아파트 매매 실거래가 자료](https://www.data.go.kr/data/15126469/openapi.do) |
| `datagokr_get_stock_prices` | 코스피·코스닥·코넥스 종목의 일별 시세: 종가, 등락, 시가·고가·저가, 거래량, 거래대금, 시가총액. 종목은 이름(`삼성전자`, `naver`), 6자리 코드, ISIN으로 받고, 사명이 바뀌어도 코드 기준으로 전체 이력을 줍니다. 2020년부터 | [금융위원회_주식시세정보](https://www.data.go.kr/data/15094808/openapi.do) |

**국가법령정보** (`LAWGOKR_OC`)

| Tool | 기능 |
|---|---|
| `lawgokr_search_laws` | 현행 법령(법률, 시행령, 시행규칙 등)을 이름이나 공식 약칭(`민법`, `중대재해처벌법`)으로 검색하고, `in_text`로 조문 본문을 검색합니다. 정확히 일치하는 법령과 법률이 먼저 나옵니다 |
| `lawgokr_get_law` | 지정한 날짜(기본 오늘)에 **시행 중인** 법령 본문을 전체 또는 조문 하나(`제50조`, `43의2`)로 항·호·목까지 보여줍니다. 공포됐지만 나중에 시행되는 개정은 `upcoming`에 알려주고, `as_of`로 과거나 미래 어느 날짜든 조회할 수 있습니다 |
| `lawgokr_search_precedents` | 판례를 최신순으로 사건명, 본문, 사건번호(`2020다296604`, 띄어 써도 됨), 법원, 선고일로 검색합니다. 기본으로 법원 판례만 검색해서 모든 결과의 본문을 읽을 수 있습니다 |
| `lawgokr_get_precedent` | id나 사건번호로 판례를 조회합니다. 판시사항, 판결요지, 참조조문, 참조판례를 주고, `include_full_text`를 켜면 판결문 전문도 줍니다 |

**Hub**: `hub_list_providers`는 활성화된 provider와 빠진 키를 알려줍니다.

## 보안, 한도, 데이터 시점

- **키는 내 컴퓨터에만 있습니다.** mcp-hub는 로컬에서 실행되고, 각 키는 그 키를 발급한 서비스에만 보냅니다. 텔레메트리는 없습니다. 에러 메시지에는 키나 쿼리 문자열이 들어가지 않습니다.
- **HTTP 모드에는 인증이 없습니다.** `-http`로 실행하면 그 포트에 접속할 수 있는 누구나 내 키로 tool을 호출할 수 있습니다. 인증 프록시 뒤에 두는 경우가 아니라면 localhost에 바인딩하세요(`-http 127.0.0.1:8080`). 기본 stdio 모드는 포트를 열지 않습니다.
- **upstream 호출 한도가 적용됩니다.** 공공데이터포털 개발계정은 API마다 하루 10,000건까지 호출할 수 있고, OpenDART와 국가법령정보에도 각자 한도가 있습니다. mcp-hub는 응답을 10분, DART 회사 목록을 24시간 캐싱합니다.
- **데이터는 원천만큼 최신입니다.** 아파트 거래는 계약 후 30일 안에 신고되므로 최근 한두 달 데이터는 시간이 지나면서 채워집니다. 주가는 일별 데이터이고 거래일마다 갱신됩니다.

## 로드맵

- [x] OpenDART: 기업, 공시, 재무제표
- [x] 공공데이터포털: 공휴일, 기업 정보, 사업자 상태, 아파트 실거래가, 주가
- [x] 국가법령정보: 법령, 판례
- [ ] 한국은행 ECOS, KOSIS(국가통계포털)
- [ ] 다음 지역: 일본 EDINET (전자공시)

추가했으면 하는 데이터 소스가 있거나 이상하게 동작하는 API를 발견하면 [이슈를 열어 주세요](https://github.com/YoungsoonLee/mcp-hub/issues). 빌드, 테스트, provider 추가 방법은 [docs/DEVELOPMENT.ko.md](docs/DEVELOPMENT.ko.md)에 있습니다.

## 라이선스

[Apache License 2.0](LICENSE). tool이 돌려주는 데이터는 각 upstream 서비스에서 오며, 해당 서비스의 이용약관을 따릅니다.
