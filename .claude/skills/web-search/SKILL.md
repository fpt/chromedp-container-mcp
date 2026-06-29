---
name: web-search
description: >-
  Run a web search from inside the browser-sandbox. Use when you need search
  results via the sandbox — e.g. to then scrape the linked pages in the same
  Chrome instance. Drive Yahoo!検索 first (search.yahoo.co.jp — no bot wall,
  real result URLs); fall back to Bing if Yahoo fails. Google is NOT usable from
  the sandbox (its datacenter IP hits a reCAPTCHA /sorry wall). For a plain
  "just search / fetch a page" with no in-sandbox scraping, prefer the harness
  WebSearch / WebFetch tools — different network path, no Chrome instance needed.
---

# Web search via the sandbox

## Pick the right tool first

- **Just need results, or a page's text?** Use the harness **`WebSearch` /
  `WebFetch`** — different network path, not bot-blocked, no Chrome instance.
  Simplest. Use the sandbox only when you must keep driving the result pages in
  the same browser (click through, scrape, screenshot).
- **In-sandbox search?** Use the engine ladder below.

## Engine ladder (verified)

| Engine | Sandbox? | Result href | Notes |
|---|---|---|---|
| **Yahoo!検索** | ✅ first choice | **real URL, direct** | no bot wall; cleanest |
| Bing | ✅ fallback | wrapped redirector (decode) | use if Yahoo fails |
| Google | 🚫 unusable | — | redirects to `/sorry` reCAPTCHA wall (`#captcha-form`); datacenter IP blocked, same class as Reuters DataDome |

All engines: create the instance with `ignore-certificate-errors: true` (Zscaler
TLS workaround), and `close` when done. URL-encode the query (spaces → `+` or `%20`).

## 1. Yahoo!検索 (first choice)

```
create-chrome-instance { ignore-certificate-errors: true } -> id
navigate { id, url: "https://search.yahoo.co.jp/search?p=<QUERY>" }
multi-extract { id, max_per_selector: 8, selectors: [
  { name: "links",    selector: "//*[@id='contents__wrap']//section//a[.//h3]" },  // href = REAL url
  { name: "titles",   selector: "//*[@id='contents__wrap']//section//a[.//h3]//h3" },
  { name: "snippets", selector: "//*[@id='contents__wrap']//section//p" }
]}
close { id }
```

- `links` hrefs are the actual destinations — no decoding needed.
- Blocks whose id starts with `aria-sw-Accordion__cnt…` are "他の人はこちらも質問"
  (People-Also-Ask), not organic results — filter them out if you only want organic.

## 2. Bing (fallback)

```
navigate { id, url: "https://www.bing.com/search?q=<QUERY>&setlang=ja" }
multi-extract { id, max_per_selector: 8, selectors: [
  { name: "titles",   selector: "//li[contains(@class,'b_algo')]//h2/a" },
  { name: "snippets", selector: "//li[contains(@class,'b_algo')]//p" }
]}
```

⚠️ Bing wraps every href in a redirector `https://www.bing.com/ck/a?…&u=a1<base64>&ntb=1`.
The real URL is the `u=` value with its leading `a1` stripped, then base64-decoded
(URL-safe base64; pad if needed):

```
u=a1aHR0cHM6Ly9jb3JwLm1vbm90YXJvLmNvbS9vdmVydmlldy5odG1s
   -> strip "a1" -> base64 -d -> https://corp.monotaro.com/overview.html
```

## If results come back empty

`page-stats { id }` to confirm the layout (an engine may A/B-test class names or
show a consent/cookie interstitial; `screenshot { id }` to see a wall). Then try
the next engine down the ladder, or fall back to the harness `WebSearch`.

## Verified

Query `monotaro 企業情報`: Yahoo!検索 returned organic results with direct hrefs
(corp.monotaro.com/overview.html, nikkei.com/nkd/company/gaiyo/?scode=3064,
ja.wikipedia.org/wiki/MonotaRO); Bing returned the same targets behind `/ck/a`
redirectors; Google redirected to the reCAPTCHA `/sorry` wall.
