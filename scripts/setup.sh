#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
for command in go docker make cc; do command -v "$command" >/dev/null || { echo "Required prerequisite missing: $command" >&2; exit 1; }; done
docker compose version >/dev/null
docker info >/dev/null
export GOTOOLCHAIN=go1.27.2
export GOBIN="$PWD/.tools/bin"
mkdir -p "$GOBIN"
go version
go mod download
install_tool() {
  local binary="$1" package="$2" version="$3"
  if [[ ! -x "$GOBIN/$binary" || ! -f "$GOBIN/$binary.version" || "$(cat "$GOBIN/$binary.version")" != "$version" ]]; then
    go install "$package@$version"
    printf '%s\n' "$version" > "$GOBIN/$binary.version"
  fi
}
install_tool sqlc github.com/sqlc-dev/sqlc/cmd/sqlc v1.31.1
install_tool golangci-lint github.com/golangci/golangci-lint/v2/cmd/golangci-lint v2.14.0
install_tool govulncheck golang.org/x/vuln/cmd/govulncheck v1.8.0
