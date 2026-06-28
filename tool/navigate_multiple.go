package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

func NewNavigateMultipleTool() mcp.Tool {
	return mcp.NewTool("navigate-multiple",
		mcp.WithDescription("Open several URLs in parallel — each in its own tab of the given Chrome instance — apply the same DOM-cleaning filter as `navigate` to every page, and return the cleaned DOM trees side by side. Use this to fetch and compare multiple sub-pages at once. The tabs are opened and closed automatically; the instance's main tab is left untouched."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance id")),
		mcp.WithArray("urls", mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
			mcp.Description("URLs to fetch in parallel")),
		mcp.WithNumber("depth", mcp.Description("Maximum DOM tree depth to traverse per page (default: 5)")),
		mcp.WithNumber("max_concurrency", mcp.Description("Maximum number of pages fetched at the same time (default: 5)")),
		mcp.WithNumber("timeout", mcp.Description("Per-page timeout in seconds (default: the server's CHROME_EXE_TIMEOUT)")),
	)
}

func NavigateMultipleHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	urls := request.GetStringSlice("urls", nil)
	if len(urls) == 0 {
		return mcp.NewToolResultError("urls is required and must be a non-empty array"), nil
	}
	depth := max(request.GetInt("depth", 5), 0)
	concurrency := max(request.GetInt("max_concurrency", 5), 1)
	timeout := time.Duration(max(request.GetInt("timeout", 0), 0)) * time.Second

	// Ensure the browser is started once before opening tabs in parallel, so the
	// concurrent tab creations below don't race on browser startup. Running the
	// instance with no actions boots the browser without touching its main tab.
	if err := mcpcdp.Manager.Execute(id); err != nil {
		return nil, fmt.Errorf("could not start browser for instance %s: %v", id, err)
	}

	type result struct {
		html string
		err  error
	}
	results := make([]result, len(urls))
	js := cleanElement(depth, "") // empty selector => clean the whole <body>

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var cleanHTML string
			err := mcpcdp.Manager.ExecuteInNewTab(id, timeout,
				chromedp.Navigate(u),
				chromedp.WaitReady("body"),
				chromedp.Evaluate(js, &cleanHTML),
			)
			results[i] = result{html: cleanHTML, err: err}
		}(i, u)
	}
	wg.Wait()

	var sb strings.Builder
	ok := 0
	for i, u := range urls {
		fmt.Fprintf(&sb, "===== [%d] %s =====\n", i+1, u)
		if results[i].err != nil {
			fmt.Fprintf(&sb, "ERROR: %v\n", results[i].err)
		} else {
			ok++
			sb.WriteString(annotateDepthTruncation(results[i].html, depth))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	header := fmt.Sprintf("Fetched %d/%d pages in parallel (depth %d)\n\n", ok, len(urls), depth)
	return mcp.NewToolResultText(header + sb.String()), nil
}
