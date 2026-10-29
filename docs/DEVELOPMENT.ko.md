# 개발 가이드

[English](DEVELOPMENT.md) | **한국어**

mcp-hub를 빌드하고 테스트하고 디버깅하는 방법과 코드 구조를 설명합니다. provider나 tool을 추가할 때는 [CONVENTIONS.ko.md](CONVENTIONS.ko.md)도 함께 읽으세요.

## 빌드

```sh
go build -o mcp-hub ./cmd/mcp-hub

./mcp-hub                            # stdio (Claude Desktop, Claude Code 등)
./mcp-hub -http 127.0.0.1:8080       # streamable HTTP
./mcp-hub -providers opendart        # 특정 provider만 활성화
./mcp-hub -env-file /abs/path/.env   # .env 파일에서 API 키 읽기
./mcp-hub -v                         # 디버그 로그 (stderr)
```

## 1. 단위 테스트 (API 키 불필요)

```sh
go test -race ./...
go test -race -v -run TestGetFinancials ./providers/opendart   # 특정 테스트만
```

provider마다 `testdata/`에 저장해 둔 실제 upstream 응답과 가짜 upstream 서버로 테스트하므로 네트워크나 키가 필요 없습니다. 각 파일이 어떤 요청의 응답인지는 `testdata/README.md`에 적혀 있습니다.

## 2. MCP Inspector (실제 API로 tool 호출)

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

![MCP Inspector에서 opendart_get_company 호출](images/inspector-call-tool.png)

3. 결과는 **Results**와 **Structured Output**에 나옵니다.

![MCP Inspector의 opendart_get_company 결과](images/inspector-result.png)

4. 다른 tool도 같은 방식으로 호출합니다. 예를 들어 `opendart_get_financials`에 `corp_code` `000660`, `year` `2025`를 넣습니다.

![MCP Inspector의 opendart_get_financials 결과](images/inspector-financials.png)

`corp_code`에 회사명을 넣으면 hint가 담긴 `INVALID_ARGUMENT`가 나오므로, 에러 형식을 확인하는 용도로 써 볼 수 있습니다.

처음 OpenDART 회사를 조회할 때는 몇 초 걸립니다. DART 전체 회사 목록(약 12만 개)을 받아 메모리 인덱스를 만들기 때문입니다. 이후 24시간 동안은 바로 응답합니다.

## 3. Claude Code (실제 에이전트에서 사용)

```sh
claude mcp add mcp-hub -- /absolute/path/to/mcp-hub -env-file /absolute/path/to/.env
```

새 세션에서 `/mcp`로 연결 상태를 확인한 뒤 질문해 보세요. 에이전트가 tool 설명만 보고 올바른 tool과 인자를 고르는지 확인할 수 있습니다. tool 설명은 이 기준으로 평가해야 합니다.

## 4. HTTP 모드

```sh
./mcp-hub -http 127.0.0.1:8080 -env-file /absolute/path/to/.env
```

Inspector에서 **Transport Type**을 `Streamable HTTP`로, URL을 `http://127.0.0.1:8080`으로 설정합니다.

HTTP 서버에는 **인증이 없습니다.** 접속할 수 있는 사람은 누구나 내 API 키로 tool을 호출할 수 있습니다. 인증 프록시 뒤에 두는 경우가 아니라면 `:8080`이 아니라 `127.0.0.1`에 바인딩하세요.

## 구조

| 경로 | 역할 |
|---|---|
| `hub/` | Provider 등록, tool 이름 규칙, 표준 에러, 페이지네이션 |
| `httpx/` | upstream HTTP 클라이언트 (재시도, 캐시, 에러 매핑, API 키 마스킹) |
| `normalize/` | 날짜, 금액, 회사명, HTML 텍스트 정규화 |
| `cache/` | TTL 메모리 캐시 |
| `providers/` | 데이터 소스마다 패키지 하나. `providers.go`에 목록이 있습니다 |
| `cmd/mcp-hub/` | 실행 파일 |
| `docs/QUIRKS.ko.md` | mcp-hub가 처리하는 upstream 특이 동작 전체와 처리 상태 |

## 새 특이 동작 기록하기

upstream API에서 예상 밖의 동작을 발견하면 실제 응답을 provider의 `testdata/`에 저장하고(요청 URL은 키가 들어 있으므로 저장하지 않습니다), 테스트로 검증하고, [QUIRKS.md](QUIRKS.md)와 [QUIRKS.ko.md](QUIRKS.ko.md)에 행을 추가합니다.
