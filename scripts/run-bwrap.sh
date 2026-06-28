#!/usr/bin/env bash
#
# Run chromedp-container-mcp under bubblewrap on Linux — no container runtime at
# run time. bubblewrap doesn't run OCI images, so this materializes the image's
# filesystem to a rootfs once (using docker/podman just for the export), then
# sandboxes the MCP server + headless Chrome with bwrap.
#
# Chrome runs with --no-sandbox (the server's default): bubblewrap IS the
# sandbox, so Chrome's own nested user-namespace sandbox isn't needed.
#
# Requirements: bubblewrap (bwrap) installed, and either a setuid bwrap or
# unprivileged user namespaces enabled (sysctl kernel.unprivileged_userns_clone=1
# / a non-zero user.max_user_namespaces). The network namespace is shared with
# the host, so in SSE mode the server listens directly on the host's interface
# (no port publishing needed).
#
# Usage:
#   ./scripts/run-bwrap.sh                 # stdio (default)
#   MCP_TRANSPORT=sse MCP_PORT=8080 ./scripts/run-bwrap.sh
#   IMAGE=myimage:tag ROOTFS=/path ./scripts/run-bwrap.sh
#
set -euo pipefail

IMAGE="${IMAGE:-chromedp-container-mcp:latest}"
ROOTFS="${ROOTFS:-./bwrap-rootfs}"
RUNTIME="${RUNTIME:-docker}"   # only used to export the rootfs the first time

if [ ! -x "$ROOTFS/usr/local/bin/chromedp-container-mcp" ]; then
  echo "Exporting $IMAGE filesystem to $ROOTFS (one-time) ..." >&2
  mkdir -p "$ROOTFS"
  cid="$("$RUNTIME" create "$IMAGE")"
  "$RUNTIME" export "$cid" | tar -x -C "$ROOTFS"
  "$RUNTIME" rm "$cid" >/dev/null
fi

exec bwrap \
  --ro-bind "$ROOTFS" / \
  --tmpfs /tmp \
  --tmpfs /dev/shm \
  --proc /proc \
  --dev /dev \
  --ro-bind-try /etc/resolv.conf /etc/resolv.conf \
  --unshare-user-try --unshare-pid --unshare-ipc --unshare-uts \
  --die-with-parent --new-session \
  --setenv PATH /headless-shell:/usr/local/bin:/usr/bin:/bin \
  --setenv HOME /tmp \
  --setenv MCP_TRANSPORT "${MCP_TRANSPORT:-stdio}" \
  --setenv MCP_HOST "${MCP_HOST:-0.0.0.0}" \
  --setenv MCP_PORT "${MCP_PORT:-8080}" \
  /usr/local/bin/chromedp-container-mcp "$@"
