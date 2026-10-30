# API 키 발급 가이드

[English](KEYS.md) | **한국어**

korea-data-mcp는 데이터 소스마다 무료 키가 필요합니다. **하나만** 받아도 시작할 수 있습니다. 키마다 해당 tool이 켜지고, 키가 없는 tool은 목록에 나오지 않을 뿐입니다.

| 키 | 쓸 수 있는 것 | 걸리는 시간 | 난이도 |
|---|---|---|---|
| [OpenDART](#opendart-여기서-시작) | 기업, 공시, 재무제표 (tool 4개) | 몇 분 | 쉬움 |
| [공공데이터포털](#공공데이터포털) | 공휴일, 기업 정보, 사업자 상태, 아파트 실거래가, 주가 (tool 6개) | 몇 분에서 몇 시간 | 보통: API마다 활용신청 |
| [국가법령정보](#국가법령정보) | 법령, 판례 (tool 4개) | 몇 분 | 쉬움 |

> **키 발급이 번거롭다면:** 키 없이 URL만으로 연결하는 호스팅 버전을 준비하고 있습니다. [대기 명단에 등록](https://github.com/YoungsoonLee/korea-data-mcp/issues/new?template=hosted-access.yml)해 주세요.

## OpenDART (여기서 시작)

1. [opendart.fss.or.kr](https://opendart.fss.or.kr)에 가입합니다.
2. **인증키 신청**에서 양식을 제출하면 40자리 키가 바로 발급됩니다.
3. `OPENDART_API_KEY`에 넣습니다.

그다음 에이전트에게 "삼성전자 2025년 상반기 매출 얼마야?"라고 물어보세요.

키 하나로 하루 약 20,000건을 호출할 수 있어서, 에이전트가 쓰기에는 충분합니다.

## 공공데이터포털

포털 전체에 키는 하나지만, **API마다 따로 활용신청**을 해야 합니다. 아래 API들은 신청하면 자동으로 승인됩니다.

1. [data.go.kr](https://www.data.go.kr)에 가입합니다.
2. 아래 API 페이지를 하나씩 열고 **활용신청**을 누릅니다. 활용 목적을 한 문장(예: "AI 에이전트 데이터 조회")으로 적어 제출합니다. 필요한 것만 신청하면 됩니다.

   | API | tool |
   |---|---|
   | [한국천문연구원_특일 정보](https://www.data.go.kr/data/15012690/openapi.do) | `datagokr_list_holidays` |
   | [금융위원회_기업기본정보](https://www.data.go.kr/data/15043184/openapi.do) | `datagokr_search_corporations`, `datagokr_get_corporation` |
   | [국세청_사업자등록정보 진위확인 및 상태조회](https://www.data.go.kr/data/15081808/openapi.do) | `datagokr_check_business_status` |
   | [국토교통부_아파트 매매 실거래가 자료](https://www.data.go.kr/data/15126469/openapi.do) | `datagokr_list_apartment_trades` |
   | [금융위원회_주식시세정보](https://www.data.go.kr/data/15094808/openapi.do) | `datagokr_get_stock_prices` |

3. **마이페이지**에서 승인된 API를 열면 키가 보입니다. **Encoding**과 **Decoding** 두 가지가 나오는데, **Decoding** 키를 `DATAGOKR_SERVICE_KEY`에 넣습니다.
4. 방금 승인된 API는 실제로 동작하기까지 몇 분에서 몇 시간이 걸릴 수 있습니다. 그 전에는 키가 틀린 것처럼 응답합니다.

개발계정은 API마다 하루 10,000건까지 호출할 수 있습니다.

## 국가법령정보

1. [open.law.go.kr](https://open.law.go.kr)에 가입합니다.
2. **OPEN API 신청**에서 법령과 판례 API를 선택합니다.
3. 신청하면 바로 승인됩니다.
4. **마이페이지 → API인증키관리**에서 **OC**를 정합니다. 직접 정하는 값이고 최대 50자입니다. `LAWGOKR_OC`에 넣습니다.

알아 둘 점이 있습니다.

- OC가 틀리면 "사용자 정보 검증에 실패하였습니다" 에러가 납니다. korea-data-mcp는 이를 `UNAUTHENTICATED`로 알려주고, OC를 확인할 위치를 hint에 적습니다.
- law.go.kr은 돌려주는 모든 링크에 OC를 넣습니다. 다른 키나 비밀번호를 재사용하지 말고 OC 전용 값을 정하세요. (korea-data-mcp는 그 링크를 넘기지 않습니다.)

## 설정 확인

에이전트에게 "korea-data-mcp에서 어떤 provider가 활성화돼 있어?"라고 물어보세요. `hub_list_providers`를 호출해서 활성화된 데이터 소스와 빠진 키를 알려줍니다.

| 에러 | 흔한 원인 |
|---|---|
| 공공데이터포털의 `UNAUTHENTICATED` | Decoding 키 대신 Encoding 키를 넣음, 해당 API를 활용신청하지 않음, 또는 승인 직후라 몇 시간 기다려야 함 |
| 공공데이터포털의 `PERMISSION_DENIED` | 해당 API를 활용신청하지 않음 |
| 국가법령정보의 `UNAUTHENTICATED` | OC가 마이페이지 → API인증키관리의 값과 다름 |
| OpenDART의 `UNAUTHENTICATED` | 키를 잘못 입력했거나 비활성 상태임 |
| `RATE_LIMITED` | 하루 한도에 도달함. 다음 날 초기화됨 |
