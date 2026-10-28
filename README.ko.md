# mcp-hub

[English](README.md) | **한국어**

**한국을 시작으로** 아시아의 공공·금융 데이터를 **하나의 규격으로 통일된 MCP 서버**로 제공합니다. 전 세계 어디서든 AI 에이전트가 데이터 소스마다 다른 특이한 동작을 신경 쓰지 않고 쓸 수 있게 하는 것이 목표입니다.

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

에이전트가 걸려 넘어질 만한 다른 문제도 모두 처리합니다.

- **최신처럼 보이는 옛날 데이터.** 정부 기업정보 API는 삼성전자 하나에 순서 없는 레코드 19개를 돌려주는데, 그중에는 전임 대표이사가 적힌 레코드도 있습니다. mcp-hub는 최신 레코드만 쓰고, 언제 기준 데이터인지(`as_of`) 함께 알려줍니다.
- **세면 안 되는 레코드.** 아파트 실거래가 데이터에는 해제된 계약이 표시만 된 채 남아 있습니다. 2025년 9월 강남구 거래 233건 중 12건이고, 75억 원짜리 거래도 있습니다. 이를 포함해 평균을 내면 틀립니다. mcp-hub는 요약에서 이를 뺍니다.
- **조용히 실패하는 입력.** 삼성전자 사업자번호를 흔히 쓰는 형식대로 `124-81-00998`로 국세청에 조회하면 "등록되지 않은 사업자등록번호"라고 답합니다. mcp-hub는 형식을 먼저 정리합니다. 주식시세 API에 `POSCO홀딩스`를 이름으로 조회하면 2년 치 이력이 빠집니다. 2022년 사명 변경 전 데이터는 `POSCO`로 기록되어 있기 때문입니다. mcp-hub는 항상 종목코드로 조회합니다.
- **성공처럼 보이는 에러.** OpenDART는 키 오류나 호출 한도 초과도 HTTP 200으로 보냅니다. data.go.kr은 잘못된 파라미터에 에러 대신 결과 0건을 줍니다. mcp-hub는 이를 [하나의 에러 형식](docs/CONVENTIONS.ko.md#6-에러)으로 바꾸고, 에이전트가 바로 따를 수 있는 `hint`를 붙입니다.
- **API마다 다른 형식.** 결과 코드가 `00`이기도 하고 `000`이기도 합니다. 결과가 1건이면 배열이 아니라 객체로 옵니다. `"84,500"`이 8억 4,500만 원을 뜻하고, 날짜가 `25.11.28`처럼 옵니다. mcp-hub는 항상 타입이 정해진 숫자, ISO 날짜, `page`/`page_size` 페이지네이션으로 돌려줍니다.
- **없는 기본 기능.** OpenDART에는 회사 검색이 없습니다. mcp-hub는 약 12만 개 회사를 인덱싱해서 `삼성전자`, `samsung electronics`, 종목코드 `005930` 모두 찾을 수 있게 합니다.
- **아직 법이 아닌 법.** law.go.kr에 현행 근로기준법을 요청하면 본문에 제44조의4가 2026-10-08부터 시행 중인 것처럼 들어 있습니다. 실제로는 **2027-01-01**부터 시행됩니다. 공식 "현행 법령" 본문에 공포만 되고 아직 시행되지 않은 개정이 섞여 있기 때문입니다. mcp-hub는 요청한 날짜(기본은 오늘)에 시행 중인 본문을 돌려주고, 앞으로 바뀔 내용은 `upcoming`에 알려줍니다.
- **조용히 멈추는 코드.** 2026년에 광주광역시와 전라남도가 통합되고, 화성시가 4개 구로 나뉘고, 인천의 중구·동구·서구가 개편됐습니다. 대부분의 코드표와 블로그 글에 아직 남아 있는 옛 시군구 코드로 조회하면 아파트 거래가 0건으로 나옵니다. **과거 달을 조회해도 마찬가지입니다.** 과거 데이터까지 새 코드로 옮겨졌기 때문입니다. mcp-hub에서는 에이전트가 `광주 북구`나 `화성시`라고만 하면 됩니다.
- **연결 문제.** Go 클라이언트로 OpenDART에 접속하면 `tls: handshake failure`가 납니다. mcp-hub가 이를 처리합니다.

모든 항목은 실제 API에서 확인했고, 실제 응답을 쓰는 테스트로 검증합니다. 전체 목록은 **[mcp-hub가 대신 처리하는 것들](docs/QUIRKS.ko.md)**에 있습니다.

## 실행

```sh
go build -o mcp-hub ./cmd/mcp-hub

./mcp-hub                       # stdio (Claude Desktop, Claude Code 등)
./mcp-hub -http :8080           # streamable HTTP
./mcp-hub -providers opendart   # 특정 provider만 활성화
./mcp-hub -env-file .env        # .env 파일에서 API 키 읽기
```

## API 키 설정

API 키는 환경변수로 설정합니다. 키가 없는 provider는 자동으로 비활성화되며, `hub_list_providers` tool로 어떤 키가 빠졌는지 확인할 수 있습니다. 값은 발급받은 문자열을 그대로 넣습니다.

| 환경변수 | Provider | 발급처 |
|---|---|---|
| `OPENDART_API_KEY` | OpenDART (전자공시) | https://opendart.fss.or.kr → 인증키 신청 |
| `DATAGOKR_SERVICE_KEY` | 공공데이터포털 | https://www.data.go.kr → 활용신청 → 마이페이지 |
| `LAWGOKR_OC` | 국가법령정보 | https://open.law.go.kr → OPEN API 신청(이 컴퓨터의 공인 IP 포함) → 마이페이지 → API인증키관리(OC) |

> **공공데이터포털 주의**: 키가 Encoding과 Decoding 두 가지로 발급됩니다. **Decoding 키**를 넣으세요. Encoding 키를 넣으면 인코딩이 두 번 되어 `SERVICE_KEY_IS_NOT_REGISTERED_ERROR`가 납니다.

> **국가법령정보 주의**: 신청할 때 **등록한 공인 IP에서만** 호출할 수 있습니다. IP가 바뀌면 open.law.go.kr에서 고쳐야 합니다. OC가 틀린 경우와 IP가 등록되지 않은 경우 모두 같은 에러("사용자 정보 검증에 실패하였습니다")가 납니다. law.go.kr은 돌려주는 모든 링크에 OC를 넣으므로, 다른 키를 OC로 재사용하지 마세요(mcp-hub는 그 링크를 넘기지 않습니다).

### 방법 1: MCP 클라이언트 설정에 직접 넣기

Claude Code:

```sh
claude mcp add mcp-hub \
  -e OPENDART_API_KEY=... \
  -e DATAGOKR_SERVICE_KEY=... \
  -- /path/to/mcp-hub
```

Claude Desktop 등 JSON 설정:

```json
{
  "mcpServers": {
    "mcp-hub": {
      "command": "/path/to/mcp-hub",
      "env": { "OPENDART_API_KEY": "...", "DATAGOKR_SERVICE_KEY": "..." }
    }
  }
}
```

### 방법 2: `.env` 파일

```sh
cp .env.example .env    # 값을 채운 뒤
./mcp-hub -env-file /absolute/path/to/.env
```

- MCP 클라이언트는 실행 디렉토리가 일정하지 않으므로 **절대 경로**를 쓰세요.
- 형식은 `KEY=VALUE`이고 `#` 주석, `export ` 접두사, 따옴표를 지원합니다.
- 같은 변수가 셸 환경이나 클라이언트 설정에도 있으면 **그쪽이 우선**합니다.
- `.env`는 `.gitignore`에 포함되어 있습니다. 커밋하지 마세요.

## Tool 목록

`hub_list_providers`를 호출하면 활성화된 provider와 빠진 키를 확인할 수 있습니다.

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

## 로컬에서 테스트하기

### 1. 단위 테스트 (API 키 불필요)

```sh
go test -race ./...
go test -race -v -run TestGetFinancials ./providers/opendart   # 특정 테스트만
```

provider마다 `testdata/`에 저장해 둔 실제 upstream 응답과 가짜 upstream 서버로 테스트하므로 네트워크나 키가 필요 없습니다.

### 2. MCP Inspector (실제 API로 tool 호출)

[MCP Inspector](https://github.com/modelcontextprotocol/inspector)는 MCP 공식 디버깅 UI입니다. Node.js가 필요합니다.

```sh
go build -o mcp-hub ./cmd/mcp-hub
npx @modelcontextprotocol/inspector -- ./mcp-hub -env-file $PWD/.env
```

> `--`를 빼면 안 됩니다. 빼면 Inspector가 `-env-file`을 자기 옵션 `-e`로 읽어서 `Invalid parameter format: nv-file` 에러가 납니다.

또는 인자 없이 `npx @modelcontextprotocol/inspector`를 실행하고 왼쪽 패널을 직접 채워도 됩니다. **Transport Type**은 `STDIO`, **Command**는 `mcp-hub`의 절대 경로, **Arguments**는 `-env-file /absolute/path/to/.env`입니다.

그다음 순서는 이렇습니다.

1. **Connect**를 누르고 **Tools** 탭에서 **List Tools**를 누릅니다.
2. tool을 고르고 입력값을 넣은 뒤 **Execute Tool**을 누릅니다. 예를 들어 `opendart_get_company`의 `corp_code`에 `000660`(SK하이닉스)을 넣습니다.

![MCP Inspector에서 opendart_get_company 호출](docs/images/inspector-call-tool.png)

3. 결과는 **Results**와 **Structured Output**에 나옵니다.

![MCP Inspector의 opendart_get_company 결과](docs/images/inspector-result.png)

4. 다른 tool도 같은 방식으로 호출합니다. 예를 들어 `opendart_get_financials`에 `corp_code` `000660`, `year` `2025`를 넣으면 주요계정이 나옵니다. 금액은 원 단위 숫자, 날짜는 ISO 형식이고, 전기 대비 비교값도 함께 나옵니다.

![MCP Inspector의 opendart_get_financials 결과](docs/images/inspector-financials.png)

`corp_code`에는 회사명이 아니라 코드를 넣습니다. `005930`(삼성전자), `000660`(SK하이닉스), `035720`(카카오)을 쓰거나, 먼저 `opendart_search_companies`(`query`에 `삼성전자` 또는 `samsung electronics`)로 찾으세요. 회사명을 넣으면 hint가 담긴 `INVALID_ARGUMENT`가 나오므로, 에러 형식을 확인하는 용도로 써 볼 수 있습니다.

처음 회사를 조회할 때는 몇 초 걸립니다. DART 전체 회사 목록(약 12만 개)을 받아 메모리 인덱스를 만들기 때문입니다. 이후 24시간 동안은 바로 응답합니다.

### 3. Claude Code (실제 에이전트에서 사용)

```sh
claude mcp add mcp-hub -- /absolute/path/to/mcp-hub -env-file /absolute/path/to/.env
```

새 세션에서 `/mcp`로 연결 상태를 확인한 뒤 "삼성전자 2025년 상반기 매출이랑 최근 공시 알려줘"처럼 물어보세요. 에이전트가 tool 설명만 보고 올바른 tool과 인자를 고르는지 확인할 수 있습니다.

### 4. HTTP 모드

```sh
./mcp-hub -http :8080 -env-file .env
```

Inspector에서 **Transport Type**을 `Streamable HTTP`로, URL을 `http://localhost:8080`으로 설정합니다.

서버 로그는 stderr로 나옵니다. 디버그 로그를 보려면 `-v`를 붙이세요.

## 구조

| 경로 | 역할 |
|---|---|
| `hub/` | Provider 등록, tool 이름 규칙, 표준 에러, 페이지네이션 |
| `httpx/` | upstream HTTP 클라이언트 (재시도, 캐시, 에러 매핑, API 키 마스킹) |
| `normalize/` | 날짜, 금액, HTML 텍스트 정규화 |
| `cache/` | TTL 메모리 캐시 |
| `providers/` | provider 목록 |
| `cmd/mcp-hub/` | 실행 파일 |

Provider를 추가할 때는 [docs/CONVENTIONS.ko.md](docs/CONVENTIONS.ko.md)를 따릅니다.

## Provider 로드맵

- [x] OpenDART (전자공시): 실제 API 검증 완료
- [x] 공공데이터포털: 실제 API 검증 완료
  - [x] 공휴일(특일 정보): 실제 API 검증 완료
  - [x] 기업기본정보: 실제 API 검증 완료
  - [x] 사업자등록 상태조회: 실제 API 검증 완료
  - [x] 아파트 매매 실거래가: 실제 API 검증 완료, 지역명 입력 지원
  - [x] 주식시세: 실제 API 검증 완료
- [x] 국가법령정보 (법령, 판례): 실제 API 검증 완료
- [ ] 한국은행 ECOS, KOSIS
- [ ] 다음 지역: 일본 EDINET (전자공시)

## 라이선스

[Apache License 2.0](LICENSE). tool이 돌려주는 데이터는 각 upstream 서비스에서 오며, 해당 서비스의 이용약관을 따릅니다.
