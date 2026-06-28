package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewMultiStepTool() mcp.Tool {
	return mcp.NewTool("multi-step",
		mcp.WithDescription("Run an ordered sequence of scoped steps on the current page in a single call. Each step's selector (CSS or XPath) is matched WITHIN the element selected by the previous step, narrowing the scope as you go — e.g. select a container, select a row inside it, click a button inside that row. If a step matches nothing the call fails and reports the 1-based step number, so an agent can drill into and act on nested structure without a round-trip per step. XPath in a scoped step should be relative (.//…); a leading // is auto-scoped to the current element."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance id")),
		mcp.WithArray("steps", mcp.Required(),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []any{"select", "click", "set-value", "extract"},
						"description": "select = narrow scope into the match; click = click the match; set-value = set an input/textarea value; extract = return the match's content without changing scope",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "CSS or XPath, evaluated within the current (narrowed) scope",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Value to set (set-value only)",
					},
				},
				"required": []any{"action", "selector"},
			}),
			mcp.Description("Ordered steps applied to the current page; each narrows the scope to its match")),
	)
}

type jsStep struct {
	Action   string `json:"action"`
	Selector string `json:"selector"`
	Value    string `json:"value"`
	XPath    bool   `json:"xpath"`
}

type msMatch struct {
	Tag        string `json:"tag"`
	ID         string `json:"id"`
	Class      string `json:"class"`
	Text       string `json:"text"`
	HTMLLength int    `json:"htmlLength"`
}

type msResult struct {
	OK         bool   `json:"ok"`
	FailedStep int    `json:"failedStep"`
	Action     string `json:"action"`
	Selector   string `json:"selector"`
	Message    string `json:"message"`
	Steps      []struct {
		Step     int      `json:"step"`
		Action   string   `json:"action"`
		Selector string   `json:"selector"`
		Matched  *msMatch `json:"matched"`
	} `json:"steps"`
	Final *struct {
		Tag  string `json:"tag"`
		Text string `json:"text"`
		HTML string `json:"html"`
		URL  string `json:"url"`
	} `json:"final"`
}

func MultiStepHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}

	rawSteps, ok := request.GetArguments()["steps"].([]any)
	if !ok || len(rawSteps) == 0 {
		return mcp.NewToolResultError("steps is required and must be a non-empty array"), nil
	}

	steps := make([]jsStep, 0, len(rawSteps))
	for i, rs := range rawSteps {
		m, ok := rs.(map[string]any)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("step %d is not an object", i+1)), nil
		}
		action, _ := m["action"].(string)
		selector, _ := m["selector"].(string)
		value, _ := m["value"].(string)
		switch action {
		case "select", "click", "set-value", "extract":
		default:
			return mcp.NewToolResultError(fmt.Sprintf("step %d has invalid action %q (want select|click|set-value|extract)", i+1, action)), nil
		}
		if strings.TrimSpace(selector) == "" {
			return mcp.NewToolResultError(fmt.Sprintf("step %d is missing a selector", i+1)), nil
		}
		steps = append(steps, jsStep{Action: action, Selector: selector, Value: value, XPath: isXPathSelector(selector)})
	}

	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("could not encode steps: %v", err)
	}

	var res msResult
	if err := mcpcdp.Manager.Execute(id, chromedp.Evaluate(multiStepJS(string(stepsJSON)), &res)); err != nil {
		return nil, fmt.Errorf("multi-step execution failed: %v", err)
	}

	if !res.OK {
		return mcp.NewToolResultError(fmt.Sprintf("step %d (%s %q) matched nothing: %s",
			res.FailedStep, res.Action, res.Selector, res.Message)), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "All %d steps succeeded.\n", len(res.Steps))
	for _, s := range res.Steps {
		desc := "(no element)"
		if s.Matched != nil {
			desc = "<" + s.Matched.Tag
			if s.Matched.ID != "" {
				desc += "#" + s.Matched.ID
			}
			if s.Matched.Class != "" {
				desc += "." + strings.ReplaceAll(strings.TrimSpace(s.Matched.Class), " ", ".")
			}
			desc += ">"
			if s.Matched.Text != "" {
				desc += " " + truncate(s.Matched.Text, 80)
			}
		}
		fmt.Fprintf(&sb, "  %d. %-9s %s -> %s\n", s.Step, s.Action, s.Selector, desc)
	}
	if res.Final != nil {
		sb.WriteString("\nFinal scope:\n")
		if res.Final.URL != "" {
			fmt.Fprintf(&sb, "  url: %s\n", res.Final.URL)
		}
		if res.Final.Tag != "" {
			fmt.Fprintf(&sb, "  <%s> text: %s\n", res.Final.Tag, truncate(res.Final.Text, 1500))
		}
		if res.Final.HTML != "" {
			fmt.Fprintf(&sb, "  html:\n%s\n", res.Final.HTML)
		}
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// multiStepJS builds a self-contained expression that walks the steps, narrowing
// the scope element at each step, and returns a plain JSON-able result object.
func multiStepJS(stepsJSON string) string {
	return fmt.Sprintf(`(() => {
  const STEPS = %s;

  const isEl = (n) => n && n.nodeType === 1;

  function find(scope, sel, xpath, scoped) {
    if (xpath) {
      let expr = sel;
      // A leading // is document-absolute; make it relative to the scope element.
      if (scoped && expr.startsWith('//')) expr = '.' + expr;
      const r = document.evaluate(expr, scope, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null);
      return r.singleNodeValue;
    }
    return scope.querySelector ? scope.querySelector(sel) : null;
  }

  function summ(el) {
    if (!isEl(el)) return null;
    const txt = (el.textContent || '').trim().replace(/\s+/g, ' ');
    return {
      tag: el.tagName.toLowerCase(),
      id: el.id || '',
      class: (el.getAttribute && el.getAttribute('class')) || '',
      text: txt.slice(0, 500),
      htmlLength: el.innerHTML ? el.innerHTML.length : 0,
    };
  }

  let scope = document;
  const trace = [];

  for (let i = 0; i < STEPS.length; i++) {
    const st = STEPS[i];
    const scoped = scope !== document;
    const el = find(scope, st.selector, st.xpath, scoped);

    if (!isEl(el)) {
      return {
        ok: false, failedStep: i + 1, action: st.action, selector: st.selector,
        message: 'no element matched within ' + (scoped ? 'the previous selection' : 'the document'),
      };
    }

    if (st.action === 'select' || st.action === 'extract') {
      trace.push({ step: i + 1, action: st.action, selector: st.selector, matched: summ(el) });
      scope = el;
    } else if (st.action === 'click') {
      el.scrollIntoView({ block: 'center' });
      el.click();
      trace.push({ step: i + 1, action: 'click', selector: st.selector, matched: summ(el) });
    } else if (st.action === 'set-value') {
      el.focus();
      const proto = (el instanceof HTMLTextAreaElement) ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const desc = Object.getOwnPropertyDescriptor(proto, 'value');
      if (desc && desc.set) desc.set.call(el, st.value || ''); else el.value = st.value || '';
      el.dispatchEvent(new Event('input', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      trace.push({ step: i + 1, action: 'set-value', selector: st.selector, matched: summ(el) });
    }
  }

  const finalEl = isEl(scope) ? scope : null;
  return {
    ok: true,
    steps: trace,
    final: finalEl
      ? {
          tag: finalEl.tagName.toLowerCase(),
          text: (finalEl.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 2000),
          html: (finalEl.outerHTML || '').slice(0, 4000),
          url: location.href,
        }
      : { url: location.href },
  };
})()`, stepsJSON)
}
