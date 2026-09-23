#!/usr/bin/env bash
# Local smoke test of the Go API against a COPY of a database.
#
#   scripts/smoke-local.sh <database> [uploads-dir]
#
# What it does:
#   1. copies <database> into a new temporary directory (sqlite3 .backup when
#      sqlite3 is installed, otherwise cp of the file and its -wal; the
#      original is only read);
#   2. builds cmd/api and cmd/adopt (go build -p 2), unless SMOKE_API_BIN and
#      SMOKE_ADOPT_BIN name prebuilt binaries;
#   3. adopts the copy with cmd/adopt --apply when it has no migration ledger;
#   4. starts the API on 127.0.0.1:<free port> against the copy, with a
#      throwaway JWT secret and an EMPTY environment otherwise (no collector,
#      publisher, IndexNow, S3, Resend, or newsletter secrets), in production
#      mode inside the temporary directory (marked with .technews-production),
#      so no repository checkout is needed at run time;
#   5. checks every public GET the website uses, plus health; with
#      SMOKE_EMAIL and SMOKE_PASSWORD it also logs in and checks the dashboard
#      and newsroom reads;
#   6. stops the server and deletes the temporary directory, also on failure
#      or Ctrl-C.
#
# It only ever talks to 127.0.0.1; there is no host option. Exit status is 0
# when every check passed and non-zero otherwise.
#
# Optional environment:
#   SMOKE_EMAIL, SMOKE_PASSWORD   dashboard login to exercise (sent only to the local server)
#   SMOKE_PORT                    port to use instead of a random free one
#   SMOKE_API_BIN, SMOKE_ADOPT_BIN  prebuilt binaries (skip go build)
#   TZ                            passed through to the server (use production's zone)
set -euo pipefail

usage() {
  printf 'usage: %s <database> [uploads-dir]\n' "$0" >&2
  exit 2
}

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || usage
source_db=$1
source_uploads=${2:-}

fail() {
  printf 'smoke: FAIL: %s\n' "$*" >&2
  exit 1
}

[ -f "$source_db" ] || fail "database $source_db is not a regular file"
if [ -n "$source_uploads" ] && [ ! -d "$source_uploads" ]; then
  fail "uploads directory $source_uploads does not exist"
fi
command -v curl >/dev/null 2>&1 || fail "curl is required"

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd -P)
module_dir=$(CDPATH= cd "$script_dir/.." && pwd -P)

