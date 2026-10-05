#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
case "${1:-}" in
	--version)
		printf '%s\n' 'CodexBar 0.45.2'
		;;
	usage)
		shift
		provider=''
		while [ "$#" -gt 0 ]; do
			if [ "$1" = '--provider' ] && [ "$#" -gt 1 ]; then
				provider=$2
				shift 2
			else
				shift
			fi
		done
		python3 - "$provider" "$script_dir/usage.json" <<'PY'
import json
import sys

provider, path = sys.argv[1:]
with open(path, encoding="utf-8") as source:
	entries = json.load(source)
if provider and provider != "all":
	entries = [entry for entry in entries if entry.get("provider") == provider]
json.dump(entries, sys.stdout, separators=(",", ":"))
PY
		;;
	*)
		printf 'unexpected fake CodexBar invocation\n' >&2
		exit 2
		;;
esac
