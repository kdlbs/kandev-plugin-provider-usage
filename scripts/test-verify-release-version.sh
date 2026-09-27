#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
verify_script=$repo_dir/scripts/verify-release-version.sh
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT
base_version=$(sed -nE 's/^version: "([0-9]+\.[0-9]+\.[0-9]+)"$/\1/p' "$repo_dir/manifest.yaml")
wrong_version=999.999.999
[ "$wrong_version" != "$base_version" ] || wrong_version=999.999.998

make_fixture() {
	fixture=$test_dir/$1
	mkdir -p "$fixture"
	cp "$repo_dir/Makefile" "$repo_dir/manifest.yaml" "$fixture/"
}

expect_failure() {
	name=$1
	fixture=$2
	shift 2
	if (cd "$fixture" && sh "$verify_script" "$@") > "$test_dir/output" 2>&1; then
		printf 'expected release verification to reject %s\n' "$name" >&2
		exit 1
	fi
}

make_fixture valid
(cd "$test_dir/valid" && sh "$verify_script" "v$base_version")

make_fixture wrong-tag
expect_failure 'a tag that differs from manifest.yaml' "$test_dir/wrong-tag" "v$wrong_version"

make_fixture wrong-manifest
sed "s/^version: \"$base_version\"$/version: \"$wrong_version\"/" "$test_dir/wrong-manifest/manifest.yaml" > "$test_dir/wrong-manifest/manifest.next"
mv "$test_dir/wrong-manifest/manifest.next" "$test_dir/wrong-manifest/manifest.yaml"
expect_failure 'a manifest version that differs from Makefile' "$test_dir/wrong-manifest" "v$base_version"

make_fixture wrong-makefile
sed "s/^VERSION := $base_version$/VERSION := $wrong_version/" "$test_dir/wrong-makefile/Makefile" > "$test_dir/wrong-makefile/Makefile.next"
mv "$test_dir/wrong-makefile/Makefile.next" "$test_dir/wrong-makefile/Makefile"
expect_failure 'a Makefile version that differs from manifest.yaml' "$test_dir/wrong-makefile" "v$base_version"

make_fixture wrong-package
mkdir -p "$test_dir/wrong-package/archive"
sed "s/^version: \"$base_version\"$/version: \"$wrong_version\"/" "$test_dir/wrong-package/manifest.yaml" > "$test_dir/wrong-package/archive/manifest.yaml"
package_file=$(cd "$test_dir/wrong-package" && make -s package-file)
tar -czf "$test_dir/wrong-package/$package_file" -C "$test_dir/wrong-package/archive" manifest.yaml
expect_failure 'an archive manifest version that differs from its tag' "$test_dir/wrong-package" "v$base_version" "$package_file"

make_fixture matching-package
mkdir -p "$test_dir/matching-package/archive"
cp "$test_dir/matching-package/manifest.yaml" "$test_dir/matching-package/archive/manifest.yaml"
package_file=$(cd "$test_dir/matching-package" && make -s package-file)
tar -czf "$test_dir/matching-package/$package_file" -C "$test_dir/matching-package/archive" manifest.yaml
(cd "$test_dir/matching-package" && sh "$verify_script" "v$base_version" "$package_file")

expect_failure 'a missing package archive' "$test_dir/matching-package" "v$base_version" missing.tar.gz
cp "$test_dir/matching-package/$package_file" "$test_dir/matching-package/wrong-name.tar.gz"
expect_failure 'a package filename that differs from Makefile' "$test_dir/matching-package" "v$base_version" wrong-name.tar.gz
expect_failure 'a non-version release tag' "$test_dir/valid" "release-$base_version"

printf 'release version negative tests passed\n'
