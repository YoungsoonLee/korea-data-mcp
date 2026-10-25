# mcp-hub 규격 (Conventions)

[English](CONVENTIONS.md) | **한국어**

모든 provider는 이 규격을 따릅니다. upstream API가 제각각이어도, 에이전트 입장에서는 **같은 모양의 입력·출력·에러**를 보게 만드는 것이 mcp-hub의 핵심 가치입니다.

## 1. Provider

- `hub.Provider` 인터페이스를 구현하고 `providers/providers.go`의 `All()`에 추가합니다.
- `ID`는 소문자와 숫자만 씁니다(`opendart`, `datagokr`, `ecos`). 이 값이 tool 이름의 prefix가 됩니다.
- API 키는 환경변수로 받습니다(`RequiredEnv`). 키가 없으면 provider는 자동으로 비활성화되고, `hub_list_providers`에 `missing_env`로 표시됩니다.
- `ProviderInfo.DocsURL`에는 키를 발급받는 페이지 주소를 넣습니다.

### 환경변수 이름

- 형식: `<PROVIDER>_<항목>`을 대문자 snake_case로 씁니다. `<PROVIDER>`는 provider `ID`를 대문자로 바꾼 값입니다.
- 항목 이름은 upstream이 부르는 이름을 따릅니다. 하나짜리 키는 `_API_KEY`, ID와 secret 쌍은 `_CLIENT_ID`/`_CLIENT_SECRET`, data.go.kr처럼 고유 명칭이 있으면 그 이름(`_SERVICE_KEY`)을 씁니다.
- 값은 발급받은 문자열 그대로 받습니다. 인코딩이 필요하면 provider 코드가 처리합니다.
- 새 provider를 추가하면 두 README(`README.md`, `README.ko.md`)의 환경변수 표와 `.env.example`도 함께 갱신합니다.

| Provider ID | 환경변수 |
|---|---|
| `opendart` | `OPENDART_API_KEY` |
| `datagokr` | `DATAGOKR_SERVICE_KEY` (**Decoding 키**. `httpx`가 query를 인코딩하므로 Encoding 키를 쓰면 두 번 인코딩되어 인증에 실패합니다) |

## 2. Tool 이름

- 형식: `<provider>_<verb>_<noun>` (snake_case). 예: `opendart_search_disclosures`, `opendart_get_financials`
- 동사는 다음 중에서 고릅니다.
  - `search`: 조건으로 목록 검색
  - `get`: ID로 단건 조회
  - `list`: 조건 없이 전체 목록
- `hub.AddTool`이 규칙을 검사합니다. 위반하면 서버 시작 시 panic이 발생합니다.

## 3. Tool 설명 (description)

- 영어로 작성하고, 한국 고유 용어는 괄호에 한글을 병기합니다. 예: `"Search corporate disclosures (공시) filed to DART."`
- 무엇을 반환하는지, 언제 써야 하는지, 다른 tool과 어떻게 이어지는지를 적습니다. 예: `"Use corp_code from opendart_search_companies."`
- 입력 필드 설명은 `jsonschema:"..."` 태그로 씁니다. 형식 예시도 함께 적습니다(`"YYYY-MM-DD"`).

## 4. 입력

- 필드명은 snake_case를 씁니다. upstream 이름(`crtfc_key`, `pageNo`, `numOfRows`)을 그대로 노출하지 않습니다.
- 날짜는 `YYYY-MM-DD`로 받고, provider 내부에서 upstream 형식(`YYYYMMDD` 등)으로 변환합니다.
- API 키는 절대 tool 입력으로 받지 않습니다. 항상 환경변수에서 읽습니다.
- 목록 tool은 `hub.PageParams`를 embed합니다(`page`, `page_size`). 범위를 벗어나면 조용히 보정하지 않고 `INVALID_ARGUMENT`를 반환합니다.

## 5. 출력

- 항상 구조화된 Go 타입(struct)을 반환합니다. SDK가 output schema와 `structuredContent`를 자동으로 생성합니다.
- 목록은 `hub.List[T]`를 씁니다:
  ```json
  {"items": [...], "page_info": {"page": 1, "page_size": 10, "total_count": 42, "has_next": true}}
  ```
