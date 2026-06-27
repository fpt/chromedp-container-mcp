---
name: explore-site
description: >-
  Explore / read / scrape a website with the browser-sandbox MCP tools (this
  project's chromedp container) and report what's there. Use when the user asks
  to open, browse, explore, screenshot, search, or extract content from a URL
  via the sandbox. Covers the Zscaler TLS workaround, the create→navigate→
  interact→close tool flow, driving dynamic/AJAX pages, and offloading oversized
  tool outputs to a summarizer subagent so the page content never floods the
  main context.
---

# Explore a site with the browser-sandbox

The `browser-sandbox` MCP server is **this project's container**
(`chromedp-container-mcp:latest`) driving an in-container headless Chrome. Its
tools are deferred — load them with ToolSearch before calling, e.g.
`select:mcp__browser-sandbox__create-chrome-instance,...navigate,...screenshot,...get-all-elements,...get-element-withtext,...click-element,...scroll,...wait,...close`.

## Two ways to run this — pick by how much you need to steer

- **Pattern B — delegate the whole browse (preferred when you just want the
  result).** Spawn the **`site-explorer`** agent (declares
  `mcpServers: [browser-sandbox]`) with the URL and exactly what to extract. It
  runs the entire loop below in its own context and returns only a summary, so
  no DOM dumps or screenshots ever land in the main conversation. Best for
  "go read X and tell me what's there."
- **Pattern A — drive it yourself inline (when you need to see/steer each step).**
  Run the loop below from the main agent, and when a single read is too big to
  inline, hand that saved file to the **`content-summarizer`** agent (§3). Best
  when you want to inspect screenshots, decide the next click interactively, or
  show the user intermediate state.

The rest of this skill is the loop both patterns use.

## 1. Always start Chrome with the Zscaler workaround

We are behind Zscaler, which intercepts TLS. The container Chrome does not trust
the corporate CA, so navigation fails with `ERR_CERT_AUTHORITY_INVALID` unless
you defeat cert verification. **Create every instance with
`ignore-certificate-errors: true`:**

```
create-chrome-instance { headless: true, ignore-certificate-errors: true }
```

(Equivalently, the server can run with `CHROME_IGNORE_CERT_ERRORS=true` — set in
`docker-compose.yml` and documented in the README — which makes that the default.
If a `navigate` ever returns `ERR_CERT_AUTHORITY_INVALID`, the running server is
an old image without the flag: it must be rebuilt (`make docker-build`) and the
MCP server reconnected.)

## 2. The core loop

1. `create-chrome-instance` → keep the returned `id` for every later call.
2. `navigate { url, id }` → returns a trimmed DOM tree. Note dynamic regions
   (empty `#result-list`-style containers populate via JS after an action).
3. `screenshot { id }` → confirm what actually rendered (catches blank pages,
   consent walls, cert interstitials).
4. Interact for dynamic content: `click-element` a search/expand control, then
   `wait { seconds: 3 }` for AJAX, then re-read with `get-all-elements` or a
   targeted `get-element-withtext { selector }`. Use `scroll` for lazy lists.
5. `close { id }` when done — always, even on error. Instances also auto-reap
   after the idle TTL, but close explicitly.

## 3. When a tool output is too big — delegate, don't inline

`get-all-elements` / `get-element-withtext` on a content-heavy page can exceed
the token limit. The tool then **saves the full output to a file** and returns a
path instead of the content. Do **not** read that whole file into your own
context — hand it to the **`content-summarizer`** subagent (defined in
`.claude/agents/`):

- Spawn it with the Agent tool.
- Give it the **file path**, tell it to **read the entire file in ~380-line
  chunks via Read offset/limit until 100% is read**, and specify **exactly what
  to return** (count, the per-record schema, N diverse samples with verbatim
  values, and cross-cutting patterns). Vague "summarize this" loses detail.
- Its final message is the only thing that comes back — the raw DOM stays out of
  your context. You relay the structured summary to the user.

This "spawn a subagent to read a saved file and return only the summary" pattern
is the default for any oversized capture (DOM dumps, big WebFetch results, logs).

## 4. Fallback when the sandbox can't be used

If the sandbox is disconnected or blocked, `WebFetch` reaches the page through a
different network path (outside the proxy) and answers a prompt against it; for
binary/PDF links it saves the file, which you can then `Read` directly. Use this
to keep delivering content while the sandbox is unavailable.

## Example: explore + search a listing page

```
load tools (ToolSearch)
create-chrome-instance { ignore-certificate-errors: true } -> id
navigate { id, url }
screenshot { id }                       # see the layout
click-element { id, selector: "#search-button" }
wait { seconds: 3 }                     # let results AJAX in
get-element-withtext { id, selector: "#result-list" }
  -> "output exceeds maximum allowed tokens ... saved to /…/result.txt"
Agent(content-summarizer): "Read /…/result.txt in ~380-line chunks until 100%
  read. Return: total count; the fields each listing exposes; 10 diverse
  listings with verbatim values; patterns across all listings."
close { id }
relay the subagent's summary to the user
```
