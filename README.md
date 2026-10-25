# mcp-hub

**English** | [한국어](README.ko.md)

Korean services (public data, corporate disclosures, statutes, statistics, and more) served as **MCP servers that all follow one consistent specification**.

## Running

```sh
go build -o mcp-hub ./cmd/mcp-hub

./mcp-hub                       # stdio (Claude Desktop, Claude Code, etc.)
./mcp-hub -http :8080           # streamable HTTP
./mcp-hub -providers opendart   # enable only specific providers
./mcp-hub -env-file .env        # read API keys from a .env file
```

## API key setup

API keys are set through environment variables. A provider whose keys are missing is disabled automatically, and the `hub_list_providers` tool shows which keys are missing. Use each key exactly as issued.

| Environment variable | Provider | Where to get it |
|---|---|---|
| `OPENDART_API_KEY` | OpenDART (전자공시, corporate disclosures) | https://opendart.fss.or.kr → 인증키 신청 (request an API key) |
| `DATAGOKR_SERVICE_KEY` | data.go.kr (공공데이터포털, Public Data Portal) | https://www.data.go.kr → 활용신청 (apply for use) → My Page |

> **data.go.kr note**: The portal issues two keys, Encoding and Decoding. Use the **Decoding key**. The Encoding key gets URL-encoded twice and fails with `SERVICE_KEY_IS_NOT_REGISTERED_ERROR`.

### Option 1: In your MCP client configuration

Claude Code:

```sh
claude mcp add mcp-hub \
  -e OPENDART_API_KEY=... \
  -e DATAGOKR_SERVICE_KEY=... \
  -- /path/to/mcp-hub
```

Claude Desktop and other clients configured with JSON:

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

### Option 2: A `.env` file

```sh
cp .env.example .env    # then fill in the values
./mcp-hub -env-file /absolute/path/to/.env
```

- Use an **absolute path**, because MCP clients do not launch servers from a predictable working directory.
- The format is `KEY=VALUE`, with support for `#` comments, an `export ` prefix, and quoted values.
- If the same variable is also set (non-empty) in the shell or the client configuration, **that value takes precedence**.
- `.env` is listed in `.gitignore`. Do not commit it.

## Tools

Call `hub_list_providers` to see which providers are enabled and which keys are missing.

**OpenDART** (`OPENDART_API_KEY`)

| Tool | What it does |
|---|---|
| `opendart_search_companies` | Find a company's `corp_code` by Korean/English name, stock code, or corp_code |
| `opendart_get_company` | Company profile (기업개황): CEO, market, registration numbers, address, fiscal year-end |
| `opendart_search_disclosures` | Search disclosures (공시) by company, date range, type, and market |
| `opendart_get_financials` | Key financial accounts (주요계정) for a year and report, with prior-period comparisons |

Every OpenDART tool that takes `corp_code` also accepts a 6-character stock code (e.g. `005930`).

## Layout

| Path | Purpose |
|---|---|
| `hub/` | Provider registration, tool naming rules, standard errors, pagination |
| `httpx/` | Upstream HTTP client (retries, caching, error mapping, API key redaction) |
| `normalize/` | Normalization of dates, amounts, and HTML text |
| `cache/` | In-memory TTL cache |
| `providers/` | List of providers |
| `cmd/mcp-hub/` | Executable |

When adding a provider, follow [docs/CONVENTIONS.md](docs/CONVENTIONS.md).

## Provider roadmap

- [x] OpenDART (전자공시, corporate disclosures): implemented, pending verification against the live API
- [ ] Popular data.go.kr APIs
- [ ] 국가법령정보 (Korean Law Information Center)
- [ ] Bank of Korea ECOS, KOSIS (Korean Statistical Information Service)
