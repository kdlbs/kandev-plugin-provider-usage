#!/bin/sh
set -eu

script_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
case "${1:-}" in
	--version)
		printf '%s\n' 'CodexBar 0.45.2'
		;;
	usage)
		cat "$script_dir/usage.json"
		;;
	*)
		printf 'unexpected fake CodexBar invocation\n' >&2
		exit 2
		;;
esac
