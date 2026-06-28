package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewPageStatsTool() mcp.Tool {
	return mcp.NewTool("page-stats",
		mcp.WithDescription("Summarize the STRUCTURE of the current page (not its content): histograms of element tag names and CSS class tokens (with counts), the list of element ids, ARIA roles, data-* attribute names, an input-type breakdown, counts of common components (links, buttons, inputs, forms, images, headings, lists, tables, iframes), plus total element count and max DOM depth. Use it to discover what selectors to use — e.g. a class that appears 24 times is probably the repeated list item. Optionally pass `selector` (XPath or CSS) to gather stats only within that element."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance id")),
		mcp.WithString("selector", mcp.Description("Limit stats to within this element (XPath or CSS). Default: the whole page body.")),
		mcp.WithNumber("top", mcp.Description("Max entries per histogram, by descending count (default: 50)")),
	)
}

type statsConfig struct {
	Selector string `json:"selector"`
	XPath    bool   `json:"xpath"`
	Top      int    `json:"top"`
}

func PageStatsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	selector := request.GetString("selector", "")
	cfg := statsConfig{
		Selector: selector,
		XPath:    selector != "" && isXPathSelector(selector),
		Top:      max(request.GetInt("top", 50), 1),
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("could not encode config: %v", err)
	}

	var raw json.RawMessage
	if err := mcpcdp.Manager.Execute(id, chromedp.Evaluate(pageStatsJS(string(cfgJSON)), &raw)); err != nil {
		return nil, fmt.Errorf("page-stats failed: %v", err)
	}

	var probe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.Error != "" {
		return mcp.NewToolResultError(probe.Error), nil
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return mcp.NewToolResultText(string(raw)), nil
	}
	return mcp.NewToolResultText(pretty.String()), nil
}

// pageStatsJS walks the (optionally scoped) DOM once and returns structural
// histograms and counts as a plain JSON-able object. CFG is injected as JSON.
func pageStatsJS(cfgJSON string) string {
	return fmt.Sprintf(`(() => {
  const CFG = %s;
  const TOP = CFG.top;

  let root = document.body || document.documentElement;
  let scopeLabel = 'document';
  if (CFG.selector) {
    let el = null;
    try {
      el = CFG.xpath
        ? document.evaluate(CFG.selector, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue
        : document.querySelector(CFG.selector);
    } catch (e) {
      return { error: 'invalid scope selector: ' + String((e && e.message) || e) };
    }
    if (!el) return { error: 'scope selector matched no element: ' + CFG.selector };
    root = el; scopeLabel = CFG.selector;
  }

  const tags = {}, classes = {}, roles = {}, dataAttrs = {}, inputTypes = {};
  const ids = [];
  let total = 0, maxDepth = 0;

  const stack = [[root, 0]];
  while (stack.length) {
    const pair = stack.pop();
    const el = pair[0], depth = pair[1];
    total++;
    if (depth > maxDepth) maxDepth = depth;

    const tag = el.tagName.toLowerCase();
    tags[tag] = (tags[tag] || 0) + 1;

    if (el.getAttribute) {
      const cls = el.getAttribute('class');
      if (cls) cls.split(/\s+/).forEach((c) => { if (c) classes[c] = (classes[c] || 0) + 1; });
      if (el.id) ids.push(el.id);
      const role = el.getAttribute('role'); if (role) roles[role] = (roles[role] || 0) + 1;
      if (tag === 'input') { const t = (el.getAttribute('type') || 'text').toLowerCase(); inputTypes[t] = (inputTypes[t] || 0) + 1; }
      const attrs = el.attributes;
      for (let i = 0; i < attrs.length; i++) {
        const n = attrs[i].name;
        if (n.indexOf('data-') === 0) dataAttrs[n] = (dataAttrs[n] || 0) + 1;
      }
    }

    for (let c = el.firstElementChild; c; c = c.nextElementSibling) stack.push([c, depth + 1]);
  }

  const top = (m) => Object.keys(m)
    .map((k) => ({ name: k, count: m[k] }))
    .sort((a, b) => (b.count - a.count) || (a.name < b.name ? -1 : 1))
    .slice(0, TOP);

  const Q = (sel) => { try { return root.querySelectorAll(sel).length; } catch (e) { return 0; } };

  return {
    scope: scopeLabel,
    totalElements: total,
    maxDepth: maxDepth,
    distinctTags: Object.keys(tags).length,
    distinctClasses: Object.keys(classes).length,
    idCount: ids.length,
    tags: top(tags),
    classes: top(classes),
    roles: top(roles),
    dataAttributes: top(dataAttrs),
    inputTypes: top(inputTypes),
    ids: ids.slice(0, 300),
    components: {
      links: Q('a[href]'),
      buttons: Q('button, input[type=submit], input[type=button]'),
      inputs: Q('input, textarea, select'),
      forms: Q('form'),
      images: Q('img'),
      iframes: Q('iframe'),
      headings: Q('h1,h2,h3,h4,h5,h6'),
      lists: Q('ul,ol'),
      listItems: Q('li'),
      tables: Q('table'),
    },
  };
})()`, cfgJSON)
}
