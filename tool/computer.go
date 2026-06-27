package tool

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
)

// This file implements coordinate-based, screenshot-driven primitives suited to
// "computer use" style agents (see OpenAI's computer-use guide): the agent reads
// a screenshot and acts on pixel coordinates rather than CSS/XPath selectors.
//
// Coordinates are CSS pixels with origin at the top-left of the viewport, which
// matches the pixels of a viewport screenshot when the instance runs at
// devicePixelRatio 1 (the default; the viewport size is fixed at instance
// creation via viewport-width / viewport-height).

// ---- mouse-click -----------------------------------------------------------

func NewMouseClickTool() mcp.Tool {
	return mcp.NewTool("mouse-click",
		mcp.WithDescription("Click at viewport pixel coordinates (x, y). Use this when working from a screenshot rather than a selector."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate in viewport pixels (0 = left edge)")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate in viewport pixels (0 = top edge)")),
		mcp.WithString("button", mcp.Enum("left", "right", "middle"), mcp.Description("Mouse button (default: left)")),
		mcp.WithNumber("clicks", mcp.Description("Click count: 1 = single, 2 = double (default: 1)")),
	)
}

func MouseClickHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	x := request.GetFloat("x", 0)
	y := request.GetFloat("y", 0)
	button := request.GetString("button", "left")
	clicks := max(request.GetInt("clicks", 1), 1)

	err := mcpcdp.Manager.Execute(id,
		chromedp.MouseClickXY(x, y, chromedp.Button(button), chromedp.ClickCount(clicks)),
	)
	if err != nil {
		return nil, fmt.Errorf("mouse-click failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("%s click (x%d) at (%.0f, %.0f)", button, clicks, x, y)), nil
}

// ---- mouse-move ------------------------------------------------------------

func NewMouseMoveTool() mcp.Tool {
	return mcp.NewTool("mouse-move",
		mcp.WithDescription("Move the cursor to viewport pixel coordinates (x, y) without clicking, e.g. to trigger hover states."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate in viewport pixels")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate in viewport pixels")),
	)
}

func MouseMoveHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	x := request.GetFloat("x", 0)
	y := request.GetFloat("y", 0)

	err := mcpcdp.Manager.Execute(id, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("mouse-move failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("moved cursor to (%.0f, %.0f)", x, y)), nil
}

// ---- mouse-drag ------------------------------------------------------------

func NewMouseDragTool() mcp.Tool {
	return mcp.NewTool("mouse-drag",
		mcp.WithDescription("Press the left mouse button at (from_x, from_y), drag to (to_x, to_y), and release. Useful for sliders, selections, and drag-and-drop."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithNumber("from_x", mcp.Required(), mcp.Description("Start X in viewport pixels")),
		mcp.WithNumber("from_y", mcp.Required(), mcp.Description("Start Y in viewport pixels")),
		mcp.WithNumber("to_x", mcp.Required(), mcp.Description("End X in viewport pixels")),
		mcp.WithNumber("to_y", mcp.Required(), mcp.Description("End Y in viewport pixels")),
	)
}

func MouseDragHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	fx := request.GetFloat("from_x", 0)
	fy := request.GetFloat("from_y", 0)
	tx := request.GetFloat("to_x", 0)
	ty := request.GetFloat("to_y", 0)

	err := mcpcdp.Manager.Execute(id, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := input.DispatchMouseEvent(input.MouseMoved, fx, fy).Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchMouseEvent(input.MousePressed, fx, fy).
			WithButton(input.Left).WithClickCount(1).Do(ctx); err != nil {
			return err
		}
		// A midpoint move makes the drag look continuous to most handlers.
		if err := input.DispatchMouseEvent(input.MouseMoved, (fx+tx)/2, (fy+ty)/2).
			WithButton(input.Left).Do(ctx); err != nil {
			return err
		}
		if err := input.DispatchMouseEvent(input.MouseMoved, tx, ty).
			WithButton(input.Left).Do(ctx); err != nil {
			return err
		}
		return input.DispatchMouseEvent(input.MouseReleased, tx, ty).
			WithButton(input.Left).WithClickCount(1).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("mouse-drag failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("dragged from (%.0f, %.0f) to (%.0f, %.0f)", fx, fy, tx, ty)), nil
}

// ---- scroll ----------------------------------------------------------------

func NewScrollTool() mcp.Tool {
	return mcp.NewTool("scroll",
		mcp.WithDescription("Scroll the page by a wheel delta, as if the wheel was used over point (x, y). Positive scroll_y scrolls down, positive scroll_x scrolls right."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithNumber("x", mcp.Description("X of the wheel anchor in viewport pixels (default: viewport center-ish 0)")),
		mcp.WithNumber("y", mcp.Description("Y of the wheel anchor in viewport pixels")),
		mcp.WithNumber("scroll_x", mcp.Description("Horizontal scroll delta in CSS pixels (default: 0)")),
		mcp.WithNumber("scroll_y", mcp.Description("Vertical scroll delta in CSS pixels (default: 0)")),
	)
}

func ScrollHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	x := request.GetFloat("x", 0)
	y := request.GetFloat("y", 0)
	dx := request.GetFloat("scroll_x", 0)
	dy := request.GetFloat("scroll_y", 0)

	err := mcpcdp.Manager.Execute(id, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseWheel, x, y).
			WithDeltaX(dx).WithDeltaY(dy).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("scroll failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("scrolled by (%.0f, %.0f) at (%.0f, %.0f)", dx, dy, x, y)), nil
}

// ---- type-text -------------------------------------------------------------

func NewTypeTextTool() mcp.Tool {
	return mcp.NewTool("type-text",
		mcp.WithDescription("Type a string into the currently focused element (e.g. after clicking an input). Inserts the literal text; use press-keys for Enter, Tab, shortcuts, etc."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type at the current focus")),
	)
}

func TypeTextHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	text := request.GetString("text", "")

	err := mcpcdp.Manager.Execute(id, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.InsertText(text).Do(ctx)
	}))
	if err != nil {
		return nil, fmt.Errorf("type-text failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("typed %d characters", len(text))), nil
}

// ---- press-keys ------------------------------------------------------------

func NewPressKeysTool() mcp.Tool {
	return mcp.NewTool("press-keys",
		mcp.WithDescription("Press a key or key combination at the current focus, e.g. [\"Enter\"], [\"Tab\"], [\"ctrl\",\"a\"], [\"ctrl\",\"shift\",\"t\"]. Modifiers are ctrl/shift/alt/meta; everything else is a key name (see key-event for the full list)."),
		mcp.WithString("id", mcp.Required(), mcp.Description("Chrome instance ID")),
		mcp.WithArray("keys", mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
			mcp.Description("Keys to press together, modifiers first. Examples: [\"Enter\"], [\"ctrl\",\"c\"]")),
	)
}

func PressKeysHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := request.GetString("id", "")
	if id == "" {
		return mcp.NewToolResultError("Chrome instance ID is required"), nil
	}
	keys := request.GetStringSlice("keys", nil)
	if len(keys) == 0 {
		return mcp.NewToolResultError("keys is required and must be a non-empty array"), nil
	}

	// Split into modifiers (ctrl/shift/alt/meta) and the main keys to send.
	var modifiers []input.Modifier
	var mainKeys []string
	for _, k := range keys {
		if mod, err := modifierMapper(k); err == nil {
			modifiers = append(modifiers, mod)
			continue
		}
		mainKeys = append(mainKeys, k)
	}
	if len(mainKeys) == 0 {
		return mcp.NewToolResultError("keys must include at least one non-modifier key"), nil
	}

	actions := make([]chromedp.Action, 0, len(mainKeys))
	for _, k := range mainKeys {
		mapped, err := KeyMapper(k)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("key [%s] is not supported: %v", k, err)), nil
		}
		if len(modifiers) > 0 {
			actions = append(actions, chromedp.KeyEvent(mapped, chromedp.KeyModifiers(modifiers...)))
		} else {
			actions = append(actions, chromedp.KeyEvent(mapped))
		}
	}

	if err := mcpcdp.Manager.Execute(id, actions...); err != nil {
		return nil, fmt.Errorf("press-keys failed: %v", err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("pressed: %s", strings.Join(keys, " + "))), nil
}

// ---- wait ------------------------------------------------------------------

func NewWaitTool() mcp.Tool {
	return mcp.NewTool("wait",
		mcp.WithDescription("Pause for a number of seconds to let the page settle (animations, network, redirects) before taking the next screenshot."),
		mcp.WithNumber("seconds", mcp.Description("Seconds to wait (default: 2, max: 30)")),
	)
}

func WaitHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	seconds := request.GetFloat("seconds", 2)
	if seconds < 0 {
		seconds = 0
	}
	if seconds > 30 {
		seconds = 30
	}
	select {
	case <-time.After(time.Duration(seconds * float64(time.Second))):
	case <-ctx.Done():
		return mcp.NewToolResultError("wait cancelled"), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("waited %.1f seconds", seconds)), nil
}
