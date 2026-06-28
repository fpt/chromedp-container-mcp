// Command server runs the chromedp headless-browsing sandbox as an MCP server
// over the SSE transport. It is designed to run inside a container that also
// ships the headless Chrome binary (e.g. based on chromedp/headless-shell).
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/mark3labs/mcp-go/server"

	cdp "chromedp-container-mcp/chromedp"
	"chromedp-container-mcp/tool"
)

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// Chrome instance manager configuration.
	maximum := envInt("CHROME_MAXIMUM_INSTANCE", 5)
	ttl := envInt("CHROME_TTL", 15)            // minutes a browser may sit idle
	timeout := envInt("CHROME_EXE_TIMEOUT", 300) // seconds a single action may run
	cdp.InitManager(maximum, time.Duration(ttl)*time.Minute, time.Duration(timeout)*time.Second)

	s := server.NewMCPServer(
		"chromedp-container-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(tool.NewTipsTool(), tool.TipsHandler)

	// Instance lifecycle
	s.AddTool(tool.NewCreateInstanceTool(), tool.CreateInstanceHandler)
	s.AddTool(tool.NewCloseInstanceTool(), tool.CloseInstanceHandler)

	// Page navigation
	s.AddTool(tool.NewNavigateTool(), tool.NavigateHandler)
	s.AddTool(tool.NewNavigateBackTool(), tool.NavigateBackHandler)
	s.AddTool(tool.NewNavigateForwardTool(), tool.NavigateForwardHandler)
	s.AddTool(tool.NewNavigateMultipleTool(), tool.NavigateMultipleHandler)

	// Element operations
	s.AddTool(tool.NewGetElementTool(), tool.GetElementHandler)
	s.AddTool(tool.NewAllElementTool(), tool.AllElementHandler)
	s.AddTool(tool.NewClickElementTool(), tool.ClickElementHandler)
	s.AddTool(tool.NewSelectElementTool(), tool.SelectElementHandler)

	// Input operations (selector-based)
	s.AddTool(tool.NewSendKeyTool(), tool.SendKeyHandler)
	s.AddTool(tool.NewSetValueTool(), tool.SetValueHandler)
	s.AddTool(tool.NewKeyEventTool(), tool.KeyEventHandler)

	// Computer-use primitives (coordinate / screenshot driven)
	s.AddTool(tool.NewMouseClickTool(), tool.MouseClickHandler)
	s.AddTool(tool.NewMouseMoveTool(), tool.MouseMoveHandler)
	s.AddTool(tool.NewMouseDragTool(), tool.MouseDragHandler)
	s.AddTool(tool.NewScrollTool(), tool.ScrollHandler)
	s.AddTool(tool.NewTypeTextTool(), tool.TypeTextHandler)
	s.AddTool(tool.NewPressKeysTool(), tool.PressKeysHandler)
	s.AddTool(tool.NewWaitTool(), tool.WaitHandler)

	// Cookie management
	s.AddTool(tool.NewSetCookieTool(), tool.SetCookieHandler)

	// File downloads
	s.AddTool(tool.NewDownloadFileTool(), tool.DownloadFileHandler)
	s.AddTool(tool.NewDownloadImageTool(), tool.DownloadImageHandler)

	// Screenshot & PDF
	s.AddTool(tool.NewScreenshotTool(), tool.ScreenshotHandler)
	s.AddTool(tool.NewPdfTool(), tool.GenPdfHandler)

	// MCP_TRANSPORT selects the transport: "stdio" (default) speaks JSON-RPC
	// over stdin/stdout for clients that launch the container as a subprocess
	// (docker run -i --rm ...); "sse" serves an HTTP/SSE endpoint instead.
	switch env("MCP_TRANSPORT", "stdio") {
	case "sse":
		host := env("MCP_HOST", "0.0.0.0")
		port := env("MCP_PORT", "8080")
		addr := host + ":" + port

		// baseURL is advertised to clients in the SSE "endpoint" event so they
		// know where to POST messages. Override with MCP_BASE_URL behind a proxy.
		baseURL := env("MCP_BASE_URL", "http://"+addr)

		sse := server.NewSSEServer(s, server.WithBaseURL(baseURL))

		log.Printf("chromedp-container-mcp listening on %s — SSE endpoint: %s/sse, messages: %s/message",
			addr, baseURL, baseURL)
		if err := sse.Start(addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
	default:
		// Logs go to stderr so they never corrupt the JSON-RPC stream on stdout.
		log.SetOutput(os.Stderr)
		log.Printf("chromedp-container-mcp serving on stdio")
		if err := server.ServeStdio(s); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}
