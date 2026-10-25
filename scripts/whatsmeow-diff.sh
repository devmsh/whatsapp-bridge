#!/usr/bin/env bash
# Compare the go.mod-pinned whatsmeow against @latest.
# Prints: commits (GitHub compare API), new exported `func (cli *Client)`
# methods, and new types/events structs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CACHE="$(go env GOMODCACHE)"
PINNED="$(go list -m -f '{{.Version}}' go.mau.fi/whatsmeow)"

echo "==> Downloading pinned (${PINNED}) and latest whatsmeow"
go mod download "go.mau.fi/whatsmeow@${PINNED}"
go mod download go.mau.fi/whatsmeow@latest

LATEST="$(go list -m -f '{{.Version}}' go.mau.fi/whatsmeow@latest)"
PINNED_DIR="${CACHE}/go.mau.fi/whatsmeow@${PINNED}"
LATEST_DIR="${CACHE}/go.mau.fi/whatsmeow@${LATEST}"

echo "Pinned: ${PINNED}"
echo "Latest: ${LATEST}"
echo "Pinned dir: ${PINNED_DIR}"
echo "Latest dir: ${LATEST_DIR}"
echo

if [[ ! -d "${PINNED_DIR}" || ! -d "${LATEST_DIR}" ]]; then
  echo "error: module directories missing after go mod download" >&2
  exit 1
fi

pinned_sha="${PINNED##*-}"
latest_sha="${LATEST##*-}"

echo "==> Commits (${pinned_sha}...${latest_sha})"
compare_url="https://api.github.com/repos/tulir/whatsmeow/compare/${pinned_sha}...${latest_sha}"
if command -v curl >/dev/null 2>&1; then
  if ! curl -fsSL "${compare_url}" | python3 -c '
import json, sys
data = json.load(sys.stdin)
commits = data.get("commits") or []
print("total:", data.get("total_commits", len(commits)))
print("html:", data.get("html_url", ""))
for c in commits:
    sha = c.get("sha", "")[:12]
    msg = (c.get("commit") or {}).get("message", "").splitlines()[0]
    print(f"  {sha}  {msg}")
' 2>/dev/null; then
    echo "GitHub compare API unavailable. Open:"
    echo "  https://github.com/tulir/whatsmeow/compare/${pinned_sha}...${latest_sha}"
  fi
else
  echo "curl not found. Open:"
  echo "  https://github.com/tulir/whatsmeow/compare/${pinned_sha}...${latest_sha}"
fi
echo

list_client_methods() {
  local dir="$1"
  grep -R --include='*.go' --exclude='*_test.go' -h -E '^func \(cli \*Client\) [A-Z]' "${dir}" \
    | sed -E 's/^func \(cli \*Client\) //' \
    | sort -u
}

list_structs() {
  local dir="$1"
  {
    grep -R --include='*.go' --exclude='*_test.go' -h -E '^type [A-Z][A-Za-z0-9_]* struct' "${dir}/types" 2>/dev/null || true
    grep -R --include='*.go' --exclude='*_test.go' -h -E '^type [A-Z][A-Za-z0-9_]* struct' "${dir}/types/events" 2>/dev/null || true
  } | sed -E 's/^type ([A-Z][A-Za-z0-9_]*) struct.*/\1/' | sort -u
}

echo "==> New exported func (cli *Client) methods"
if [[ "${PINNED}" == "${LATEST}" ]]; then
  echo "(pinned is already @latest — none)"
else
  comm -13 <(list_client_methods "${PINNED_DIR}") <(list_client_methods "${LATEST_DIR}") || true
fi
echo

echo "==> New types/events structs"
if [[ "${PINNED}" == "${LATEST}" ]]; then
  echo "(pinned is already @latest — none)"
else
  comm -13 <(list_structs "${PINNED_DIR}") <(list_structs "${LATEST_DIR}") || true
fi
