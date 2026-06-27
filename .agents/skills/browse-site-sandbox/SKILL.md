---
name: browse-site-sandbox
description: Explore, inspect, screenshot, search, and summarize websites with the browser-sandbox MCP tools. Use when Codex needs to browse a URL in a real Chrome session, verify rendered page state, interact with dynamic pages, extract DOM text, inspect product/listing/detail flows, or report concrete site structure and findings while keeping browser sessions closed cleanly.
---

# Browse Site Sandbox

Use this skill to drive `browser-sandbox` end to end: create a Chrome instance, navigate, inspect DOM text, capture screenshots, interact with controls, and return a concise report.

## Tool Discovery

Load browser-sandbox tools with `tool_search` before use. Search for terms such as:

```text
browser-sandbox create chrome instance navigate screenshot get element click scroll close
```

Prefer these tools when available:

- `create_chrome_instance`
- `navigate`
- `screenshot`
- `get_element_withtext`
- `select_element`
- `click_element`
- `send_key`, `type_text`, `key_event`
- `scroll`, `wait`
- `navigate_back`, `navigate_forward`
- `close`

If a call fails with `chrome instance not found`, create a new instance and retry. Keep the returned `id` and pass it to every browser action.

## Core Workflow

1. Create Chrome:

```json
{"headless": true, "viewport-width": 1365, "viewport-height": 900, "ignore-certificate-errors": true}
```

Use `ignore-certificate-errors` when available. It avoids TLS interception failures in corporate proxy environments.

2. Navigate to the URL with moderate DOM depth, usually `depth: 4` or `depth: 5`.

3. Take a viewport screenshot immediately after navigation. Use it to verify that the page rendered, spot consent/login walls, and decide what to click next.

4. Extract targeted text from meaningful containers instead of dumping the whole page:

- `header` for nav, search, login, cart, promos
- `main`, article, product detail, listing grid, search results, FAQ, footer
- Site-specific selectors discovered from `navigate` or `select_element`

5. Interact with the page when needed:

- Type into search inputs with `send_key` or click and `type_text`.
- Submit forms with `click_element` or `key_event` Enter.
- Use `wait { "seconds": 2-3 }` after AJAX, navigation, or lazy rendering.
- Use `scroll` for lazy-loaded lists and then re-read the relevant container.

6. Open one or two representative detail pages when exploring commerce, listings, docs, or catalogs. Capture both page-level structure and concrete fields such as title, price, SKU, stock, date, author, category, or CTA state.

7. Always call `close` with the Chrome instance id before finishing, including after partial failures.

## Reading Strategy

Use screenshots for layout and visual state, but use DOM text extraction for accurate labels, numbers, and non-Latin text. Headless Chrome screenshots may miss fonts or show missing glyph boxes even when the DOM text is correct.

Avoid broad reads that flood context. If an output is too large:

- Narrow the selector and retry.
- Read separate sections one at a time.
- If the tool saves oversized output to a file, summarize the file in chunks before reporting. Do not paste the full dump into the conversation.

## Report Format

Return concrete findings, not a transcript of tool use.

Include:

- What the site/page is.
- The key navigation and search paths.
- Main content sections and interaction patterns.
- Representative extracted records or product/detail fields with exact values.
- Any rendering, login, consent, network, or font caveats.
- Whether the browser session was closed.

For ecommerce/catalog sites, check at least:

- Homepage/header/navigation.
- Search or category listing.
- One product/detail page.
- Availability/price/action states when visible.

## Example Loop

```text
tool_search("browser-sandbox create chrome instance navigate screenshot get element click close")
create_chrome_instance(...)
navigate(id, url, depth: 5)
screenshot(id)
get_element_withtext(id, "header")
send_key(id, "input[name=\"q\"]", "raspberry pi")
click_element(id, "button[type=\"submit\"]")
wait(seconds: 2)
get_element_withtext(id, ".productgrid--items")
navigate(id, first_detail_url, depth: 5)
get_element_withtext(id, ".product-main")
screenshot(id)
close(id)
report concise findings
```
