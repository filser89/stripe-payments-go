#!/usr/bin/env bash
# Exercise configuration through Make with the real Compose dotenv parser.
set -euo pipefail
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
unset BASIC_AUTH_USERNAME BASIC_AUTH_PASSWORD STRIPE_SECRET_KEY

fail() { echo "FAIL: $*" >&2; exit 1; }
fixture() {
  work="$scratch/$1"
  mkdir -p "$work/scripts"
  cp "$project_root/Makefile" "$work/Makefile"
  cp "$project_root/.env.example" "$work/.env.example"
  if [[ -f "$project_root/scripts/configure.sh" ]]; then
    cp "$project_root/scripts/configure.sh" "$work/scripts/configure.sh"
  fi
}
configure() { (cd "$work" && make --no-print-directory configure); }
resolved() {
  docker compose --project-directory "$work" --env-file "$work/.env" -f - config --environment <<'MODEL'
name: configuration-test
services:
  configuration:
    image: scratch
    environment:
      USERNAME: "${BASIC_AUTH_USERNAME:-}"
      PASSWORD: "${BASIC_AUTH_PASSWORD:-}"
      KEY: "${STRIPE_SECRET_KEY:-}"
MODEL
}
resolved_json() {
  docker compose --project-directory "$work" --env-file "$work/.env" -f - config --format json <<'MODEL'
name: configuration-test
services:
  configuration:
    image: scratch
    environment:
      PASSWORD: "${BASIC_AUTH_PASSWORD:-}"
      DATABASE_PASSWORD: "${POSTGRES_PASSWORD:-}"
MODEL
}
expect_value() {
  local name="$1" expected="$2" found=false line
  while IFS= read -r line; do
    if [[ "$line" == "$name="* ]]; then
      [[ "${line#*=}" == "$expected" ]] || fail "$name did not round-trip"
      found=true
    fi
  done < <(resolved)
  [[ "$found" == true ]] || fail "$name not resolved"
}
no_leak() {
  local sensitive
  for sensitive in "$@"; do
    if [[ -n "$sensitive" ]] && [[ "$(cat "$work/output")" == *"$sensitive"* ]]; then
      fail 'sensitive input appeared in configuration output'
    fi
  done
}

fixture first-run
if ! printf '%s\n' 'fixture-user' 'fixture-password' 'sk_test_fixture' | configure > "$work/output" 2>&1; then
  fail 'first run must configure successfully through make configure'
fi
expect_value BASIC_AUTH_USERNAME fixture-user
expect_value BASIC_AUTH_PASSWORD fixture-password
expect_value STRIPE_SECRET_KEY sk_test_fixture
# Existing database settings must remain exactly as supplied by the template.
expect_value POSTGRES_PASSWORD replace-with-local-password
permissions="$(stat -c '%a' "$work/.env" 2>/dev/null || stat -f '%Lp' "$work/.env")"
[[ "$permissions" == 600 ]] || fail '.env must be private to its owner'
no_leak fixture-password sk_test_fixture
for message in 'saved to .env' 'change' 'make up'; do
  [[ "$(cat "$work/output")" == *"$message"* ]] || fail "missing configuration message: $message"
done
cp "$work/.env" "$work/before"
configure < /dev/null > "$work/output" 2>&1 || fail 'repeat setup must not request input'
cmp -s "$work/before" "$work/.env" || fail 'repeat setup changed existing configuration'
[[ "$(cat "$work/output")" != *'App username:'* ]] || fail 'repeat setup prompted'
echo 'PASS: first run, private save, instructions, unchanged database and repeat setup'

fixture special-characters
password='  $HOME ${VALUE} $$ # : = <>& "double" '\''single'\'' \\ tail\'
username='fixture$#"\user'
key='sk_test_fixture$#"\suffix'
printf '%s\n' "$username" "$password" "$key" | configure > "$work/output" 2>&1 || fail 'special characters rejected'
expect_value BASIC_AUTH_USERNAME "$username"
expect_value BASIC_AUTH_PASSWORD "$password"
expect_value STRIPE_SECRET_KEY "$key"
no_leak "$password" "$key"
configure < /dev/null > "$work/output" 2>&1 || fail 'special values were not reusable'
echo 'PASS: spaces, dollars, quotes, hashes and backslashes round-trip'

fixture partial
cp "$work/.env.example" "$work/.env"
cat >> "$work/.env" <<'VALUES'
# Preserve these existing settings and comments.
BASIC_AUTH_USERNAME=fixture-existing-user
STRIPE_SECRET_KEY=sk_test_existing
POSTGRES_PASSWORD='existing database password'
APP_PORT=8181
VALUES
printf '%s\n' 'fixture-new-password' | configure > "$work/output" 2>&1 || fail 'partial setup must ask only for password'
expect_value BASIC_AUTH_USERNAME fixture-existing-user
expect_value BASIC_AUTH_PASSWORD fixture-new-password
expect_value STRIPE_SECRET_KEY sk_test_existing
expect_value POSTGRES_PASSWORD 'existing database password'
expect_value APP_PORT 8181
[[ "$(cat "$work/output")" != *'App username:'* ]] || fail 'existing username was prompted'
[[ "$(cat "$work/output")" != *'Stripe sandbox secret key (hidden):'* ]] || fail 'existing Stripe key was prompted'
[[ "$(cat "$work/.env")" == *'# Preserve these existing settings and comments.'* ]] || fail 'unrelated comment removed'
echo 'PASS: only missing fields are requested and unrelated settings survive'

