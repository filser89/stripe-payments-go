#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export GOTOOLCHAIN=go1.27.2
export PATH="$PWD/.tools/bin:$PATH"
# Include new files: foundation verification must work before the first commit.
formatted="$(find cmd internal -name '*.go' -type f -not -path '*/testdata/*' -exec gofmt -l {} +)"
if [[ -n "$formatted" ]]; then printf 'Unformatted Go files:\n%s\n' "$formatted" >&2; exit 1; fi
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
cp -R internal/postgres/queries "$scratch/queries"
sqlc generate
if ! diff -ru "$scratch/queries" internal/postgres/queries; then echo 'Generated SQL code was stale; review regeneration.' >&2; exit 1; fi
./scripts/testdata/workflows.sh
golangci-lint run --timeout=5m ./...
govulncheck ./...
make build
make test
go test -race -count=1 -timeout=5m ./...
