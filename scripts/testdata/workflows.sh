#!/usr/bin/env bash
# Exercise the real commands with controlled Docker boundary behavior.
set -euo pipefail
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/scripts" "$scratch/fake-bin" "$scratch/state"
cp "$project_root/Makefile" "$scratch/Makefile"
cp "$project_root/scripts/up.sh" "$scratch/scripts/up.sh"
cp "$project_root/scripts/configure.sh" "$scratch/scripts/configure.sh"
cp "$project_root/.env.example" "$scratch/.env.example"
cat > "$scratch/.env" <<'VALUES'
BASIC_AUTH_USERNAME=workflow-user
BASIC_AUTH_PASSWORD=workflow-password
STRIPE_SECRET_KEY=sk_test_workflow
VALUES
export REAL_DOCKER="$(command -v docker)"
unset BASIC_AUTH_USERNAME BASIC_AUTH_PASSWORD STRIPE_SECRET_KEY
export WORKFLOW_STATE="$scratch/state"
cat > "$scratch/fake-bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
state="$WORKFLOW_STATE"
case "$*" in
  *' config --format json'|*' config --quiet') exec "$REAL_DOCKER" "$@" ;;
  'compose build') cp "$state/source" "$state/image" ;;
  'compose stop app') rm -f "$state/app-running" ;;
  'compose run '*migrate)
    case " $* " in *' --build '*) cp "$state/source" "$state/image" ;; esac
    cp "$state/image" "$state/applied"
    ;;
  *'--exit-code-from migrate migrate')
    [[ ! -f "$state/fail-migration" ]] || exit 17
    cp "$state/image" "$state/applied"
    ;;
  *'--wait-timeout 60 app')
    cmp "$state/source" "$state/applied"
    touch "$state/app-running"
    ;;
  'compose logs '*|*'--wait-timeout 60 db') ;;
  *) echo "Unexpected Docker invocation: $*" >&2; exit 99 ;;
esac
DOCKER
chmod +x "$scratch/fake-bin/docker"
export PATH="$scratch/fake-bin:$PATH"
cd "$scratch"
failures=0
mkdir -p .tools/bin
cat > .tools/bin/sqlc <<'GENERATOR'
#!/bin/sh
[ "$1" = generate ] || exit 1
touch .generated
GENERATOR
chmod +x .tools/bin/sqlc
if ! PATH=/usr/bin:/bin make --no-print-directory generate >/dev/null 2>&1 || [[ ! -f .generated ]]; then
  echo 'FAIL: make generate cannot find the project-local sqlc on a clean PATH' >&2
  failures=$((failures+1))
fi
printf 'old SQL\n' > "$WORKFLOW_STATE/image"
printf 'new SQL\n' > "$WORKFLOW_STATE/source"
make --no-print-directory migrate >/dev/null
if ! cmp -s "$WORKFLOW_STATE/source" "$WORKFLOW_STATE/applied"; then
  echo 'FAIL: make migrate applied stale image SQL' >&2
  failures=$((failures+1))
fi
# A failed migration must leave a previously running application stopped.
touch "$WORKFLOW_STATE/app-running" "$WORKFLOW_STATE/fail-migration"
if ./scripts/up.sh >/dev/null 2>&1; then
  echo 'FAIL: make up reported success after failed migration' >&2
  failures=$((failures+1))
fi
if [[ -f "$WORKFLOW_STATE/app-running" ]]; then
  echo 'FAIL: old application remained running after failed migration' >&2
  failures=$((failures+1))
fi
rm -f "$WORKFLOW_STATE/fail-migration"
./scripts/up.sh >/dev/null
if [[ ! -f "$WORKFLOW_STATE/app-running" ]] || ! cmp -s "$WORKFLOW_STATE/source" "$WORKFLOW_STATE/applied"; then
  echo 'FAIL: successful startup did not run the current migrations before the app' >&2
  failures=$((failures+1))
fi
# First startup configures before Docker build/start; canceled input starts nothing.
cp .env "$WORKFLOW_STATE/configured"
rm .env
if ! printf '%s\n' workflow-new-user workflow-new-password sk_test_workflow_new | ./scripts/up.sh > "$WORKFLOW_STATE/configure-output" 2>&1; then
  echo 'FAIL: first startup did not configure and start the application' >&2
  failures=$((failures+1))
fi
if [[ ! -f .env || ! -f "$WORKFLOW_STATE/app-running" ]]; then
  echo 'FAIL: first startup did not save configuration and start the application' >&2
  failures=$((failures+1))
fi
cp "$WORKFLOW_STATE/image" "$WORKFLOW_STATE/before-cancel"
rm .env
if printf '%s\n' workflow-new-user | ./scripts/up.sh > "$WORKFLOW_STATE/configure-output" 2>&1; then
  echo 'FAIL: canceled configuration reported startup success' >&2
  failures=$((failures+1))
fi
if [[ -f .env || ! -f "$WORKFLOW_STATE/app-running" ]] || ! cmp -s "$WORKFLOW_STATE/image" "$WORKFLOW_STATE/before-cancel"; then
  echo 'FAIL: canceled configuration modified files or the running stack' >&2
  failures=$((failures+1))
fi
[[ "$failures" == 0 ]] || exit 1
echo 'Workflow tests passed: current migrations, failed-start isolation, ordered startup.'
