# mcp-hub

[English](README.md) | **한국어**

한국 서비스(공공데이터, 전자공시, 네이버 등)를 **하나의 규격으로 통일된 MCP 서버**로 제공합니다.

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
| `NAVER_CLIENT_ID`, `NAVER_CLIENT_SECRET` | 네이버 검색 | https://developers.naver.com → 애플리케이션 등록 (검색 API 선택) |
| `DATAGOKR_SERVICE_KEY` | 공공데이터포털 | https://www.data.go.kr → 활용신청 → 마이페이지 |

> **공공데이터포털 주의**: 키가 Encoding과 Decoding 두 가지로 발급됩니다. **Decoding 키**를 넣으세요. Encoding 키를 넣으면 인코딩이 두 번 되어 `SERVICE_KEY_IS_NOT_REGISTERED_ERROR`가 납니다.

### 방법 1: MCP 클라이언트 설정에 직접 넣기

Claude Code:

```sh
claude mcp add mcp-hub \
  -e OPENDART_API_KEY=... \
  -e NAVER_CLIENT_ID=... -e NAVER_CLIENT_SECRET=... \
  -- /path/to/mcp-hub
```

Claude Desktop 등 JSON 설정:

```json
{
  "mcpServers": {
    "mcp-hub": {
      "command": "/path/to/mcp-hub",
      "env": { "OPENDART_API_KEY": "...", "NAVER_CLIENT_ID": "...", "NAVER_CLIENT_SECRET": "..." }
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

- [ ] OpenDART (전자공시)
- [ ] 네이버 검색 (블로그, 뉴스, 쇼핑, 지역)
- [ ] 공공데이터포털 인기 API
- [ ] 국가법령정보
- [ ] 한국은행 ECOS, KOSIS