- 결과가 0건이면 에러가 아니라 빈 `items`를 반환합니다. `NOT_FOUND`는 ID로 단건을 조회했는데 없을 때만 씁니다.
- 값 정규화는 `normalize` 패키지를 사용합니다.

  | 원본 | 변환 | 함수 |
  |---|---|---|
  | `20251019`, `2025.10.19.` | `"2025-10-19"` | `normalize.Date` |
  | `"1,234,567"`, `"(1,234)"`, `"-"` | `1234567`, `-1234`, `null` | `normalize.Int` |
  | `<b>삼성</b> &quot;AI&quot;` | `삼성 "AI"` | `normalize.Text` |

- 값이 없으면 `0`이나 `"-"`가 아니라 `null`(Go에서는 포인터 또는 `omitempty`)로 표현합니다.
- 금액 단위는 원(KRW)으로 통일합니다. upstream이 천원이나 백만원 단위로 주면 변환하고, 필드 설명에 단위를 적습니다.
- 응답이 너무 크면 필요한 필드만 추립니다. 컨텍스트 비용을 줄이는 것도 규격의 목적입니다.

## 6. 에러

모든 에러는 `hub.Error` 하나의 형태로 반환되며, tool 결과에 `isError: true`와 JSON 텍스트로 들어갑니다.

```json
{"code":"RATE_LIMITED","message":"...","hint":"...","retryable":true,"provider":"opendart","upstream_code":"020"}
```

| code | 의미 | 에이전트가 할 일 |
|---|---|---|
| `INVALID_ARGUMENT` | 입력 오류 | 인자를 고쳐서 다시 호출 |
| `NOT_FOUND` | ID에 해당하는 대상 없음 | 검색 tool로 ID 확인 |
| `UNAUTHENTICATED` | API 키 없음, 잘못됨, 만료 | 사용자에게 키 설정 요청 |
| `PERMISSION_DENIED` | 권한 없음(미신청 API 등) | 사용자에게 안내 |
| `RATE_LIMITED` | 호출 한도 초과 | 나중에 재시도 |
| `UNAVAILABLE` | upstream 장애, 타임아웃 | 재시도 |
| `UPSTREAM_ERROR` | 기타 upstream 오류 | 사용자에게 보고 |
| `INTERNAL` | mcp-hub 버그 | 사용자에게 보고 |

- HTTP 200 안에 에러 코드를 담아 보내는 API(OpenDART `status`, data.go.kr `resultCode`)는 provider가 위 코드로 매핑하고, 원래 코드는 `upstream_code`에 넣습니다.
- `hint`에는 모델이 스스로 고칠 수 있도록 구체적인 다음 행동을 적습니다.
- 에러 메시지에 API 키나 query string이 들어가면 안 됩니다. `httpx`가 URL을 `scheme://host/path`까지만 남깁니다.

## 7. HTTP 호출

- 직접 `http.Client`를 쓰지 말고 `httpx.Client`를 사용합니다. 타임아웃, 429/502/503/504 재시도(지수 backoff, Retry-After 반영), 상태 코드에서 에러 코드로의 매핑, 키 마스킹이 기본으로 들어 있습니다.
- 자주 바뀌지 않는 데이터는 `httpx.WithCache(ttl, max)`로 캐싱해서 호출 한도를 아낍니다.

## 8. 테스트

- upstream 응답 샘플을 `testdata/`에 저장하고 `httptest`로 재생합니다. 실제 API 호출은 테스트에서 하지 않습니다.
- tool 단위 테스트는 `mcp.NewInMemoryTransports()`로 실제 MCP 호출 경로를 검증합니다(`hub/hub_test.go` 참고).

## 9. 문서

- 문서는 영어(`*.md`, 기본)와 한국어(`*.ko.md`) 두 벌로 관리합니다. 한쪽을 고치면 같은 변경에서 다른 쪽도 고칩니다.
