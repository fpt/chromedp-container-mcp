package tool

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func NewNetworkCheckTool() mcp.Tool {
	return mcp.NewTool("network-check",
		mcp.WithDescription("Diagnose the sandbox's outbound network from inside the container: DNS servers, how a host resolves (IPv4 vs IPv6 addresses), and whether IPv4 and IPv6 egress actually work (TCP connect to public anycast resolvers on :443). Optionally probe a URL over HTTP(S). Use this when navigation fails or hangs — e.g. a host that resolves only to IPv6 while the VM has no IPv6 route is a common cause."),
		mcp.WithString("host", mcp.Description("Hostname to resolve (default: example.com)")),
		mcp.WithString("url", mcp.Description("Optional URL to probe with an HTTP(S) GET (reports status or error)")),
	)
}

func NetworkCheckHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	host := request.GetString("host", "example.com")
	url := request.GetString("url", "")

	type egress struct {
		Target    string `json:"target"`
		OK        bool   `json:"ok"`
		LatencyMS int64  `json:"latencyMs,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	type result struct {
		DNSServers   []string `json:"dnsServers"`
		Host         string   `json:"host"`
		ResolvedIPv4 []string `json:"resolvedIPv4"`
		ResolvedIPv6 []string `json:"resolvedIPv6"`
		ResolveError string   `json:"resolveError,omitempty"`
		EgressIPv4   egress   `json:"egressIPv4"`
		EgressIPv6   egress   `json:"egressIPv6"`
		HTTPProbe    *struct {
			URL       string `json:"url"`
			Status    int    `json:"status,omitempty"`
			LatencyMS int64  `json:"latencyMs,omitempty"`
			Error     string `json:"error,omitempty"`
		} `json:"httpProbe,omitempty"`
		Hint string `json:"hint,omitempty"`
	}

	var r result
	r.Host = host
	r.DNSServers = resolvConfServers()

	// DNS resolution, split by family.
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ips, err := net.DefaultResolver.LookupIP(rctx, "ip", host); err != nil {
		r.ResolveError = err.Error()
	} else {
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				r.ResolvedIPv4 = append(r.ResolvedIPv4, v4.String())
			} else {
				r.ResolvedIPv6 = append(r.ResolvedIPv6, ip.String())
			}
		}
	}

	r.EgressIPv4 = dialCheck("tcp4", "1.1.1.1:443")
	r.EgressIPv6 = dialCheck("tcp6", "[2606:4700:4700::1111]:443")

	// Actionable hint for the situation this tool was built for.
	if r.EgressIPv4.OK && !r.EgressIPv6.OK && len(r.ResolvedIPv6) > 0 {
		r.Hint = "IPv6 egress fails but the host has IPv6 addresses; Chrome should fall back to IPv4 (Happy Eyeballs) but may be slow. IPv4 egress works."
	} else if !r.EgressIPv4.OK && !r.EgressIPv6.OK {
		r.Hint = "No outbound TCP on either family — the container has no internet egress. Check the container runtime's networking."
	}

	if url != "" {
		probe := struct {
			URL       string `json:"url"`
			Status    int    `json:"status,omitempty"`
			LatencyMS int64  `json:"latencyMs,omitempty"`
			Error     string `json:"error,omitempty"`
		}{URL: url}
		client := &http.Client{Timeout: 12 * time.Second}
		// Mirror the browser: when the server is configured to ignore cert errors
		// (e.g. behind a TLS-intercepting proxy), the probe should too, so its
		// result reflects what navigation would actually do.
		if os.Getenv("CHROME_IGNORE_CERT_ERRORS") == "true" {
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}
		start := time.Now()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			probe.Error = err.Error()
		} else if resp, err := client.Do(req); err != nil {
			probe.Error = err.Error()
			probe.LatencyMS = time.Since(start).Milliseconds()
		} else {
			probe.Status = resp.StatusCode
			probe.LatencyMS = time.Since(start).Milliseconds()
			resp.Body.Close()
		}
		r.HTTPProbe = &probe
	}

	out, _ := json.Marshal(r)
	var pretty bytes.Buffer
	if json.Indent(&pretty, out, "", "  ") == nil {
		return mcp.NewToolResultText(pretty.String()), nil
	}
	return mcp.NewToolResultText(string(out)), nil
}

func dialCheck(network, addr string) (e struct {
	Target    string `json:"target"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
}) {
	e.Target = network + " " + addr
	start := time.Now()
	conn, err := net.DialTimeout(network, addr, 6*time.Second)
	if err != nil {
		e.Error = err.Error()
		return e
	}
	conn.Close()
	e.OK = true
	e.LatencyMS = time.Since(start).Milliseconds()
	return e
}

// resolvConfServers reads nameserver entries from /etc/resolv.conf.
func resolvConfServers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			servers = append(servers, f[1])
		}
	}
	return servers
}
