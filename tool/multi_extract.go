package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// jsExtract is one (name, selector) extraction spec passed into the page.
type jsExtract struct {
	Name     string `json:"name"`
	Selector string `json:"selector"`
	XPath    bool   `json:"xpath"`
}

// defaultExtractSpecs are common page components, probed when the caller passes
// no selectors. Names are prefixed with "-" to mark them as the built-in probes;
// each returns a (possibly empty) list, so an empty list tells the agent the
// component is absent or the usual pattern doesn't apply on this page.
var defaultExtractSpecs = []jsExtract{
	{Name: "-title", Selector: "//title"},
	{Name: "-h1", Selector: "//h1"},
	{Name: "-h2", Selector: "//h2"},
	{Name: "-main", Selector: "//main | //*[@role='main'] | //article"},
	{Name: "-nav", Selector: "//nav//a | //*[@role='navigation']//a"},
	{Name: "-search", Selector: "//input[@type='search'] | //*[@role='search']//input | //*[@role='search']//select | //input[contains(@name,'q') or contains(@name,'search') or contains(@name,'query')]"},
	{Name: "-breadcrumb", Selector: "//nav[contains(translate(@aria-label,'BREADCRUMB','breadcrumb'),'breadcrumb')]//a | //*[contains(@class,'breadcrumb')]//a | //*[contains(@class,'breadcrumb')]//li"},
	{Name: "-pagination", Selector: "//a[@rel='next'] | //a[@rel='prev'] | //*[contains(@class,'pag')]//a"},
	{Name: "-buttons", Selector: "//button | //input[@type='submit'] | //input[@type='button']"},
	{Name: "-forms", Selector: "//form"},
	{Name: "-tabs", Selector: "//*[@role='tab']"},
}

func NewMultiExtractTool() mcp.Tool {
	return mcp.NewTool("multi-extract",
		mcp.WithDescription("Extract several things from the current page in one call. Provide name/selector pairs; each selector (XPath or CSS) matches 0..n elements and the result maps every name to a LIST of matches (text, xpath, and href/value/options where relevant). An empty list means that selector matched nothing — so you can tell whether a component is absent or your pattern is wrong, instead of guessing. Omit `selectors` to run a default probe of common components (keys prefixed with '-', e.g. -title => //title, -h1 => //h1, -nav, -search, -breadcrumb, -pagination, ...)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance id")),
		mcp.WithArray("selectors",
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":     map[string]any{"type": "string", "description": "key under which this selector's matches are returned"},
					"selector": map[string]any{"type": "string", "description": "XPath (e.g. //h1) or CSS; extracts 0..n elements"},
				},
				"required": []any{"name", "selector"},
			}),
			mcp.Description("Name/selector pairs to extract. Omit to use the default probe of common page components.")),
		mcp.WithNumber("max_per_selector", mcp.Description("Maximum matches returned per selector (default: 50)")),
	)
}

func MultiExtractHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	maxPer := max(request.GetInt("max_per_selector", 50), 1)

	var specs []jsExtract
	if raw, ok := request.GetArguments()["selectors"].([]any); ok && len(raw) > 0 {
		for i, rs := range raw {
			m, ok := rs.(map[string]any)
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("selector %d is not an object", i+1)), nil
			}
			name, _ := m["name"].(string)
			selector, _ := m["selector"].(string)
			if strings.TrimSpace(name) == "" || strings.TrimSpace(selector) == "" {
				return mcp.NewToolResultError(fmt.Sprintf("selector %d needs both name and selector", i+1)), nil
			}
			specs = append(specs, jsExtract{Name: name, Selector: selector, XPath: isXPathSelector(selector)})
		}
	} else {
		specs = make([]jsExtract, len(defaultExtractSpecs))
		copy(specs, defaultExtractSpecs)
		for i := range specs {
			specs[i].XPath = isXPathSelector(specs[i].Selector)
		}
	}

	specsJSON, err := json.Marshal(specs)
	if err != nil {
		return nil, fmt.Errorf("could not encode selectors: %v", err)
	}

	var raw json.RawMessage
	if err := mcpcdp.Manager.Execute(id, chromedp.Evaluate(multiExtractJS(string(specsJSON), maxPer), &raw)); err != nil {
		return nil, fmt.Errorf("multi-extract failed: %v", err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		return mcp.NewToolResultText(string(raw)), nil
	}
	return mcp.NewToolResultText(pretty.String()), nil
}

// multiExtractJS runs every spec against the current document and returns
// { url, extracted: {name: [matches...]}, errors: {name: message} }. Every spec
// name appears in `extracted` even with zero matches, so an empty list is a clear
// "nothing matched" signal. Invalid selectors surface under `errors`.
func multiExtractJS(specsJSON string, maxPer int) string {
	return fmt.Sprintf(`(() => {
  const SPECS = %s;
  const MAX = %d;
  const q = (s) => JSON.stringify(s);
  const clean = (s) => ((s || '').trim().replace(/\s+/g, ' '));
  const trim = (s, n) => (s.length > n ? s.slice(0, n) + '…' : s);

  function xpath(el) {
    if (!el || el.nodeType !== 1) return '';
    if (el.id) return '//*[@id=' + q(el.id) + ']';
    const segs = [];
    for (let cur = el; cur && cur.nodeType === 1; cur = cur.parentElement) {
      if (cur.id) { segs.unshift('*[@id=' + q(cur.id) + ']'); return '//' + segs.join('/'); }
      let ix = 1, sib = cur.previousElementSibling;
      while (sib) { if (sib.tagName === cur.tagName) ix++; sib = sib.previousElementSibling; }
      segs.unshift(cur.tagName.toLowerCase() + '[' + ix + ']');
    }
    return '/' + segs.join('/');
  }

  function describe(el) {
    if (!el || el.nodeType !== 1) return { text: clean(el && el.textContent) };
    const tag = el.tagName.toLowerCase();
    const o = { text: trim(clean(el.textContent), 300), xpath: xpath(el) };
    const href = el.getAttribute && el.getAttribute('href');
    if (href) o.href = el.href || href;
    if (tag === 'input' || tag === 'textarea' || tag === 'select') {
      o.value = el.value || '';
      const nm = el.getAttribute('name'); if (nm) o.name = nm;
      const ty = el.getAttribute('type'); if (ty) o.type = ty;
      const ph = el.getAttribute('placeholder'); if (ph) o.placeholder = ph;
      if (tag === 'select') o.options = Array.from(el.options).slice(0, 40).map((op) => clean(op.textContent));
    }
    if (tag === 'img') {
      const src = el.getAttribute('src'); if (src) o.src = el.src || src;
      const alt = el.getAttribute('alt'); if (alt) o.alt = alt;
    }
    return o;
  }

  function collect(spec) {
    const out = [];
    if (spec.xpath) {
      const r = document.evaluate(spec.selector, document, null, XPathResult.ORDERED_NODE_SNAPSHOT_TYPE, null);
      for (let i = 0; i < r.snapshotLength && out.length < MAX; i++) out.push(describe(r.snapshotItem(i)));
    } else {
      const nodes = document.querySelectorAll(spec.selector);
      for (let i = 0; i < nodes.length && out.length < MAX; i++) out.push(describe(nodes[i]));
    }
    return out;
  }

  const extracted = {};
  const errors = {};
  for (const spec of SPECS) {
    try {
      extracted[spec.name] = collect(spec);
    } catch (e) {
      extracted[spec.name] = [];
      errors[spec.name] = String((e && e.message) || e);
    }
  }
  return { url: location.href, extracted, errors };
})()`, specsJSON, maxPer)
}
