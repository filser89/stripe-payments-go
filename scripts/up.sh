#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
./scripts/configure.sh
on_failure() { docker compose logs --no-color --tail=80 app migrate >&2 || true; }
trap on_failure ERR
docker compose build
# Recreate the one-shot service so new migrations run even after a successful prior start.
docker compose up -d --wait --wait-timeout 60 db
docker compose stop app
docker compose up --no-deps --force-recreate --exit-code-from migrate migrate
docker compose up -d --no-deps --wait --wait-timeout 60 app
