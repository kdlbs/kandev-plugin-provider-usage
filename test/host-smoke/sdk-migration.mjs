// Smoke an installed public-SDK package on a disposable minimum-host instance.
// Configure test/host-smoke/fake-codexbar.sh first; uses no provider accounts.
import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import pathModule from "node:path";
import { fileURLToPath } from "node:url";
const repo = pathModule.resolve(
  pathModule.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const output = pathModule.join(repo, ".build/sdk-migration");
fs.mkdirSync(output, { recursive: true });
const deps =
  process.env.BROWSER_DEPS_DIR || pathModule.join(repo, ".build/browser-tools");
const require = createRequire(pathModule.join(deps, "package.json"));
const { chromium, expect: baseExpect } = require("@playwright/test");
const expect = baseExpect.configure({ timeout: 30_000 });
const base = process.env.SMOKE_BASE_URL || "http://127.0.0.1:43981",
  path = "/api/plugins/kandev-provider-usage";
const checks = [];
async function request(url, method = "GET", body, expected = 200) {
  const r = await fetch(base + url, {
    method,
    signal: AbortSignal.timeout(30_000),
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  console.log(method, url, r.status);
  assert.equal(r.status, expected, `${method} ${url}: ${text.slice(0, 300)}`);
  checks.push({ method, url, status: r.status });
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
assert.equal((await request("/health")).version, "v0.99.0");
assert.equal((await request(path)).status, "active");
for (const key of ["status", "discovery", "providers", "session", "overview"])
  await request(path + "/webhooks/" + key);
await request(path + "/webhooks/cursor-team", "GET", null, 405);
await request(path + "/webhooks/cursor-team", "POST", {}, 400);
await request(path + "/webhooks/cursor-team", "POST", { team_id: "" });
await request(path + "/webhooks/update", "GET", null, 405);
await request(path + "/webhooks/not-declared", "GET", null, 404);
const config = (await request(path + "/config")).config;
await request(
  path,
  "PATCH",
  { config: { ...config, codexbar_poll_minutes: "invalid" } },
  400,
);
assert.match(
  config.codexbar_command,
  /[/\\]fake-codexbar\.sh$/,
  "Configure the synthetic CodexBar fixture first",
);
const browser = await chromium.launch({ headless: true });
const browserChecks = [];
try {
  const page = await browser.newPage({
    viewport: { width: 1280, height: 900 },
  });
  await page.addInitScript(() =>
    localStorage.setItem("kandev.onboarding.completed", "true"),
  );
  await page.goto(base);
  const trigger = page.locator("#provider-usage-topbar");
  await expect(trigger).toHaveCount(1);
  await expect(trigger).toBeVisible();
  await trigger.focus();
  await expect(
    page.locator("[data-provider-usage-panel]").first(),
  ).toBeVisible();
  browserChecks.push("desktop installed main top bar and provider panel");
  await page.screenshot({ path: pathModule.join(output, "desktop-panel.png") });
  await request(path + "/disable", "POST");
  await page.reload();
  await expect(trigger).toHaveCount(0);
  assert.equal((await request(path)).status, "disabled");
  await request(path + "/enable", "POST");
  await page.reload();
  await expect(trigger).toHaveCount(1);
  assert.equal((await request(path)).status, "active");
  assert.deepEqual((await request(path + "/config")).config, config);
  browserChecks.push(
    "disable/re-enable and browser reload remove and restore exactly one contribution, preserves config",
  );
  await page.goto(base + "/settings/plugins/kandev-provider-usage");
  await expect(page.locator("[data-provider-settings]").first()).toBeVisible();
  browserChecks.push("installed desktop provider settings");
  await page.screenshot({
    path: pathModule.join(output, "settings-desktop.png"),
  });
  const phone = await browser.newContext({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
  });
  await phone.addInitScript(() =>
    localStorage.setItem("kandev.onboarding.completed", "true"),
  );
  const phonePage = await phone.newPage();
  await phonePage.goto(base + "/settings/plugins/kandev-provider-usage");
  await expect(
    phonePage.locator("[data-provider-settings]").first(),
  ).toBeVisible();
  assert.equal(
    await phonePage.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
    false,
  );
  await phonePage.screenshot({
    path: pathModule.join(output, "settings-phone.png"),
  });
  browserChecks.push(
    "installed phone provider settings, 390 × 844 touch emulation, no document overflow",
  );
  await phone.close();
  fs.writeFileSync(
    pathModule.join(output, "result.json"),
    JSON.stringify(
      {
        host: "v0.99.0",
        plugin: "0.12.0",
        browser: browser.version(),
        checks,
        browserChecks,
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({
      checks: checks.length,
      browserChecks,
      browser: browser.version(),
    }),
  );
} finally {
  await browser.close();
}
