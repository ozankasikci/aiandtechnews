#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd -P)
server_go_dir=$(CDPATH= cd "$script_dir/.." && pwd -P)
canonical=${CONTRACT_CANONICAL_PATH:-"$server_go_dir/../server/contracts/node/contracts.json"}
mirror=${CONTRACT_MIRROR_PATH:-"$server_go_dir/contracts/fixtures/node-contracts.json"}

if [ ! -f "$canonical" ]; then
  printf 'canonical contract fixture not found: %s\n' "$canonical" >&2
  exit 1
fi

mode=${1:---check}
case "$mode" in
  --check)
    if [ "$#" -ne 0 ] && [ "$#" -ne 1 ]; then
      printf 'usage: %s [--check|--accept]\n' "$0" >&2
      exit 2
    fi
    if [ ! -f "$mirror" ] || ! cmp -s "$canonical" "$mirror"; then
      printf '%s\n' 'contract mirror drift detected.' >&2
      printf '%s\n' 'Review the Node fixture diff first: apps/server/contracts/node/contracts.json' >&2
      printf '%s\n' 'Then explicitly run: apps/server-go/scripts/sync-contracts.sh --accept' >&2
      exit 1
    fi
    printf '%s\n' 'contract mirror matches canonical Node fixture'
    ;;
  --accept)
    [ "$#" -eq 1 ] || { printf 'usage: %s [--check|--accept]\n' "$0" >&2; exit 2; }
    mkdir -p "$(dirname "$mirror")"
    temporary=$(mktemp "${mirror}.tmp.XXXXXX")
    trap 'rm -f "$temporary"' 0 HUP INT TERM
    cp "$canonical" "$temporary"
    chmod 0644 "$temporary"
    mv -f "$temporary" "$mirror"
    trap - 0 HUP INT TERM
    printf '%s\n' 'accepted reviewed canonical Node fixture into Go mirror'
    ;;
  *)
    printf 'usage: %s [--check|--accept]\n' "$0" >&2
    exit 2
    ;;
esac
