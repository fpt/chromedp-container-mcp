# chromedp-container-mcp

A standalone, self-contained **headless-browsing sandbox** exposed as an
[MCP](https://modelcontextprotocol.io) server over the **SSE** transport.

The container bundles both halves:

- the **MCP server** (Go, based on the tool set from
  [KePatrick/chromedp-mcp](https://github.com/KePatrick/chromedp-mcp)), and
- a **headless Chrome** runtime (the
  [`chromedp/headless-shell`](https://hub.docker.com/r/chromedp/headless-shell/)
  base image).

Chrome is launched on demand by [`chromedp`](https://github.com/chromedp/chromedp)
inside the same container — there is nothing else to run.

Built against current dependencies: `mcp-go v0.55.1`, `chromedp v0.15.1`.

## Quick start

```bash
docker compose up --build
# SSE endpoint:  http://localhost:8080/sse
# message POST:  http://localhost:8080/message?sessionId=...
```

Or with plain Docker:

```bash
docker build -t chromedp-container-mcp:latest .
docker run --rm --init --shm-size 1g -p 8080:8080 \
  -e MCP_BASE_URL=http://localhost:8080 \
  chromedp-container-mcp:latest
```

`--init` reaps Chrome's child processes (PID 1 problem); `--shm-size 1g`
avoids Chrome crashing on the default 64 MB `/dev/shm`.

## Connecting a client

This server speaks the MCP **SSE** transport. A client opens `GET /sse`, reads
the `endpoint` event to learn its per-session message URL, then POSTs JSON-RPC
to that URL. Example client config:

```json
{
  "mcpServers": {
    "browser-sandbox": {
      "url": "http://localhost:8080/sse"
    }
  }
}
```

A typical session: call `create-chrome-instance` (returns an instance `id`),
pass that `id` to `navigate` / `click-element` / `screenshot` / etc., then
`close` when finished. Idle instances are reaped automatically after
`CHROME_TTL` minutes.

## Configuration

All configuration is via environment variables:

| Variable                  | Default                 | Description |
|---------------------------|-------------------------|-------------|
| `MCP_HOST`                | `0.0.0.0`               | Bind address |
| `MCP_PORT`                | `8080`                  | Listen port |
| `MCP_BASE_URL`            | `http://<host>:<port>`  | URL advertised to SSE clients in the `endpoint` event. Set to the externally reachable address when behind a reverse proxy or a remapped Docker port. |
| `CHROME_MAXIMUM_INSTANCE` | `5`                     | Max concurrent Chrome instances |
| `CHROME_TTL`              | `15`                    | Idle timeout (minutes) before an instance is reaped |
| `CHROME_EXE_TIMEOUT`      | `300`                   | Per-action timeout (seconds) |

> **Note:** when the host port differs from the container port (e.g.
> `-p 8765:8080`), set `MCP_BASE_URL` to the host-side URL so the message
> endpoint clients receive is reachable.

## Tools

`create-chrome-instance`, `close`, `navigate`, `navigate-back`,
`navigate-forward`, `get-element-withtext`, `get-all-elements`,
`select-element`, `click-element`, `send-key`, `set-value`, `key-event`,
`set-cookie`, `download-file`, `download_image`, `screenshot`, `generate_pdf`,
`tips`.

`create-chrome-instance` defaults to **container-safe** flags
(`headless`, `no-sandbox`, `disable-dev-shm-usage`, `disable-gpu` all on); each
can be overridden per call.

## Local development

```bash
go build ./cmd/server
MCP_PORT=8080 ./server      # requires a local Chrome/Chromium on PATH
```

The container is the supported runtime since it ships Chrome; running the bare
binary requires a Chromium-based browser installed locally.

## Layout

```
cmd/server/main.go   SSE MCP server wiring
tool/                MCP tool definitions + handlers (browser actions)
chromedp/manager.go  concurrency-safe Chrome instance manager (TTL, cleanup)
Dockerfile           multi-stage build on chromedp/headless-shell
docker-compose.yml   one-command run with --init + shm sizing
```

## Credits

Browser tool implementations are derived from
[KePatrick/chromedp-mcp](https://github.com/KePatrick/chromedp-mcp); this
project repackages them with an SSE transport, container-safe defaults, current
dependencies, and an all-in-one Chrome image.
