# mcp-hub Conventions

**English** | [한국어](CONVENTIONS.ko.md)

Every provider follows these conventions. Upstream APIs differ wildly, but agents should always see **the same shape of inputs, outputs, and errors**. That consistency is the core value of mcp-hub.

## 1. Providers

- Implement the `hub.Provider` interface and add it to `All()` in `providers/providers.go`.
- `ID` uses only lowercase letters and digits (`opendart`, `datagokr`, `ecos`). It becomes the tool name prefix.
- API keys come from environment variables (`RequiredEnv`). A provider with missing keys is disabled automatically and shown in `hub_list_providers` with `missing_env`.
- Set `ProviderInfo.DocsURL` to the page where users obtain a key.

### Environment variable names

- Format: `<PROVIDER>_<ITEM>` in uppercase snake_case, where `<PROVIDER>` is the provider `ID` in uppercase.
- Name the item the way the upstream service does: `_API_KEY` for a single key, `_CLIENT_ID`/`_CLIENT_SECRET` for an ID and secret pair, and the service's own term where one exists (`_SERVICE_KEY` for data.go.kr).
- Values are accepted exactly as issued. If encoding is needed, the provider code handles it.
- When adding a provider, also update the environment variable tables in both READMEs (`README.md`, `README.ko.md`) and `.env.example`.

| Provider ID | Environment variables |
|---|---|
| `opendart` | `OPENDART_API_KEY` |
| `datagokr` | `DATAGOKR_SERVICE_KEY` (the **Decoding key**: `httpx` encodes the query, so the Encoding key would be encoded twice and fail authentication) |

## 2. Tool names

- Format: `<provider>_<verb>_<noun>` (snake_case). Examples: `opendart_search_disclosures`, `opendart_get_financials`
- Choose the verb from:
  - `search`: find a list of items by criteria
  - `get`: fetch a single item by ID
  - `list`: list everything without criteria
- `hub.AddTool` enforces these rules. A violation panics at server startup.

## 3. Tool descriptions

- Write them in English, with Korean-specific terms followed by the Korean in parentheses. Example: `"Search corporate disclosures (공시) filed to DART."`
- State what the tool returns, when to use it, and how it connects to other tools. Example: `"Use corp_code from opendart_search_companies."`
- Describe input fields with the `jsonschema:"..."` tag, including a format example (`"YYYY-MM-DD"`).

## 4. Inputs

- Field names use snake_case. Do not expose upstream names (`crtfc_key`, `pageNo`, `numOfRows`) as-is.
- Dates are accepted as `YYYY-MM-DD` and converted inside the provider to the upstream format (`YYYYMMDD`, etc.).
- Never accept API keys as tool inputs. Always read them from the environment.
- List tools embed `hub.PageParams` (`page`, `page_size`). Out-of-range values return `INVALID_ARGUMENT` instead of being silently clamped.

## 5. Outputs

- Always return a structured Go type (struct). The SDK generates the output schema and `structuredContent` automatically.
- Lists use `hub.List[T]`:
  ```json
  {"items": [...], "page_info": {"page": 1, "page_size": 10, "total_count": 42, "has_next": true}}
  ```
- Zero results is not an error: return an empty `items`. Use `NOT_FOUND` only when a single item looked up by ID does not exist.
- Normalize values with the `normalize` package.

  | Upstream value | Normalized | Function |
  |---|---|---|
  | `20251019`, `2025.10.19.` | `"2025-10-19"` | `normalize.Date` |
  | `"1,234,567"`, `"(1,234)"`, `"-"` | `1234567`, `-1234`, `null` | `normalize.Int` |
  | `<b>삼성</b> &quot;AI&quot;` | `삼성 "AI"` | `normalize.Text` |

- Represent missing values as `null` (a pointer or `omitempty` in Go), never as `0` or `"-"`.
- Express amounts in won (KRW). Convert upstream values given in thousands or millions of won, and state the unit in the field description.
- Trim large responses down to the fields that matter. Reducing context cost is part of the purpose of these conventions.

## 6. Errors

Every error is returned in the single `hub.Error` shape, placed in the tool result as JSON text with `isError: true`.

```json
{"code":"RATE_LIMITED","message":"...","hint":"...","retryable":true,"provider":"opendart","upstream_code":"020"}
```

| code | Meaning | What the agent should do |
|---|---|---|
| `INVALID_ARGUMENT` | Bad input | Fix the arguments and call again |
| `NOT_FOUND` | Nothing exists for the given ID | Look up the ID with a search tool |
| `UNAUTHENTICATED` | API key missing, invalid, or expired | Ask the user to configure the key |
| `PERMISSION_DENIED` | Not permitted (e.g. API not applied for) | Inform the user |
| `RATE_LIMITED` | Quota or rate limit exceeded | Retry later |
| `UNAVAILABLE` | Upstream outage or timeout | Retry |
| `UPSTREAM_ERROR` | Other upstream failure | Report to the user |
| `INTERNAL` | Bug in mcp-hub | Report to the user |

- For APIs that return error codes inside an HTTP 200 response (OpenDART `status`, data.go.kr `resultCode`), the provider maps them to the codes above and puts the original code in `upstream_code`.
- Use `hint` for a concrete next step the model can take to fix the problem on its own.
- Error messages must never contain API keys or query strings. `httpx` trims URLs down to `scheme://host/path`.

## 7. HTTP calls

- Use `httpx.Client` instead of `http.Client` directly. It includes timeouts, retries on 429/502/503/504 (exponential backoff, honoring Retry-After), mapping of status codes to error codes, and key redaction.
- Cache data that changes rarely with `httpx.WithCache(ttl, max)` to save upstream quota.
- If the upstream reports errors inside HTTP 200 (OpenDART `status`, data.go.kr `resultCode`), add `httpx.WithCacheCheck` so those responses are not cached.
- If the upstream signals errors in a non-2xx response body (data.go.kr answers an unregistered key with HTTP 403), add `httpx.WithErrorBody` so the body decides the error code.
- data.go.kr APIs go through `providers/datagokr`'s shared client (`getList`). It requests XML, recognizes both error shapes, normalizes result codes, and returns `[]T` plus the total count. A tool only defines its item struct.
- Validate every input yourself, including formats (`YYYYMM`, month 1–12, region codes). Many upstream APIs, data.go.kr in particular, answer an invalid parameter with "success, zero results", which an agent cannot tell apart from "no data".
- Never ask users for administrative codes they would have to look up. `providers/datagokr/regions_data.go` is generated from the official legal-dong code list by `internal/genregions` (maintainers only; the key must be approved for 행정안전부_행정표준코드_법정동코드). Rerun `DATAGOKR_SERVICE_KEY=... go generate ./providers/datagokr` after administrative reorganizations, and add removed districts to `formerDistricts` in `regions.go`.
- When you find a new upstream quirk, save the real response in `testdata/`, cover it with a test, and add it to [QUIRKS.md](QUIRKS.md).
- If Go fails with `tls: handshake failure` while curl works, the server probably offers only RSA key exchange ciphers, which Go dropped from its defaults in 1.22. Check with `openssl s_client -connect host:443` and add `httpx.WithLegacyRSATLS()` (OpenDART needs it).

## 8. Tests

- Store upstream response samples in `testdata/` and replay them with `httptest`. Tests never call real APIs.
- Tool-level tests exercise the real MCP call path through `mcp.NewInMemoryTransports()` (see `hub/hub_test.go`).

## 9. Documentation

- Documents exist in English (`*.md`, the default) and Korean (`*.ko.md`). When changing one, update the other in the same change.