work=$(mktemp -d "${TMPDIR:-/tmp}/technews-smoke.XXXXXX")
server_pid=""
cleanup() {
  status=$?
  if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      kill -0 "$server_pid" 2>/dev/null || break
      sleep 0.5
    done
    kill -KILL "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [ "$status" -ne 0 ] && [ -f "$work/server.log" ]; then
    printf -- '--- last server log lines ---\n' >&2
    tail -n 20 "$work/server.log" >&2 || true
  fi
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# The temporary directory is a self-contained production data root.
: > "$work/.technews-production"
mkdir -p "$work/data" "$work/uploads" "$work/bin"
db="$work/data/technews.db"

printf 'smoke: copying %s\n' "$source_db"
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$source_db" ".backup '$db'" || fail "sqlite3 .backup of $source_db failed"
else
  printf 'smoke: sqlite3 not found; copying the file with cp (the source must not be written meanwhile)\n'
  cp "$source_db" "$db"
  [ ! -f "$source_db-wal" ] || cp "$source_db-wal" "$db-wal"
fi

sample_upload=""
if [ -n "$source_uploads" ]; then
  sample_upload=$(find "$source_uploads" -maxdepth 1 -type f ! -name '.*' \( -iname '*.png' -o -iname '*.jpg' -o -iname '*.jpeg' -o -iname '*.webp' -o -iname '*.gif' \) | head -n 1 || true)
  if [ -n "$sample_upload" ]; then
    cp "$sample_upload" "$work/uploads/"
    sample_upload=$(basename "$sample_upload")
  else
    printf 'smoke: no image in %s; skipping the /uploads check\n' "$source_uploads"
  fi
fi

api_bin=${SMOKE_API_BIN:-}
adopt_bin=${SMOKE_ADOPT_BIN:-}
if [ -z "$api_bin" ] || [ -z "$adopt_bin" ]; then
  command -v go >/dev/null 2>&1 || fail "go is required (or set SMOKE_API_BIN and SMOKE_ADOPT_BIN)"
  printf 'smoke: building cmd/api and cmd/adopt\n'
  (cd "$module_dir" && go build -p 2 -o "$work/bin/api" ./cmd/api && go build -p 2 -o "$work/bin/adopt" ./cmd/adopt) || fail "build failed"
  api_bin="$work/bin/api"
  adopt_bin="$work/bin/adopt"
fi

port_in_use() {
  (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
}
port=${SMOKE_PORT:-}
if [ -z "$port" ]; then
  for _ in $(seq 1 50); do
    candidate=$((20000 + RANDOM % 25000))
    if ! port_in_use "$candidate"; then
      port=$candidate
      break
    fi
  done
  [ -n "$port" ] || fail "no free port found"
else
  port_in_use "$port" && fail "port $port is already in use"
fi
base="http://127.0.0.1:$port"

jwt_secret="smoke-$(date +%s)-$RANDOM-$RANDOM-$RANDOM"
# server_env replaces the calling (sub)shell with the command in a clean
# environment. Run it in a subshell or in the background: with exec, the
# background job's PID ($!) is the process itself, so cleanup can stop it.
server_env() {
  exec env -i PATH="$PATH" HOME="${HOME:-/tmp}" ${TZ:+TZ="$TZ"} \
    APP_ENV=production DATABASE_PATH="$db" UPLOADS_DIR="$work/uploads" \
    SERVER_ADDR="127.0.0.1:$port" JWT_SECRET="$jwt_secret" \
    COLLECTOR_ENABLED=0 PUBLISHER_ENABLED=0 INDEXNOW_ENABLED=0 MEDIA_STORAGE=local \
    "$@"
}

printf 'smoke: adopting the copy if needed\n'
(server_env "$adopt_bin" --apply) > "$work/adopt.log" 2>&1 || { cat "$work/adopt.log" >&2; fail "cmd/adopt failed on the copy"; }
grep -E '^(Result|Adopted|The database is already managed)' "$work/adopt.log" | sed 's/^/smoke:   /' || true

printf 'smoke: starting the API on %s\n' "$base"
server_env "$api_bin" > "$work/server.log" 2>&1 &
server_pid=$!

for _ in $(seq 1 60); do
  kill -0 "$server_pid" 2>/dev/null || fail "the API exited during startup"
  if curl -fsS -o /dev/null --max-time 2 "$base/api/health" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
curl -fsS -o /dev/null --max-time 2 "$base/api/health" 2>/dev/null || fail "the API did not become healthy"

checks=0
body="$work/body"
# check <expected status> <path> [substring the body must contain] [extra curl args...]
check() {
  expected=$1
  path=$2
  needle=${3:-}
  shift 3 || shift $#
  status=$(curl -sS --max-time 15 -o "$body" -w '%{http_code}' "$@" "$base$path") || fail "GET $path: request failed"
  [ "$status" = "$expected" ] || fail "GET $path: HTTP $status, want $expected: $(head -c 200 "$body")"
  if [ -n "$needle" ] && ! grep -qF -- "$needle" "$body"; then
    fail "GET $path: body lacks $needle: $(head -c 200 "$body")"
  fi
  checks=$((checks + 1))
  printf 'smoke: ok   %s %s\n' "$status" "$path"
}
first() {
  # first <json key> <file>: the first string value of "key":"..." in the file
  grep -o "\"$1\":\"[^\"]*\"" "$2" | head -n 1 | sed "s/^\"$1\":\"//; s/\"\$//"
}
first_number() {
  grep -o "\"$1\":[0-9]*" "$2" | head -n 1 | sed "s/^\"$1\"://"
}

check 200 /api/health '"status":"ok"'
check 200 /api/articles '"articles":['
cp "$body" "$work/articles.json"
check 200 '/api/articles?page=1&limit=10' '"totalPages"'
check 200 '/api/articles?page=2&limit=50' '"articles":['
check 200 '/api/articles?search=ai' '"articles":['
check 200 '/api/articles/trending?limit=5' '"articles":['
check 200 /api/categories '"categories":['
category=$(first slug "$body")
if [ -n "$category" ]; then
  check 200 "/api/articles?category=$category" '"articles":['
fi
check 200 /api/authors '"authors":['
check 404 /api/articles/smoke-check-no-such-article-slug ''

slug=$(first slug "$work/articles.json")
id=$(first_number id "$work/articles.json")
if [ -n "$slug" ]; then
  check 200 "/api/articles/$slug" "\"slug\":\"$slug\""
  check 200 "/api/articles/id/$id" "\"id\":$id"
else
  printf 'smoke: no published articles; skipping article detail checks\n'
fi

check 200 '/api/newsletter/editions?limit=30' '"editions":['
edition=$(first edition "$body")
if [ -n "$edition" ]; then
  check 200 "/api/newsletter/editions/$edition" "\"edition\":\"$edition\""
fi

if [ -n "$sample_upload" ]; then
  check 200 "/uploads/$sample_upload" ''
fi

check 401 /api/dashboard/articles ''
check 401 /api/newsroom/overview ''

if [ -n "${SMOKE_EMAIL:-}" ] && [ -n "${SMOKE_PASSWORD:-}" ]; then
  json_escape() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
  umask 077
  printf '{"email":"%s","password":"%s"}' "$(json_escape "$SMOKE_EMAIL")" "$(json_escape "$SMOKE_PASSWORD")" > "$work/login.json"
  status=$(curl -sS --max-time 15 -o "$body" -w '%{http_code}' -H 'Content-Type: application/json' --data-binary "@$work/login.json" "$base/api/auth/login") || fail "login request failed"
  rm -f "$work/login.json"
  [ "$status" = 200 ] || fail "POST /api/auth/login: HTTP $status: $(head -c 200 "$body")"
  token=$(first token "$body")
  [ -n "$token" ] || fail "login response has no token"
  checks=$((checks + 1))
  printf 'smoke: ok   200 POST /api/auth/login\n'
  printf 'header = "Authorization: Bearer %s"\n' "$token" > "$work/auth.curl"
  check 200 /api/auth/me '"email"' -K "$work/auth.curl"
  check 200 /api/dashboard/articles '"articles":[' -K "$work/auth.curl"
  check 200 /api/dashboard/categories '"categories":[' -K "$work/auth.curl"
  check 200 /api/dashboard/media '' -K "$work/auth.curl"
  check 200 /api/dashboard/settings '' -K "$work/auth.curl"
  check 200 /api/newsroom/overview '' -K "$work/auth.curl"
  check 200 /api/newsroom/candidates '' -K "$work/auth.curl"
  check 200 /api/newsroom/settings '' -K "$work/auth.curl"
else
  printf 'smoke: SMOKE_EMAIL/SMOKE_PASSWORD not set; skipping dashboard and newsroom reads\n'
fi

kill -0 "$server_pid" 2>/dev/null || fail "the API exited during the checks"
printf 'smoke: PASS (%d checks against a copy of %s)\n' "$checks" "$source_db"
