package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// isXPathSelector reports whether sel looks like an XPath expression rather than
// a CSS selector. chromedp's BySearch handles both, but raw document.querySelector
// does not — so JS that resolves an element must branch on this.
func isXPathSelector(sel string) bool {
	t := strings.TrimSpace(sel)
	return strings.HasPrefix(t, "/") ||
		strings.HasPrefix(t, "(/") ||
		strings.HasPrefix(t, "./") ||
		strings.HasPrefix(t, "(.//")
}

func NewClickElementTool() mcp.Tool {
	return mcp.NewTool("click-element",
		mcp.WithDescription("Click on an element specified by selector (CSS selector, XPath, ID, class, etc.). Returns success status and any errors."),
		mcp.WithString("selector",
			mcp.Required(),
			mcp.Description("The selector to identify the element to click. Examples: '#button-id', '.button-class', 'button[type=\"submit\"]', '//button[@id=\"submit\"]'"),
		),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Chrome instance ID to perform the click action on"),
		),
		mcp.WithNumber("timeout",
			mcp.Description("Timeout in seconds to wait for element (default: 10)"),
		),
		mcp.WithBoolean("wait_visible",
			mcp.Description("Whether to wait for element to be visible before clicking (default: true)"),
		),
		mcp.WithString("click_type",
			mcp.Enum("left", "right", "double"),
			mcp.Description("Type of click: 'left' (default), 'right', 'double'"),
		),
	)
}

// ClickElementHandler handles the click element tool request
func ClickElementHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters from request
	selector := request.GetString("selector", "")
	instanceID := request.GetString("id", "")
	timeout := request.GetInt("timeout", 15)
	waitVisible := request.GetBool("wait_visible", true)
	clickType := request.GetString("click_type", "left")

	// Validate required parameters
	if selector == "" {
		return mcp.NewToolResultError("selector parameter is required"), nil
	}
	if instanceID == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}

	var elements []*cdp.Node
	err := mcpcdp.Manager.Execute(instanceID, 
		chromedp.Nodes(selector, &elements),
		)
	if err != nil || len(elements) < 1{
		return mcp.NewToolResultError(fmt.Sprintf("No elements found with selector: %s, please check element tree again or use screenshot for anlalyze", selector)), nil
	}

	// Validate click type
	validClickTypes := map[string]bool{
		"left":   true,
		"right":  true,
		"double": true,
	}
	if !validClickTypes[clickType] {
		return mcp.NewToolResultError("click_type must be 'left', 'right', or 'double'"), nil
	}

	// Perform the click action
	result, err := performClick(ctx, instanceID, selector, int(timeout), waitVisible, clickType)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Click action failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

// performClick executes the actual click operation
func performClick(ctx context.Context, instanceID, selector string, timeoutSec int, waitVisible bool, clickType string) (string, error) {
	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Build chromedp tasks
	var tasks []chromedp.Action

	// First, check if element exists
	tasks = append(tasks, chromedp.WaitReady(selector))

	// Wait for element to be visible if requested
	if waitVisible {
		tasks = append(tasks, chromedp.WaitVisible(selector))
	}

	// Add click action based on click type
	switch clickType {
	case "left":
		tasks = append(tasks, chromedp.Click(selector))
	case "right":
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			var nodes []*cdp.Node
			if err := chromedp.Nodes(selector, &nodes, chromedp.AtLeast(0)).Do(ctx); err != nil {
				return err
			}
			if len(nodes) == 0 {
				return fmt.Errorf("element not found: %s", selector)
			}
			return chromedp.MouseClickNode(nodes[0], chromedp.ButtonRight).Do(ctx)
		}))
	case "double":
		tasks = append(tasks, chromedp.DoubleClick(selector))
	}

	// // Execute the click operation
	// err := mcpcdp.Manager.Execute(instanceID, tasks...)
	// if err != nil {
	// 	return "", fmt.Errorf("failed to execute click: %w", err)
	// }

	done := make(chan error, 1)

	go func() {
		done <- mcpcdp.Manager.Execute(instanceID, tasks...)
	}()

	select {
	case err := <-done:
		if err != nil {
			return "", err
		}
	case <-timeoutCtx.Done():
		// Handle timeout case - return simple "timeout" error
		return "", fmt.Errorf("timeout")
	}

	// Verify the click was successful
	clickResult, err := verifyClickSuccess(instanceID, selector, clickType)
	if err != nil {
		return "", fmt.Errorf("click verification failed: %w", err)
	}

   return clickResult, nil
}

	
// verifyClickSuccess checks if the click was successful and provides feedback.
//
// The selector and clickType are passed into the page as JSON-encoded string
// literals (never string-interpolated into the JS source): an XPath such as
// //a[contains(text(),'foo')] contains quotes/parens that would otherwise break
// the script with "SyntaxError: missing ) after argument list". The element is
// resolved with document.evaluate for XPath and document.querySelector for CSS,
// since querySelector cannot handle XPath at all.
func verifyClickSuccess(instanceID, selector, clickType string) (string, error) {
	var result string

	// Get element information after click
	err := mcpcdp.Manager.Execute(instanceID,
		chromedp.Evaluate(verifyClickJS(selector, clickType), &result),
	)

	if err != nil {
		return "", fmt.Errorf("failed to verify click result: %w", err)
	}

	return result, nil
}

// verifyClickJS builds the in-page script that inspects the clicked element.
// selector and clickType are injected as JSON-encoded string literals so quotes
// or parens in an XPath can't break the script; the element is resolved with
// document.evaluate for XPath and document.querySelector for CSS.
func verifyClickJS(selector, clickType string) string {
	selJSON, _ := json.Marshal(selector)
	ctJSON, _ := json.Marshal(clickType)
	return fmt.Sprintf(`
			(() => {
				const SELECTOR = %s;
				const CLICK_TYPE = %s;
				const IS_XPATH = %t;
				try {
					const element = IS_XPATH
						? document.evaluate(SELECTOR, document, null, XPathResult.FIRST_ORDERED_NODE_TYPE, null).singleNodeValue
						: document.querySelector(SELECTOR);
					if (!element) {
						return JSON.stringify({
							success: false,
							message: 'Element no longer exists after click',
							selector: SELECTOR
						});
					}

					const elementInfo = {
						success: true,
						message: 'Click action completed successfully',
						selector: SELECTOR,
						clickType: CLICK_TYPE,
						elementInfo: {
							tag: element.tagName.toLowerCase(),
							id: element.id || 'none',
							className: element.className || 'none',
							disabled: element.disabled || false,
							visible: element.offsetParent !== null,
							text: (element.textContent || '').trim().substring(0, 50)
						}
					};

					// Check if it's a form element and provide additional info
					if (element.tagName.toLowerCase() === 'input' ||
						element.tagName.toLowerCase() === 'button' ||
						element.tagName.toLowerCase() === 'select') {
						elementInfo.elementInfo.value = element.value || '';
						elementInfo.elementInfo.type = element.type || '';
					}

					// Check if it's a link
					if (element.tagName.toLowerCase() === 'a') {
						elementInfo.elementInfo.href = element.href || '';
					}

					return JSON.stringify(elementInfo, null, 2);
				} catch (error) {
					return JSON.stringify({
						success: false,
						message: 'Error verifying click: ' + error.message,
						selector: SELECTOR
					});
				}
			})()
		`, selJSON, ctJSON, isXPathSelector(selector))
}