fixture invalid-input
long_user="$(printf '%129s' '' | tr ' ' x)"
long_password="$(printf '%257s' '' | tr ' ' x)"
printf '%s\n' '' 'invalid username' 'invalid:username' "$long_user" 'invalid-é' fixture-valid-user '' $'invalid\tpassword' "$long_password" 'invalid-é' fixture-valid-password sk_live_fixture sk_test_ 'sk_test_with space' sk_test_valid | configure > "$work/output" 2>&1 || fail 'invalid entries must allow correction'
expect_value BASIC_AUTH_USERNAME fixture-valid-user
expect_value BASIC_AUTH_PASSWORD fixture-valid-password
expect_value STRIPE_SECRET_KEY sk_test_valid
no_leak fixture-valid-password sk_live_fixture sk_test_valid
echo 'PASS: invalid and live credentials are rejected without disclosure'

fixture abort-new
if printf '%s\n' fixture-user | configure > "$work/output" 2>&1; then fail 'incomplete input succeeded'; fi
[[ ! -e "$work/.env" ]] || fail 'incomplete input created partial .env'
fixture abort-existing
cp "$work/.env.example" "$work/.env"
cp "$work/.env" "$work/before"
if printf '%s\n' fixture-user | configure > "$work/output" 2>&1; then fail 'incomplete input succeeded'; fi
cmp -s "$work/before" "$work/.env" || fail 'incomplete input changed existing .env'
if compgen -G "$work/.env.configure.*" > /dev/null; then fail 'temporary credential files remain'; fi
echo 'PASS: interrupted input leaves existing configuration intact'

fixture malformed
cp "$work/.env.example" "$work/.env"
printf "BASIC_AUTH_PASSWORD='private-fixture-without-closing-quote\n" >> "$work/.env"
cp "$work/.env" "$work/before"
if configure < /dev/null > "$work/output" 2>&1; then fail 'malformed dotenv succeeded'; fi
cmp -s "$work/before" "$work/.env" || fail 'malformed dotenv was overwritten'
no_leak private-fixture-without-closing-quote
echo 'PASS: malformed dotenv diagnostics remain sanitized'

fixture multiline-invalid
cp "$work/.env.example" "$work/.env"
cat >> "$work/.env" <<'VALUES'
BASIC_AUTH_USERNAME=fixture-user
BASIC_AUTH_PASSWORD='invalid
multiline password'
STRIPE_SECRET_KEY=sk_test_multiline
APP_PORT=8181
VALUES
printf '%s\n' fixture-corrected-password | configure > "$work/output" 2>&1 || fail 'multiline invalid credentials must allow correction'
expect_value BASIC_AUTH_PASSWORD fixture-corrected-password
expect_value STRIPE_SECRET_KEY sk_test_multiline
expect_value APP_PORT 8181
echo 'PASS: correcting a multiline credential removes its complete assignment'

fixture interpolated-existing
cp "$work/.env.example" "$work/.env"
cat >> "$work/.env" <<'VALUES'
BASIC_AUTH_USERNAME=invalid:username
BASIC_AUTH_PASSWORD="${BASIC_AUTH_USERNAME}"
STRIPE_SECRET_KEY=sk_test_interpolated
VALUES
printf '%s\n' fixture-corrected-user | configure > "$work/output" 2>&1 || fail 'interpolated credentials must allow correction'
expect_value BASIC_AUTH_USERNAME fixture-corrected-user
expect_value BASIC_AUTH_PASSWORD invalid:username
expect_value STRIPE_SECRET_KEY sk_test_interpolated
configure < /dev/null > "$work/output" 2>&1 || fail 'corrected configuration cannot be reused'
echo 'PASS: valid resolved credentials survive correction of another field'

fixture unrelated-multiline
cp "$work/.env.example" "$work/.env"
cat >> "$work/.env" <<'VALUES'
BASIC_AUTH_USERNAME=fixture-user
STRIPE_SECRET_KEY=sk_test_multiline_database
POSTGRES_PASSWORD='first line
BASIC_AUTH_PASSWORD=part-of-database-password
last line'
VALUES
printf '%s\n' fixture-app-password | configure > "$work/output" 2>&1 || fail 'unrelated multiline settings must survive configuration'
configuration_json="$(resolved_json)"
[[ "$configuration_json" == *'"PASSWORD": "fixture-app-password"'* ]] || fail 'Basic password was not saved'
[[ "$configuration_json" == *'"DATABASE_PASSWORD": "first line\nBASIC_AUTH_PASSWORD=part-of-database-password\nlast line"'* ]] || fail 'multiline database password was modified'
echo 'PASS: credential-like text in an unrelated multiline value is preserved'

fixture duplicate-credentials
cp "$work/.env.example" "$work/.env"
cat >> "$work/.env" <<'VALUES'
BASIC_AUTH_PASSWORD=old-password
POSTGRES_PASSWORD="${BASIC_AUTH_PASSWORD}"
BASIC_AUTH_PASSWORD=new-password
STRIPE_SECRET_KEY=sk_test_duplicate
VALUES
printf '%s\n' fixture-user | configure > "$work/output" 2>&1 || fail 'duplicate credential assignments must preserve other values'
expect_value BASIC_AUTH_PASSWORD new-password
expect_value POSTGRES_PASSWORD old-password
echo 'PASS: existing assignment order and database interpolation are preserved'

echo 'Configuration tests passed.'
