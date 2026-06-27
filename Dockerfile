# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build

WORKDIR /src

# corporate proxy CA certificate (optional, skipped if not present).
# Local: `make certs.pem` copies ~/.corp-ca/certs.pem, then injects it via a
# Docker build secret. CI: the secret does not exist so this is skipped, and the
# bookworm base's system roots already trust proxy.golang.org / github.com.
RUN --mount=type=secret,id=ca_cert,required=false \
    if [ -f /run/secrets/ca_cert ] && [ -s /run/secrets/ca_cert ]; then \
      cp /run/secrets/ca_cert /usr/local/share/ca-certificates/custom-ca.crt && \
      update-ca-certificates && \
      echo "Custom CA certificate installed"; \
    fi

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Cross-compile a fully static binary for the target platform.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/chromedp-container-mcp ./cmd/server

# ---- runtime stage ---------------------------------------------------------
# Base image already ships headless Chrome at /headless-shell/headless-shell,
# which is on PATH so chromedp auto-discovers it.
FROM chromedp/headless-shell:latest

COPY --from=build /out/chromedp-container-mcp /usr/local/bin/chromedp-container-mcp

# CHROME_PROXY_SERVER is the default for the create-chrome-instance `proxy-server`
# flag. Set it when the container's only route to the internet is a corporate
# proxy (Chrome ignores http_proxy/HTTPS_PROXY, so it needs the --proxy-server
# flag). Empty by default; override at run time, e.g.
#   -e CHROME_PROXY_SERVER=http://proxy:3128
ENV MCP_HOST=0.0.0.0 \
    MCP_PORT=8080 \
    CHROME_MAXIMUM_INSTANCE=5 \
    CHROME_TTL=15 \
    CHROME_EXE_TIMEOUT=300 \
    CHROME_PROXY_SERVER=""

EXPOSE 8080

# Override the base image's Chrome entrypoint with the MCP server. Chrome is
# launched per-session by chromedp itself.
ENTRYPOINT ["/usr/local/bin/chromedp-container-mcp"]
