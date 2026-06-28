# chromedp-container-mcp

A standalone, self-contained **headless-browsing sandbox** exposed as an
[MCP](https://modelcontextprotocol.io) server. It speaks **stdio** by default
(launch the container as a subprocess) and can also serve an **SSE** HTTP
endpoint.

The container bundles both halves:

- the **MCP server** (Go, based on the tool set from
  [KePatrick/chromedp-mcp](https://github.com/KePatrick/chromedp-mcp)), and
- a **headless Chrome** runtime (the
  [`chromedp/headless-shell`](https://hub.docker.com/r/chromedp/headless-shell/)
  base image).

Chrome is launched on demand by [`chromedp`](https://github.com/chromedp/chromedp)
inside the same container — there is nothing else to run.

Built against current dependencies: `mcp-go v0.55.1`, `chromedp v0.15.1`.

## Quick start (stdio — default)

Build the image, then point an MCP client at it. The client launches the
container per session and talks JSON-RPC over stdin/stdout:

```bash
docker build -t chromedp-container-mcp:latest .
```

With the [Claude Code](https://docs.claude.com/en/docs/claude-code) CLI
(everything after `--` is the command it launches per session):

```bash
claude mcp add browser-sandbox -- \
  docker run -i --rm --init --shm-size 1g chromedp-container-mcp:latest
```

Or by hand in any MCP client config:

```json
{
  "mcpServers": {
    "browser-sandbox": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm", "--init", "--shm-size", "1g",
        "chromedp-container-mcp:latest"
      ]
    }
  }
}
```

`-i` keeps stdin open for the protocol; `--init` reaps Chrome's child processes
(PID 1 problem); `--shm-size 1g` avoids Chrome crashing on the default 64 MB
`/dev/shm`. In stdio mode no port is published and `MCP_HOST` / `MCP_PORT` /
`MCP_BASE_URL` are ignored; logs go to stderr so they don't corrupt the
protocol stream.

A typical session: call `create-chrome-instance` (returns an instance `id`),
pass that `id` to `navigate` / `click-element` / `screenshot` / etc., then
`close` when finished. Idle instances are reaped automatically after
`CHROME_TTL` minutes.

## SSE transport

To run a long-lived HTTP/SSE endpoint instead, set `MCP_TRANSPORT=sse`:

```bash
make certs.pem        # required once: creates the build-secret file (empty if no corporate CA)
docker compose up --build
# SSE endpoint:  http://localhost:8080/sse
# message POST:  http://localhost:8080/message?sessionId=...
```

> Compose mounts `./certs.pem` as a build secret, so the file must exist — run
> `make certs.pem` first (it writes an empty file when you have no corporate CA).
> Behind a TLS-intercepting proxy, enable the proxy options inline, e.g.
> `CHROME_IGNORE_CERT_ERRORS=true CHROME_PROXY_SERVER=http://proxy:3128 docker compose up --build`.

Or with plain Docker:

```bash
docker run --rm --init --shm-size 1g -p 8080:8080 \
  -e MCP_TRANSPORT=sse -e MCP_BASE_URL=http://localhost:8080 \
  chromedp-container-mcp:latest
```

Register the running endpoint with the Claude Code CLI:

```bash
claude mcp add --transport sse browser-sandbox http://localhost:8080/sse
```

A client opens `GET /sse`, reads the `endpoint` event to learn its per-session
message URL, then POSTs JSON-RPC to that URL. Equivalent manual config:

```json
{
  "mcpServers": {
    "browser-sandbox": {
      "url": "http://localhost:8080/sse"
    }
  }
}
```

## Running with Apple `container`

The image is OCI-compatible, so it also runs on [Apple's `container`](https://github.com/apple/container)
(macOS 26+ on Apple silicon) — Linux containers as lightweight VMs. Verified
with `container` 1.0. Start the service once with `container system start`, then:

```bash
# Build (the CA build secret is optional; omit it if you have no corporate CA)
container build -t chromedp-container-mcp:latest .

# stdio (default) — register with the Claude Code CLI
claude mcp add browser-sandbox -- \
  container run -i --rm --shm-size 1g chromedp-container-mcp:latest

# SSE
container run --rm -p 8080:8080 \
  -e MCP_TRANSPORT=sse -e MCP_BASE_URL=http://localhost:8080 \
  chromedp-container-mcp:latest
```

Differences from Docker to keep in mind:

- **No `--init` flag.** The server runs as PID 1 inside the container, the same
  as `docker run` *without* `--init`. For per-session stdio use this is a
  non-issue; for very long-lived SSE deployments prefer Docker with `--init` if
  automatic zombie reaping matters.
- **Build secret:** `container build` supports `--secret id=ca_cert,src=certs.pem`
  just like Docker, but the secret is optional here too (the Dockerfile skips the
  CA install when it's absent).
- **IPv6:** the guest VM may have no IPv6 egress; headless Chrome falls back to
  IPv4 automatically, so navigation to public sites works (verified).
- `--shm-size` is supported (same `/dev/shm` guidance as Docker).

## Running with bubblewrap (Linux, no container runtime)

On Linux you can sandbox the server + headless Chrome with
[bubblewrap](https://github.com/containers/bubblewrap) instead of a container
runtime. bubblewrap doesn't run OCI images, so the approach is to materialize the
image's filesystem to a rootfs once, then `bwrap` into it. Chrome runs
`--no-sandbox` (the default) — **bubblewrap is the sandbox**. Verified end to end
(create instance, navigate, screenshot).

```bash
docker build -t chromedp-container-mcp:latest .   # or build the rootfs any way you like
./scripts/run-bwrap.sh                             # exports rootfs once, then runs under bwrap
```

`scripts/run-bwrap.sh` exports `./bwrap-rootfs` on first run (via `docker`/`podman`,
used only for the export) and then launches:

```bash
bwrap --ro-bind ./bwrap-rootfs / \
  --tmpfs /tmp --tmpfs /dev/shm --proc /proc --dev /dev \
  --ro-bind-try /etc/resolv.conf /etc/resolv.conf \
  --unshare-user-try --unshare-pid --unshare-ipc --unshare-uts \
  --die-with-parent --new-session \
  --setenv PATH /headless-shell:/usr/local/bin:/usr/bin:/bin \
  --setenv HOME /tmp --setenv MCP_TRANSPORT stdio \
  /usr/local/bin/chromedp-container-mcp
```

Register it with the Claude Code CLI (pre-export the rootfs first so startup is fast):

```bash
claude mcp add browser-sandbox -- /abs/path/to/scripts/run-bwrap.sh
```

Notes:

- **Requires** `bwrap` plus either a setuid bwrap (the usual distro package) or
  unprivileged user namespaces enabled (`kernel.unprivileged_userns_clone=1` /
  non-zero `user.max_user_namespaces`).
- The **network namespace is shared with the host**, so in SSE mode the server
  listens directly on the host's interface — no port mapping needed.
- The server runs as PID 1 inside the sandbox; like Docker without `--init`,
  there's no automatic zombie reaping (fine for per-session stdio use).

## Configuration

All configuration is via environment variables:

| Variable                  | Default                 | Description |
|---------------------------|-------------------------|-------------|
| `MCP_TRANSPORT`           | `stdio`                 | `stdio` (subprocess over stdin/stdout) or `sse` (HTTP endpoint) |
| `MCP_HOST`                | `0.0.0.0`               | Bind address (SSE only) |
| `MCP_PORT`                | `8080`                  | Listen port (SSE only) |
| `MCP_BASE_URL`            | `http://<host>:<port>`  | SSE only. URL advertised to clients in the `endpoint` event. Set to the externally reachable address when behind a reverse proxy or a remapped Docker port. |
| `CHROME_MAXIMUM_INSTANCE` | `5`                     | Max concurrent Chrome instances |
| `CHROME_TTL`              | `15`                    | Idle timeout (minutes) before an instance is reaped |
| `CHROME_EXE_TIMEOUT`      | `300`                   | Per-action timeout (seconds) |
| `CHROME_IGNORE_CERT_ERRORS` | `false`               | Default for the `ignore-certificate-errors` instance flag. Set to `true` behind a TLS-intercepting corporate proxy, where Chrome otherwise fails navigation with `ERR_CERT_AUTHORITY_INVALID`. |
| `CHROME_PROXY_SERVER`     | _(empty)_               | Default for the `proxy-server` instance flag. Set when the container can only reach the internet through a corporate proxy, e.g. `http://proxy.host:3128`. Chrome ignores `http_proxy`/`HTTPS_PROXY`, so this maps to its `--proxy-server` launch flag. |

> **Behind a corporate proxy:** the proxy substitutes its own TLS
> certificate, which the in-container Chrome does not trust, so `navigate` fails
> with `ERR_CERT_AUTHORITY_INVALID`. Either run with `-e CHROME_IGNORE_CERT_ERRORS=true`
> (or set it in `docker-compose.yml`), or pass `ignore-certificate-errors: true` to a
> single `create-chrome-instance` call. This disables TLS verification for that
> browser, which is acceptable for a browsing sandbox but should stay off on
> networks without TLS interception.
>
> If the container has **no direct internet egress** and must go through the
> proxy, also set `CHROME_PROXY_SERVER` (e.g. `http://proxy.host:3128`) or pass
> `proxy-server` to `create-chrome-instance` — Chrome does not honor
> `http_proxy`/`HTTPS_PROXY` and needs the `--proxy-server` flag.

> **Note:** when the host port differs from the container port (e.g.
> `-p 8765:8080`), set `MCP_BASE_URL` to the host-side URL so the message
> endpoint clients receive is reachable.

## Tools

**Selector-based** (drive the page via CSS/XPath selectors and the DOM tree):
`create-chrome-instance`, `close`, `navigate`, `navigate-back`,
`navigate-forward`, `navigate-multiple`, `multi-extract`, `page-stats`,
`get-element-withtext`, `get-all-elements`, `select-element`, `click-element`,
`multi-step`, `send-key`, `set-value`, `key-event`, `set-cookie`,
`download-file`, `download_image`, `screenshot`, `tips`.

`multi-extract` pulls several things off the current page in one call. You pass
name/selector pairs (XPath or CSS); each selector matches 0..n elements and the
result maps **every name to a list** of matches (text, an XPath you can act on,
plus href/value/select-options where relevant). An **empty list means that
selector matched nothing** — so the agent can tell "this component is absent / my
pattern is wrong" from "present but empty", instead of a fixed report implying
there's nothing there. Invalid selectors are reported under a separate `errors`
map. Omit `selectors` to run a default probe of common components, whose keys are
prefixed with `-` (e.g. `-title` → `//title`, `-h1`, `-nav`, `-search`,
`-breadcrumb`, `-pagination`, `-buttons`, `-forms`, `-tabs`).

`page-stats` summarizes the page's **structure** rather than its content:
histograms of element tags and CSS class tokens (with counts), the list of ids,
ARIA roles, `data-*` attribute names, an input-type breakdown, counts of common
components, and total element count / max DOM depth. Use it to discover what
selectors to write — a class that appears 24 times is probably the repeated list
item. Pass an optional `selector` to gather stats only within one element.

The DOM-returning tools (`navigate`, `navigate-multiple`, `get-all-elements`,
`select-element`) take a `depth` limit. When the result is cut off at that
limit, the output is **prefixed with a `⚠` warning naming the depth** so you
notice content is hidden (not absent) and can re-run with a larger `depth`,
rather than being silently truncated.

`navigate-multiple` fetches several URLs **in parallel**, each in its own tab of
the instance, applies the same DOM-cleaning filter as `navigate` to all of them,
and returns the cleaned trees side by side — handy for exploring and comparing
sub-pages in one call. The tabs are opened and closed automatically and the
instance's main tab is left untouched (`max_concurrency` and per-page `timeout`
are tunable).

`multi-step` runs an ordered sequence of **scoped** steps in one call: each
step's selector is matched *within* the element selected by the previous step, so
the scope narrows as you go (e.g. select a container → select a row inside it →
click a button inside that row). Step actions are `select`, `click`, `set-value`,
and `extract`. If a step matches nothing the call fails and reports the 1-based
step number — letting an agent drill into and act on nested structure without a
round-trip per step. Scoped XPath should be relative (`.//…`); a leading `//` is
auto-scoped to the current element.

**Computer-use** (coordinate / screenshot driven — for agents that read a
screenshot and act on pixels, à la
[OpenAI's computer-use](https://developers.openai.com/api/docs/guides/tools-computer-use)
and Anthropic computer-use loops): `mouse-click`, `mouse-move`, `mouse-drag`,
`scroll`, `type-text`, `press-keys`, `wait`.

The loop is: `screenshot` → inspect → act with a coordinate tool → `screenshot`
again. To make pixels line up with click coordinates:

- The viewport is **fixed** at instance creation (`viewport-width` /
  `viewport-height`, default **1280×800**) and runs at devicePixelRatio 1.
- `screenshot` defaults to the **visible viewport** (not the full page) and its
  result description reports the image size in pixels — that is your coordinate
  space. Pass `full_page: true` for the whole scrollable page.
- Coordinates are CSS pixels from the top-left of the viewport.

`create-chrome-instance` defaults to **container-safe** flags
(`headless`, `no-sandbox`, `disable-dev-shm-usage`, `disable-gpu` all on); each
can be overridden per call. Concurrent tool calls against the same instance are
serialized internally, so a browser instance is safe to share within a session.

**Diagnostics**: `system-stats`, `network-check`.

`system-stats` reports resource usage: total memory and process count of the
headless Chrome tree (read from `/proc`), the MCP server's own RSS / Go heap /
goroutines, and the managed instances (active vs. `CHROME_MAXIMUM_INSTANCE`, with
each instance's idle time and TTL). Use it to check headroom before creating more
instances or to debug memory pressure.

`network-check` diagnoses outbound connectivity from inside the container: the
DNS servers, how a `host` resolves (IPv4 vs IPv6 addresses), and whether IPv4 and
IPv6 egress actually work (TCP connect to public resolvers on `:443`), plus an
optional HTTP(S) `url` probe (which honors `CHROME_IGNORE_CERT_ERRORS` so it
mirrors the browser). It also emits a hint for the common failure mode — a host
that resolves to IPv6 while the VM has no IPv6 route. Reach for it when
navigation fails, hangs, or errors.

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
