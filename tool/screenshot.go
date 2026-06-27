package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewScreenshotTool() mcp.Tool {
	return mcp.NewTool("screenshot",
		mcp.WithDescription("Take a screenshot of a specific element or the browser viewport. The result description reports the image dimensions in pixels, which define the coordinate space for the mouse-* tools."),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("Chrome instance ID to perform the screenshot action on"),
		),
		mcp.WithString("url",
			mcp.Description("Navigate to the provided URL before taking screenshot. If URL is not provided, will stay at current page"),
		),
		mcp.WithString("selector",
			mcp.Description("The selector to identify the element for screenshot. Examples: '#input-id', '.input-class', 'input[name=\"username\"]', '//input[@id=\"email\"]'. If not provided, will take screenshot of the browser viewport"),
		),
		mcp.WithBoolean("full_page",
			mcp.Description("Capture the full scrollable page instead of just the visible viewport (default: false). Keep the default for computer-use loops so screenshot pixels match mouse-* coordinates."),
		),
	)
}

func ScreenshotHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	url := request.GetString("url", "")
	selector := request.GetString("selector", "")
	fullPage := request.GetBool("full_page", false)

	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}

	// Navigate to URL if provided
	if url != "" {
		err := mcpcdp.Manager.Execute(id,
			chromedp.Navigate(url),
			chromedp.WaitReady("body"),
		)
		if err != nil {
			return nil, fmt.Errorf("error happened when navigating to URL: %v", err)
		}
	}

	var screenshotBuf []byte
	var screenshotDescription string

	switch {
	case selector != "":
		// Check if element exists before taking screenshot
		var elements []*cdp.Node
		err := mcpcdp.Manager.Execute(id,
			chromedp.Nodes(selector, &elements),
		)
		if err != nil || len(elements) < 1 {
			return mcp.NewToolResultError(fmt.Sprintf("No elements found with selector: %s, please check element tree again", selector)), nil
		}

		err = mcpcdp.Manager.Execute(id,
			chromedp.Screenshot(selector, &screenshotBuf),
		)
		if err != nil {
			return nil, fmt.Errorf("error happened when taking element screenshot: %v", err)
		}
		screenshotDescription = fmt.Sprintf("Screenshot of element with selector: %s", selector)
	case fullPage:
		// Full scrollable page (resized to fit page height).
		err := mcpcdp.Manager.Execute(id,
			chromedp.FullScreenshot(&screenshotBuf, 100),
		)
		if err != nil {
			return nil, fmt.Errorf("error happened when taking full-page screenshot: %v", err)
		}
		screenshotDescription = "Screenshot of full scrollable page"
	default:
		// Visible viewport only — pixels match the mouse-* coordinate space.
		err := mcpcdp.Manager.Execute(id,
			chromedp.CaptureScreenshot(&screenshotBuf),
		)
		if err != nil {
			return nil, fmt.Errorf("error happened when taking viewport screenshot: %v", err)
		}
		screenshotDescription = "Screenshot of visible viewport"
	}

	// Report dimensions so a computer-use agent knows its coordinate space.
	if cfg, err := png.DecodeConfig(bytes.NewReader(screenshotBuf)); err == nil {
		screenshotDescription = fmt.Sprintf("%s (%dx%d px)", screenshotDescription, cfg.Width, cfg.Height)
	}

	imageData := base64.StdEncoding.EncodeToString(screenshotBuf)
	return mcp.NewToolResultImage(screenshotDescription, imageData, "image/png"), nil
}
