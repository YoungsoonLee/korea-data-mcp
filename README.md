# mcp-hub

**English** | [한국어](README.ko.md)

Asian public and financial data, **starting with Korea**, served as **MCP servers that all follow one consistent specification**, so AI agents anywhere can use it without dealing with each source's quirks.

Official APIs such as OpenDART (Korean corporate disclosures) return errors inside HTTP 200, have no company-name search, and send amounts as strings like `"1,234"` or `"-"`. mcp-hub handles all of this and returns clean, typed results with English tool descriptions, ISO dates, and standard errors.

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

## Testing locally

### 1. Unit tests (no API key needed)

```sh
go test -race ./...
go test -race -v -run TestGetFinancials ./providers/opendart   # a single test
```

The tests use real upstream responses saved under each provider's `testdata/` and a fake upstream server, so they need no network or keys.

### 2. MCP Inspector (call tools against the live API)

[MCP Inspector](https://github.com/modelcontextprotocol/inspector) is the official MCP debugging UI. It requires Node.js.

```sh
go build -o mcp-hub ./cmd/mcp-hub
npx @modelcontextprotocol/inspector -- ./mcp-hub -env-file $PWD/.env
```

> Keep the `--`. Without it, Inspector reads `-env-file` as its own `-e` option and fails with `Invalid parameter format: nv-file`.

Alternatively, run `npx @modelcontextprotocol/inspector` with no arguments and fill in the left panel: **Transport Type** `STDIO`, **Command** the absolute path to `mcp-hub`, **Arguments** `-env-file /absolute/path/to/.env`.

Then:

1. Click **Connect**, open the **Tools** tab, and click **List Tools**.
2. Pick a tool, fill in its inputs, and click **Execute Tool**. For example, `opendart_get_company` with `corp_code` = `000660` (SK hynix):

![Calling opendart_get_company in MCP Inspector](docs/images/inspector-call-tool.png)

3. The result appears under **Results** and **Structured Output**:

![opendart_get_company result in MCP Inspector](docs/images/inspector-result.png)

4. Try the other tools the same way. For example, `opendart_get_financials` with `corp_code` = `000660` and `year` = `2025` returns key accounts with amounts in KRW as plain numbers, ISO dates, and prior-period comparisons:

![opendart_get_financials result in MCP Inspector](docs/images/inspector-financials.png)

`corp_code` takes a code, not a company name. Use `005930` (Samsung Electronics), `000660` (SK hynix), or `035720` (Kakao), or find one first with `opendart_search_companies` (`query` = `삼성전자` or `samsung electronics`). Passing a name returns `INVALID_ARGUMENT` with a hint, which is a quick way to see the error format.

The first company lookup takes a few seconds, because it downloads DART's full company list (about 120,000 entries) and builds an in-memory index. Later lookups are instant for 24 hours.

### 3. Claude Code (use it from a real agent)

```sh
claude mcp add mcp-hub -- /absolute/path/to/mcp-hub -env-file /absolute/path/to/.env
```

Start a new session, check the connection with `/mcp`, and ask something like "What were Samsung Electronics' revenue in the first half of 2025 and its latest disclosures?". This shows whether an agent picks the right tools and arguments from the descriptions alone.

### 4. HTTP mode

```sh
./mcp-hub -http :8080 -env-file .env
```

In Inspector, set **Transport Type** to `Streamable HTTP` and the URL to `http://localhost:8080`.

Server logs go to stderr. Add `-v` for debug logs.

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

- [x] OpenDART (전자공시, corporate disclosures): verified against the live API
- [ ] Popular data.go.kr APIs
- [ ] 국가법령정보 (Korean Law Information Center)
- [ ] Bank of Korea ECOS, KOSIS (Korean Statistical Information Service)
- [ ] Next region: Japan EDINET (corporate disclosures)
