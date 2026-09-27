#!/bin/sh
set -eu

fail() {
	printf 'release version verification failed: %s\n' "$1" >&2
	exit 1
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	fail 'usage: verify-release-version.sh TAG [PACKAGE_FILE]'
fi

tag=$1
if ! printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	fail "tag is not a release version: $tag"
fi

tag_version=${tag#v}
manifest_version=$(sed -nE 's/^version: "([0-9]+\.[0-9]+\.[0-9]+)"$/\1/p' manifest.yaml)
make_version=$(sed -nE 's/^VERSION := ([0-9]+\.[0-9]+\.[0-9]+)$/\1/p' Makefile)
[ -n "$manifest_version" ] || fail 'manifest.yaml has no SemVer version'
[ -n "$make_version" ] || fail 'Makefile has no SemVer VERSION'
[ "$manifest_version" = "$make_version" ] || fail "manifest version $manifest_version differs from Makefile version $make_version"
[ "$tag_version" = "$manifest_version" ] || fail "tag $tag differs from manifest version $manifest_version"

if [ "$#" -eq 2 ]; then
	package_file=$2
	expected_package=$(make -s package-file)
	[ -f "$package_file" ] || fail "package file not found: $package_file"
	[ "$(basename "$package_file")" = "$expected_package" ] || fail "package filename $(basename "$package_file") differs from $expected_package"
	tmp_dir=$(mktemp -d)
	trap 'rm -rf "$tmp_dir"' EXIT
	tar -xOzf "$package_file" manifest.yaml > "$tmp_dir/manifest.yaml" || fail 'cannot read manifest.yaml from package'
	package_manifest=$(sed -nE 's/^version: "([0-9]+\.[0-9]+\.[0-9]+)"$/\1/p' "$tmp_dir/manifest.yaml")
	[ "$package_manifest" = "$tag_version" ] || fail "package manifest version ${package_manifest:-missing} differs from tag $tag"
fi
