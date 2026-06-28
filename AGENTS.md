# AGENTS.md

Guidance for agents working in this repository. User-facing setup and usage live in `README.md`; repository-specific implementation notes also live in `CLAUDE.md`.

## Project

This repo builds `chromedp-container-mcp`, a Go MCP server that exposes a managed headless Chrome sandbox. The supported runtime is the Docker image based on `chromedp/headless-shell`; local binary runs require Chrome or Chromium on PATH.

## Layout

- `cmd/server/main.go` registers tools and serves MCP over stdio by default or SSE with `MCP_TRANSPORT=sse`.
- `chromedp/manager.go` owns Chrome instances, TTL cleanup, instance limits, and per-instance action serialization.
- `tool/*.go` defines one MCP tool per file.
- `.agents/skills/` contains project-local agent skills.
- `.claude/` contains Claude-specific agents and skills kept for Claude Code workflows.

## Commands

- `make build` builds the local binary.
- `make docker-build` builds `chromedp-container-mcp:latest`.
- `make docker-run` builds and runs the container.
- `make tidy` updates Go module metadata.
- `make vet` runs Go vet.

After server code changes, rebuild the image and reconnect the MCP server. Existing connected containers do not pick up a rebuild automatically.

## Browser-Sandbox Operation

Typical tool loop:

1. `create-chrome-instance`, keeping the returned `id`.
2. Use `navigate`, `screenshot`, selector tools, keyboard/mouse tools, and `wait` with that `id`.
3. Call `close` when finished.

Behind TLS-intercepting proxies, use `ignore-certificate-errors: true` on `create-chrome-instance` or set `CHROME_IGNORE_CERT_ERRORS=true` for the server. If the container has no direct egress, configure `CHROME_PROXY_SERVER`; Chrome does not honor `http_proxy` or `HTTPS_PROXY`.

For exploration tasks, use the project-local skill at `.agents/skills/browse-site-sandbox/SKILL.md`.

## Code Invariants

- Run shared Chrome instance actions through `Manager.Execute` or `Manager.ExecuteWithTimeout`.
- Do not call `chromedp.Run(instance.Context, ...)` directly from new handlers; it bypasses per-instance serialization.
- Keep stdio protocol output clean: logs must go to stderr, not stdout.

## Skill Conventions

Project-local reusable skills belong under `.agents/skills/<skill-name>/`.

Each skill should include:

- `SKILL.md` with `name` and `description` frontmatter.
- `agents/openai.yaml` when UI metadata is useful.

Do not add new project skills under `.claude/skills` unless the intent is specifically Claude Code integration.
