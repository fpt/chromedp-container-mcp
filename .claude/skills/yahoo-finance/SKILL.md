---
name: yahoo-finance
description: >-
  Fetch data from Yahoo!ファイナンス (finance.yahoo.co.jp) with the browser-sandbox
  MCP tools — news, individual stock quotes, market indicators (indices / FX /
  economic calendar), and rankings. Use when the user asks to read or scrape
  stock prices, indices, exchange rates, financial news, or ranking lists from
  Yahoo Finance Japan. Gives verified URL patterns, stable section anchors, and
  selectors per category, plus the page-stats → multi-extract → navigate-multiple
  workflow. (Unlike jp.reuters.com, this site is not bot-blocked and renders fine
  in the sandbox.)
---

# Fetch from Yahoo!ファイナンス (finance.yahoo.co.jp)

This site renders cleanly in the browser-sandbox (no DataDome/CAPTCHA, unlike
`jp.reuters.com`). It's a React SPA: the cleaned DOM from a bare `navigate` is
mostly empty containers, so **drive it with `page-stats` + `multi-extract`**, not
by eyeballing the DOM tree.

## Setup (same as every sandbox session)

1. `create-chrome-instance { ignore-certificate-errors: true }` — keep the `id`
   (Zscaler TLS workaround; see the `explore-site` skill).
2. `navigate { id, url: "https://finance.yahoo.co.jp/" }`.
3. `close { id }` when done.

## The workflow that works on this SPA

1. **`page-stats { id }`** → discover structure: section `ids` (the stable
   anchors) and repeated `classes` (a class with a high count is the repeated
   list item / value).
2. **`multi-extract { id, selectors: [...] }`** → pull every category in ONE
   call as name→matches lists. An empty list means the selector is wrong or the
   component is absent — you can tell which.
3. **`navigate-multiple { id, urls: [...] }`** → fetch several sub-pages
   (per-category pages, multiple `/quote/CODE`) in parallel.
4. **`multi-step`** → when you must drill into nested rows (container → row →
   cell/button) without a round-trip per level.

## Stability rule — anchor on IDs and URL patterns, NOT class names

React class names are build-hashed (`_StyledNumber__value_1arhg_9`,
`styles_CategoryRanking__item__jNwoo`) and **change on every site redeploy**. The
durable anchors are the **section `id`s** and the **`/quote/{CODE}` URL pattern**.
When you must use a hashed class, match a prefix:
`contains(@class,'_StyledNumber__value')`. Prefer: section id → structural XPath
(`dl/dt/dd`, `table/tbody/tr/td`, `ol/li`).

---

## 1. News 📰

- Top-page section: `#headline`. Dedicated page: `/news` (full list `/news/headline`).
- Headline + URL: `//section[@id='headline']//li//article/a` (text = headline+source, `href` = article URL)
- Timestamp: `//section[@id='headline']//time`
- Per-stock news: `/quote/{CODE}/news`

## 2. Individual stock (銘柄) 🏢

- URL pattern: `https://finance.yahoo.co.jp/quote/{CODE}` — e.g. `7203.T` (Toyota).
  Indices and FX use the same `/quote/` space (see §3).
- Name: `//main//h1`
- Last price: first `//*[contains(@class,'_StyledNumber__value')]` under the first
  `section`
- Change: `//main//section[1]//dl/dd` (e.g. `+67.5(+2.50%)`)
- OHLC / 前日終値・始値・高値・安値…: `#detail` section's `dt`/`dd` pairs
- Sub-nav tabs: `#stk_info` → `/quote/{CODE}/{chart,news,forum,disclosure,ai-topics}`

## 3. Indicators — indices / FX / economic calendar 📊

- Index ticker bar: `#marquee` → `dt/a` = name + link, `dd//*[contains(@class,'_StyledNumber__value')]` = value
- Index quote URLs: 日経=`/quote/998407.O`, TOPIX=`/quote/998405.T`,
  NYダウ=`/quote/%5EDJI`, USD/JPY=`/quote/USDJPY=X`
- FX detail block: `#fx` → `//*[@id='fx']//li//header/a` (pair name + `/quote/USDJPY=FX`)
- Economic-indicator calendar: `#mktclndr` → `table/tbody/tr/td` (time・importance・name / forecast / result)

## 4. Rankings 🏆

| Kind | Top-page anchor | Selector | Dedicated page |
|---|---|---|---|
| 注目株 (hot) | `#hotrnk` | **table**: `//*[@id='hotrnk']//table/tbody/tr/td[2]/a` | — |
| 株ランキング | `#stockrank` | `//*[@id='stockrank']//ol/li/a/span[2]` | `/stocks/ranking` |
| 投信ランキング | `#fundrnk` | `//*[@id='fundrnk']//ol/li/a/span[2]` | `/funds` |

⚠️ 注目株ランキングの 1〜4位は有料会員限定でマスク表示（5位のみ公開）。

## Category pages (via global nav, for深掘り)

`//*[contains(@class,'_GlobalNav__item')]//a` →
日本株 `/stocks` · 米国株 `/stocks/us` · FX・為替 `/fx/` · 投資信託 `/funds` ·
ニュース `/news`. Pass several of these to `navigate-multiple` to grab them in parallel.

---

## One-shot example: full dashboard from the top page

```
create-chrome-instance { ignore-certificate-errors: true } -> id
navigate { id, url: "https://finance.yahoo.co.jp/" }
multi-extract { id, max_per_selector: 8, selectors: [
  { name: "news",        selector: "//section[@id='headline']//li//article/a" },
  { name: "news_time",   selector: "//section[@id='headline']//time" },
  { name: "indices",     selector: "//*[@id='marquee']//dt/a" },
  { name: "index_values",selector: "//*[@id='marquee']//dd//*[contains(@class,'_StyledNumber__value')]" },
  { name: "fx",          selector: "//*[@id='fx']//li//header/a" },
  { name: "rank_stock",  selector: "//*[@id='stockrank']//ol/li/a/span[2]" },
  { name: "rank_fund",   selector: "//*[@id='fundrnk']//ol/li/a/span[2]" },
  { name: "rank_hot",    selector: "//*[@id='hotrnk']//table/tbody/tr/td[2]/a" },
  { name: "econ_cal",    selector: "//*[@id='mktclndr']//table/tbody/tr" }
]}
close { id }
```

To read individual stocks afterwards: `navigate-multiple { id, urls: ["…/quote/7203.T", "…/quote/6758.T", …] }`,
or `navigate { id, url: "…/quote/7203.T", selector: "//main", depth: 6 }` for one.

## If a selector returns empty

Re-run `page-stats` (optionally scoped: `page-stats { id, selector: "#hotrnk" }`)
to see the actual tag/class layout of that section — Yahoo mixes `ol/li`, `table`,
and `dl` across modules (e.g. rankings are `ol/li` but 注目株 is a `table`).
