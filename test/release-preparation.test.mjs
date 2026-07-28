import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workflow = () => readFileSync(new URL("../.github/workflows/prepare-release.yml", import.meta.url), "utf8");

test("release preparation workflow creates a version-bump pull request from manual dispatch", () => {
  const source = workflow();

  assert.match(source, /^on:\n  workflow_dispatch:/m);
  assert.match(source, /bump:\n        description: "Version bump"/);
  assert.match(source, /dry_run:/);
  assert.match(source, /pull-requests: write/);
  assert.match(source, /CHANGELOG\.md/);
  assert.match(source, /BRANCH="release\/\$TAG"/);
  assert.match(source, /gh pr create/);
});
