package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	mcpcdp "chromedp-container-mcp/chromedp"

	"github.com/mark3labs/mcp-go/mcp"
)

func NewSystemStatsTool() mcp.Tool {
	return mcp.NewTool("system-stats",
		mcp.WithDescription("Report resource usage of the sandbox: total memory of the headless Chrome processes (and process count), the MCP server's own memory and goroutines, and the managed Chrome instances (active vs. max, with each instance's idle time and TTL). Use it to check headroom before creating more instances or to debug memory pressure."),
	)
}

func SystemStatsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type instanceStat struct {
		ID          string `json:"id"`
		IdleSeconds int    `json:"idleSeconds"`
		TTLSeconds  int    `json:"ttlSeconds"`
		Expired     bool   `json:"expired"`
	}
	type stats struct {
		Server struct {
			RSSMB         float64 `json:"rssMB"`
			GoHeapAllocMB float64 `json:"goHeapAllocMB"`
			GoSysMB       float64 `json:"goSysMB"`
			Goroutines    int     `json:"goroutines"`
		} `json:"server"`
		Chrome struct {
			ProcessCount int     `json:"processCount"`
			TotalRSSMB   float64 `json:"totalRssMB"`
			Note         string  `json:"note,omitempty"`
		} `json:"chrome"`
		Instances struct {
			Active int            `json:"active"`
			Max    int            `json:"max"`
			List   []instanceStat `json:"list"`
		} `json:"instances"`
	}

	var s stats
	const mb = 1024 * 1024

	// MCP server (Go) memory.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.Server.GoHeapAllocMB = round2(float64(ms.HeapAlloc) / mb)
	s.Server.GoSysMB = round2(float64(ms.Sys) / mb)
	s.Server.Goroutines = runtime.NumGoroutine()
	if rss, ok := procRSSKB(strconv.Itoa(os.Getpid())); ok {
		s.Server.RSSMB = round2(float64(rss) / 1024)
	}

	// Chrome processes: sum RSS across the headless-shell process tree (skipping
	// this server process). Relies on /proc, which exists in the Linux container.
	self := os.Getpid()
	if entries, err := os.ReadDir("/proc"); err == nil {
		var totalKB int64
		count := 0
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid == self {
				continue
			}
			name, rss, ok := procNameRSSKB(e.Name())
			if !ok {
				continue
			}
			if strings.Contains(name, "headless") || strings.Contains(name, "chrome") {
				totalKB += rss
				count++
			}
		}
		s.Chrome.ProcessCount = count
		s.Chrome.TotalRSSMB = round2(float64(totalKB) / 1024)
	} else {
		s.Chrome.Note = "process memory unavailable (no /proc on this platform)"
	}

	// Managed instances.
	info := mcpcdp.Manager.GetInstancesInfo()
	s.Instances.Active = len(info)
	s.Instances.Max = mcpcdp.Manager.Maximum()
	s.Instances.List = make([]instanceStat, 0, len(info))
	for id, in := range info {
		s.Instances.List = append(s.Instances.List, instanceStat{
			ID:          id,
			IdleSeconds: int(time.Since(in.LastUsed).Seconds()),
			TTLSeconds:  int(in.TTL.Seconds()),
			Expired:     in.IsExpired,
		})
	}

	out, _ := json.Marshal(s)
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		return mcp.NewToolResultText(pretty.String()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

// procRSSKB reads VmRSS (in kB) for a pid from /proc/<pid>/status.
func procRSSKB(pid string) (int64, bool) {
	_, rss, ok := procNameRSSKB(pid)
	return rss, ok
}

// procNameRSSKB returns the process Name and VmRSS (kB) from /proc/<pid>/status.
func procNameRSSKB(pid string) (string, int64, bool) {
	data, err := os.ReadFile("/proc/" + pid + "/status")
	if err != nil {
		return "", 0, false
	}
	var name string
	var rss int64
	var haveRSS bool
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		} else if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if v, err := strconv.ParseInt(f[1], 10, 64); err == nil {
					rss = v
					haveRSS = true
				}
			}
		}
	}
	return name, rss, haveRSS
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
