# CLAUDE.md

Guidance for working in this repo. User-facing usage lives in `README.md`; this
file covers architecture invariants and the non-obvious operational details.

## What this is

An MCP server that exposes a headless Chrome "browser sandbox" as tools
(navigate, click, screenshot, search the DOM, cookies, PDF, computer-use mouse
primitives). Module `chromedp-container-mcp`, Go 1.26. It ships as a container
based on `chromedp/headless-shell`, and is registered as an MCP server via
`docker run -i ... chromedp-container-mcp:latest` (see README).

## Layout

- `cmd/server/main.go` — entry point. Reads env config, builds the
  `ChromeManager`, registers every tool, then serves over stdio (default) or SSE
  (`MCP_TRANSPORT=sse`).
- `chromedp/manager.go` — `ChromeManager`: a package-level singleton (`Manager`)
  that owns all Chrome instances, their TTL reaping, and the concurrency locks.
- `tool/*.go` — one file per MCP tool: `NewXxxTool()` returns the schema,
  `XxxHandler()` is the handler. Registered in `main.go`.

## Build & run

- `make build` — local binary (needs Chrome on PATH to actually run).
- `make docker-build` — builds the image. Depends on the `certs.pem` target and
  passes `--secret id=ca_cert,src=certs.pem` (corporate-proxy CA; see below).
- `make docker-run` — build + run.
- `make tidy` / `make vet`.

After changing server code, the image must be **rebuilt** *and* the MCP server
**reconnected** for a live session to pick it up — the running server is a
long-lived container started at connect time; a rebuild alone does nothing to it.

## Behind a corporate proxy — two separate layers

We are behind a corporate proxy, which intercepts TLS. This breaks two different things:

1. **Build-time** (`go mod download` reaching proxy.golang.org). The build stage
   installs a CA via a BuildKit secret: `make certs.pem` copies
   `~/.corp-ca/certs.pem`, and the Dockerfile does
   `--mount=type=secret,id=ca_cert ... update-ca-certificates` (skipped if the
   secret is absent, e.g. CI). Never bake the cert into a layer or commit it
   (`certs.pem` is gitignored).
2. **Runtime** (the in-container Chrome navigating HTTPS). Chrome doesn't trust
   the intercepted cert and fails with `ERR_CERT_AUTHORITY_INVALID`. Fix:
   `create-chrome-instance` takes `ignore-certificate-errors`, defaulting from
   the `CHROME_IGNORE_CERT_ERRORS` env var (off by default). Enable it by running
   the server with `-e CHROME_IGNORE_CERT_ERRORS=true`, or pass the param per
   call. This disables TLS verification for that browser — acceptable for a
   browsing sandbox, leave off on non-intercepted networks. (Importing the CA
   into the runtime image isn't viable: headless-shell ships no
   certutil/ca-certificates and Chromium uses NSS, not the system bundle.)

## Concurrency model — the key invariant

`ChromeManager` is built for multiple concurrent instances and is the source of
truth for safety:

- Instance map is `RWMutex`-guarded. Instance count is capped at
  `CHROME_MAXIMUM_INSTANCE` (default 5); the next `create-chrome-instance`
  returns a clean error, not a crash.
- Each `ChromeInstance` has a `runMu` mutex. A chromedp context runs **one**
  action sequence at a time, so concurrent actions on the same instance must
  serialize.
- **Invariant for new tool handlers: run Chrome actions through
  `Manager.Execute(id, actions...)` or `Manager.ExecuteWithTimeout(...)`** —
  these acquire `runMu` and apply the execute timeout. Do **not** call
  `chromedp.Run(instance.Context, ...)` directly; that bypasses the lock and can
  race a concurrent action on the same instance. (`cookie.go` had this bug and
  was fixed; `pdf.go` is exempt because it builds its own throwaway context and
  touches no shared instance.)
- Transport: the stdio server (mcp-go) dispatches `tools/call` via a worker pool
  (default size 5). So practical parallelism ≈ min(5 workers, 5 instances). To
  scale: raise `CHROME_MAXIMUM_INSTANCE` (env) **and** add
  `server.WithWorkerPoolSize(n)` in `main.go` (code) — both, or the worker pool
  bottlenecks at 5.

## Config (env)

`MCP_TRANSPORT` (stdio|sse), `MCP_HOST`, `MCP_PORT`, `MCP_BASE_URL`,
`CHROME_MAXIMUM_INSTANCE` (5), `CHROME_TTL` (idle minutes, 15),
`CHROME_EXE_TIMEOUT` (per-action seconds, 300), `CHROME_IGNORE_CERT_ERRORS`
(false). Full table in README.

## Gotchas

- `pdf.go` builds its own ephemeral Chrome and **ignores** the
  `ignore-certificate-errors` flag and the managed instance — so `generate_pdf`
  from a URL fails behind a TLS-intercepting proxy and won't share cookies/session with an
  explored instance. Known, unfixed.
- The `chromedp/headless-shell` image runs Chrome as a remote-debugging server;
  its one-shot CLI modes (`--dump-dom`, `--screenshot`) don't reliably emit
  output. To probe cert/navigation at the Chrome level, drive it over the
  DevTools HTTP endpoint (`/json/version`, `/json/list`) on the published port,
  not those flags.
- Logs go to **stderr** in stdio mode (stdout is the JSON-RPC stream — never
  print to it).

## Subagents & skills (`.claude/`)

- `agents/site-explorer.md` — drives the browser-sandbox MCP end-to-end and
  returns only a summary (declares `mcpServers: [browser-sandbox]`); use to keep
  page bulk out of the main context.
- `agents/content-summarizer.md` — reads an oversized saved tool output (file)
  in chunks and returns a structured summary; no MCP.
- `skills/explore-site/SKILL.md` — the explore workflow and when to use each
  agent.
