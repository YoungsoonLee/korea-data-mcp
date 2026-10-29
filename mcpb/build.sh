#!/usr/bin/env bash
# Packs the GoReleaser binaries in dist/ into an MCP Bundle (korea-data-mcp.mcpb),
# which installs in Claude Desktop with a double click and is what the MCP
# Registry lists. Run after goreleaser: mcpb/build.sh <version> [out]
set -euo pipefail

version=${1:?usage: mcpb/build.sh <version> [out]}
out=${2:-dist/korea-data-mcp.mcpb}
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# One binary per OS: a universal binary for macOS; amd64 for Windows (it also
# runs on Windows on ARM) and Linux.
mkdir -p "$work"/server/{darwin,win32,linux}
cp "$root"/dist/korea-data-mcp-darwin_darwin_all/korea-data-mcp "$work/server/darwin/"
cp "$root"/dist/korea-data-mcp_windows_amd64_v1/korea-data-mcp.exe "$work/server/win32/"
cp "$root"/dist/korea-data-mcp_linux_amd64_v1/korea-data-mcp "$work/server/linux/"
cp "$root/LICENSE" "$work/"
jq --arg v "$version" '.version = $v' "$root/mcpb/manifest.json" > "$work/manifest.json"

npx -y @anthropic-ai/mcpb@2.1.2 pack "$work" "$out"
