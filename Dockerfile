# syntax=docker/dockerfile:1

# ---- build stage -----------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build

WORKDIR /src

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

ENV MCP_HOST=0.0.0.0 \
    MCP_PORT=8080 \
    CHROME_MAXIMUM_INSTANCE=5 \
    CHROME_TTL=15 \
    CHROME_EXE_TIMEOUT=300

EXPOSE 8080

# Override the base image's Chrome entrypoint with the MCP server. Chrome is
# launched per-session by chromedp itself.
ENTRYPOINT ["/usr/local/bin/chromedp-container-mcp"]
