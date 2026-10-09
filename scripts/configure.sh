#!/usr/bin/env bash
# Prepare local credentials before Compose starts the application.
set -euo pipefail
export LC_ALL=C
cd "$(dirname "$0")/.."
umask 077
scratch=""
cleanup() { [[ -z "$scratch" ]] || rm -f "$scratch"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' HUP TERM
fail() { echo "$*" >&2; exit 1; }
[[ ! -L .env ]] || fail 'Edit the linked .env directly before configuring.'
source_file=.env
if [[ ! -f "$source_file" ]]; then source_file=.env.example; fi
[[ -r "$source_file" ]] || fail 'Configuration template is missing or unreadable.'

# Decode the JSON strings emitted by Compose. Credentials accept ASCII only;
# escaped controls/non-ASCII values are invalid and must be entered again.
decode_ascii() {
  local encoded="$1" index character escape digits code octal byte
  REPLY=""
  for ((index=0; index<${#encoded}; index++)); do
    character="${encoded:index:1}"
    if [[ "$character" == '\' ]]; then
      index=$((index+1))
      escape="${encoded:index:1}"
      case "$escape" in
        '"'|'\'|'/') character="$escape" ;;
        u)
          digits="${encoded:index+1:4}"
          [[ "$digits" =~ ^[0-9a-fA-F]{4}$ ]] || return 1
          code=$((16#$digits))
          ((code >= 32 && code <= 126)) || return 1
          printf -v octal '%03o' "$code"
          printf -v byte '%b' "\\$octal"
          character="$byte"
          index=$((index+4))
          ;;
        *) return 1 ;;
      esac
    fi
    REPLY+="$character"
  done
  # Canonical Compose output escapes each literal dollar for re-interpolation.
  REPLY="${REPLY//\$\$/\$}"
}
resolve_credentials() {
  local configuration
  # Compose owns dotenv parsing and interpolation; never execute .env as shell code.
  # The small model resolves only the three application credentials, without a daemon.
  if ! configuration="$(docker compose --project-directory "$PWD" --env-file "$1" -f - config --format json 2>/dev/null <<'MODEL'
name: stripe-payments-configuration
services:
  configuration:
    image: scratch
    environment:
      BASIC_AUTH_USERNAME: "${BASIC_AUTH_USERNAME:-}"
      BASIC_AUTH_PASSWORD: "${BASIC_AUTH_PASSWORD:-}"
      STRIPE_SECRET_KEY: "${STRIPE_SECRET_KEY:-}"
MODEL
  )"; then
    return 1
  fi

  resolved_username="" resolved_password="" resolved_key=""
  local fields=0 line name encoded pattern
  pattern='^[[:space:]]*"(BASIC_AUTH_USERNAME|BASIC_AUTH_PASSWORD|STRIPE_SECRET_KEY)"[[:space:]]*:[[:space:]]*"(.*)"[,]?[[:space:]]*$'
  while IFS= read -r line; do
    if [[ "$line" =~ $pattern ]]; then
      name="${BASH_REMATCH[1]}"
      encoded="${BASH_REMATCH[2]}"
      if ! decode_ascii "$encoded"; then REPLY=""; fi
      case "$name" in
        BASIC_AUTH_USERNAME) resolved_username="$REPLY" ;;
        BASIC_AUTH_PASSWORD) resolved_password="$REPLY" ;;
        STRIPE_SECRET_KEY) resolved_key="$REPLY" ;;
      esac
      fields=$((fields+1))
    fi
  done <<< "$configuration"
  [[ "$fields" == 3 ]] || return 1
  unset REPLY
}
if ! resolve_credentials "$source_file"; then
  fail "Could not read configuration. Check .env syntax and Docker Compose availability."
fi
username="$resolved_username" password="$resolved_password" stripe_key="$resolved_key"

valid_username() {
  [[ ${#1} -ge 1 && ${#1} -le 128 && "$1" != *[!\!-\~]* && "$1" != *:* ]]
}
valid_password() {
  [[ ${#1} -ge 1 && ${#1} -le 256 && "$1" != *[!\ -\~]* ]]
}
valid_stripe_key() {
  [[ "$1" == sk_test_?* && "$1" != *[!\!-\~]* ]]
}
heading_color="" prompt_color="" reset_color=""
if [[ -t 2 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]; then
  heading_color=$'\033[1;36m'
  prompt_color=$'\033[1m'
  reset_color=$'\033[0m'
fi
heading() { printf '\n%s%s%s\n' "$heading_color" "$1" "$reset_color" >&2; }

read_value() {
  local label="$1" sensitive="$2"
  printf '%s%s%s' "$prompt_color" "$label" "$reset_color" >&2
  if [[ "$sensitive" == true ]]; then
    if ! IFS= read -r -s REPLY; then
      printf '\n' >&2
      fail 'Configuration canceled; .env was not changed.'
    fi
    printf '\n' >&2
  elif ! IFS= read -r REPLY; then
    printf '\n' >&2
    fail 'Configuration canceled; .env was not changed.'
  fi
}
change_username=false change_password=false change_key=false
if ! valid_username "$username" || ! valid_password "$password"; then
  heading 'Local application access'
  echo 'Choose a username and password for this local application.' >&2
  echo 'Use them for browser access and authenticated API requests, such as creating orders.' >&2
  echo 'For curl, use --user <app-username> and enter this password when prompted.' >&2
fi
while ! valid_username "$username"; do
  read_value 'App username: ' false
  username="$REPLY"
  change_username=true
  if ! valid_username "$username"; then
    echo 'Use 1–128 printable ASCII characters without spaces or colons.' >&2
  fi
done
while ! valid_password "$password"; do
  read_value 'App password (hidden): ' true
  password="$REPLY"
  change_password=true
  if ! valid_password "$password"; then
    echo 'Use 1–256 printable ASCII characters.' >&2
  fi
done
if ! valid_stripe_key "$stripe_key"; then
  heading 'Stripe sandbox connection'
  echo 'Register or sign in to Stripe: https://dashboard.stripe.com/register' >&2
  echo 'Open API keys: https://dashboard.stripe.com/apikeys' >&2
  echo 'Select your sandbox and copy its Secret key beginning with sk_test_. It will be saved locally in .env.' >&2
fi
while ! valid_stripe_key "$stripe_key"; do
  read_value 'Stripe sandbox secret key (hidden): ' true
  stripe_key="$REPLY"
  change_key=true
  if ! valid_stripe_key "$stripe_key"; then
    echo 'Use a sandbox secret key beginning with sk_test_ and a nonempty printable suffix.' >&2
  fi
done
unset REPLY

if [[ ! -f .env || "$change_username" == true || "$change_password" == true || "$change_key" == true ]]; then
  scratch="$(mktemp ./.env.configure.XXXXXX)"
  write_value() {
    local value="$2"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//\$/\\\$}"
    printf '%s="%s"\n' "$1" "$value" >> "$scratch"
  }
  # Replace only the final assignment of each credential. Earlier assignments
  # can feed unrelated interpolated settings and must retain their position/value.
  quote=""
  scan_quote() {
    local text="$1" index character escaped=false
    for ((index=0; index<${#text}; index++)); do
      character="${text:index:1}"
      if [[ "$escaped" == true ]]; then
        escaped=false
      elif [[ "$character" == '\' ]]; then
        escaped=true
      elif [[ "$character" == "$quote" ]]; then
        quote=""
        return
      fi
    done
  }
  assignment_quote() {
    local value="${1#*=}"
    value="${value#"${value%%[![:space:]]*}"}"
    case "${value:0:1}" in
      "'"|'"') quote="${value:0:1}"; scan_quote "${value:1}" ;;
    esac
  }
  assignment_pattern='^[[:space:]]*(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*='
  last_username=0 last_password=0 last_key=0 line_number=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number+1))
    if [[ -n "$quote" ]]; then scan_quote "$line"; continue; fi
    if [[ "$line" =~ $assignment_pattern ]]; then
      case "${BASH_REMATCH[2]}" in
        BASIC_AUTH_USERNAME) last_username="$line_number" ;;
        BASIC_AUTH_PASSWORD) last_password="$line_number" ;;
        STRIPE_SECRET_KEY) last_key="$line_number" ;;
      esac
      assignment_quote "$line"
    fi
  done < "$source_file"
  quote="" discard_quote=false line_number=0
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_number=$((line_number+1))
    if [[ -n "$quote" ]]; then
      if [[ "$discard_quote" == false ]]; then printf '%s\n' "$line" >> "$scratch"; fi
      scan_quote "$line"
      continue
    fi
    discard_quote=false
    if [[ "$line_number" == "$last_username" ]]; then
      write_value BASIC_AUTH_USERNAME "$username"
      discard_quote=true
    elif [[ "$line_number" == "$last_password" ]]; then
      write_value BASIC_AUTH_PASSWORD "$password"
      discard_quote=true
    elif [[ "$line_number" == "$last_key" ]]; then
      write_value STRIPE_SECRET_KEY "$stripe_key"
      discard_quote=true
    else
      printf '%s\n' "$line" >> "$scratch"
    fi
    if [[ "$line" =~ $assignment_pattern ]]; then assignment_quote "$line"; fi
  done < "$source_file"
  if [[ "$last_username" == 0 ]]; then write_value BASIC_AUTH_USERNAME "$username"; fi
  if [[ "$last_password" == 0 ]]; then write_value BASIC_AUTH_PASSWORD "$password"; fi
  if [[ "$last_key" == 0 ]]; then write_value STRIPE_SECRET_KEY "$stripe_key"; fi
  if ! resolve_credentials "$scratch" ||
    [[ "$resolved_username" != "$username" || "$resolved_password" != "$password" || "$resolved_key" != "$stripe_key" ]]; then
    fail 'Could not save matching credentials; .env was not changed.'
  fi
  mv "$scratch" .env
  scratch=""
  echo 'Configuration saved to .env.'
else
  echo 'Configuration is already set in .env.'
fi
echo 'You can change these values in .env at any time.'
echo 'Run make up again after editing to apply the changes.'
