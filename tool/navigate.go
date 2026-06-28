package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewNavigateTool() mcp.Tool {
	return mcp.NewTool("navigate",
		mcp.WithDescription("Navigate to provided URL and return a clean DOM element tree structure without scripts/styles and textContent, if content is truncated, call the select-element tool to get a deeper DOM tree"),
		mcp.WithString("url",
			mcp.Required(),
			mcp.Description("The URL to navigate"),
			),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Chrome instance id"),
			),
		mcp.WithNumber("depth",
			 mcp.Description("Maximum DOM tree depth to traverse (default: 5)"),
			),
		mcp.WithString("selector",
			mcp.Description("Optional CSS selector or XPath to scope the returned DOM tree to one subtree (e.g. '#progress_list', '//main'). When set, traversal starts from the first matching element instead of <body>, so you can request a high depth for the part you care about without serializing the whole (often heavy/ad-laden) page and timing out."),
			),
		)
}


func NavigateHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error){
	url := request.GetString("url", "")

	if url == "" {
		return mcp.NewToolResultError("url parameter is required"), nil
	}

	id := request.GetString("id", "")

	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}

	depth := max(request.GetInt("depth", 5), 0)

	selector := request.GetString("selector", "")

	var cleanHTML string
	err := cdp.Manager.Execute(id,
		chromedp.Navigate(url),
		chromedp.Evaluate(cleanElement(depth, selector), &cleanHTML),
		)

	if (err != nil) {
		return nil, err
	}

	return mcp.NewToolResultText(annotateDepthTruncation(cleanHTML, depth)), nil
}

// truncationMarkers are the placeholders the DOM-cleaning filters emit when they
// hit the depth limit. Keep in sync with cleanElement (below) and selectElement
// (element.go).
var truncationMarkers = []string{
	"[Content truncated - max depth reached]",
	"[Depth limit reached]",
	`data-truncated="true"`,
}

// annotateDepthTruncation prepends a warning when the cleaned DOM hit the depth
// limit, so the agent knows content is present but hidden by `depth` (a silent
// truncation is a footgun) and can re-run with a larger value instead of
// concluding the branch is empty.
func annotateDepthTruncation(out string, depth int) string {
	for _, m := range truncationMarkers {
		if strings.Contains(out, m) {
			return fmt.Sprintf("⚠ Output was cut off at depth=%d — some branches hit the max-depth limit (see the truncation markers below). Re-run with a larger `depth` to reveal them.\n\n%s", depth, out)
		}
	}
	return out
}

func NewNavigateBackTool() mcp.Tool {
	return mcp.NewTool("navigate-back",
		mcp.WithDescription("Navigate to previous page"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Chrome instance id"),
			),
		)
}

func NavigateBackHandler (ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error){
	id := request.GetString("id", "")

	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	
	var url string

	err := cdp.Manager.Execute(id,
		chromedp.NavigateBack(),
		chromedp.Location(&url),
		)
	if (err != nil) {
		return nil, err
	}
	
	return mcp.NewToolResultText(fmt.Sprintf("Navigate back success, current url : %s", url)), nil
}

func NewNavigateForwardTool() mcp.Tool {
	return mcp.NewTool("navigate-forward",
		mcp.WithDescription("Navigate to next page"),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Chrome instance id"),
		),
	)
}

func NavigateForwardHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	
	var url string
	err := cdp.Manager.Execute(id,
		chromedp.NavigateForward(),
		chromedp.Location(&url),
	)
	if err != nil {
		return nil, err
	}
	
	return mcp.NewToolResultText(fmt.Sprintf("Navigate forward success, current url: %s", url)), nil
}

func cleanElement(depth int, selector string) string {
    // Encode the selector as a JS string literal so quotes/parens in an XPath
    // (e.g. //a[contains(text(),'x')]) can't break the script. Empty => <body>.
    selJSON, _ := json.Marshal(selector)
    isXPath := false
    if t := strings.TrimSpace(selector); t != "" {
        isXPath = strings.HasPrefix(t, "/") || strings.HasPrefix(t, "(/") ||
            strings.HasPrefix(t, "./") || strings.HasPrefix(t, "(.//")
    }
    return fmt.Sprintf(`
        (() => {
            const MAX_DEPTH = %d;
            const SELECTOR = %s;
            const IS_XPATH = %t;

            // Dangerous protocols to filter
            const DANGEROUS_PROTOCOLS = ['javascript:', 'data:', 'vbscript:', 'file:', 'about:', 'blob:'];
            
            function cleanForLLM(element, depth = 0) {
                if (depth > MAX_DEPTH) return '[Content truncated - max depth reached]';
                
                if (['SCRIPT', 'STYLE', 'NOSCRIPT', 'META', 'LINK', 'IFRAME', 'OBJECT', 'EMBED'].includes(element.tagName)) {
                    return '';
                }
                
                const newEl = document.createElement(element.tagName);
                
                // Process attributes with special handling for href and src
                Array.from(element.attributes).forEach(attr => {
                    const name = attr.name.toLowerCase();
                    const value = attr.value || '';
                    
                    // Block event handlers
                    if (name.startsWith('on')) {
                        // Optionally preserve for LLM analysis
                        newEl.setAttribute('data-event-' + name.substring(2), value);
                        return;
                    }
                    
                    // Block style attribute
                    if (name === 'style') return;
                    
                    // Special handling for href and src
                    if (['href', 'src'].includes(name)) {
                        const lowerValue = value.toLowerCase().trim();
                        
                        // Check for dangerous protocols
                        const isDangerous = DANGEROUS_PROTOCOLS.some(protocol => 
                            lowerValue.startsWith(protocol)
                        );
                        
                        if (!isDangerous) {
                            // Safe to include
                            newEl.setAttribute(attr.name, value);
                        } else {
                            // Preserve for LLM analysis but mark as filtered
                            newEl.setAttribute('data-filtered-' + name, value);
                            newEl.setAttribute('data-filter-reason', 'Dangerous protocol: ' + lowerValue.split(':')[0]);
                        }
                        return;
                    }
                    
                    // Keep all other attributes
                    try {
                        newEl.setAttribute(attr.name, value);
                    } catch (e) {
                        // Skip invalid attributes
                    }
                });
                
                Array.from(element.children).forEach(child => {
                    const cleaned = cleanForLLM(child, depth + 1);
                    if (cleaned) {
                        newEl.innerHTML += cleaned;
                    }
                });
                
                // Preserve text content for leaf nodes
                if (element.children.length === 0) {
                    const text = element.textContent?.trim();
                    if (text) newEl.textContent = text;
                }
                
                return newEl.outerHTML;
            }
            
            let root = document.body;
            if (SELECTOR) {
                root = IS_XPATH
                    ? document.evaluate(SELECTOR, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue
                    : document.querySelector(SELECTOR);
                if (!root) {
                    return '[selector not found: ' + SELECTOR + ']';
                }
            }
            return cleanForLLM(root, 0);
        })()
    `, depth, selJSON, isXPath)
}

