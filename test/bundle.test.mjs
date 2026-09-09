import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

function bundleSource() {
  return readFileSync(new URL("../ui/bundle.js", import.meta.url), "utf8");
}

function statusMeterHelpers(now) {
  const sandbox = {
    window: { registerKandevPlugin() {} },
    Date: now == null ? Date : class extends Date { static now() { return now; } },
    Math,
    String,
    isFinite,
  };
  vm.runInNewContext(
    bundleSource() +
      "\nwindow.__statusMeterTest = {" +
      " statusBarMode: typeof statusBarMode === 'function' ? statusBarMode : null," +
      " statusBarParts: typeof statusBarParts === 'function' ? statusBarParts : null," +
      " usagePopoverPosition: typeof usagePopoverPosition === 'function' ? usagePopoverPosition : null," +
      " statusRefreshDelay: typeof statusRefreshDelay === 'function' ? statusRefreshDelay : null," +
      " statusMeterProviders: typeof statusMeterProviders === 'function' ? statusMeterProviders : null," +
      " statusMeterDetail: typeof statusMeterDetail === 'function' ? statusMeterDetail : null," +
      " statusMeterBarItem: typeof statusMeterBarItem === 'function' ? statusMeterBarItem : null," +
      " statusMeterDrawerRow: statusMeterDrawerRow," +
      " providerIcon: typeof providerIcon === 'function' ? providerIcon : null," +
      " providerPanel: providerPanel," +
      " tabStrip: typeof tabStrip === 'function' ? tabStrip : null," +
      " readTopBarProviderPreference: typeof readTopBarProviderPreference === 'function' ? readTopBarProviderPreference : null," +
      " saveTopBarProviderPreference: typeof saveTopBarProviderPreference === 'function' ? saveTopBarProviderPreference : null," +
      " topBarSelectedProvider: typeof topBarSelectedProvider === 'function' ? topBarSelectedProvider : null," +
      " pillContent: typeof pillContent === 'function' ? pillContent : null," +
      " codexbarRow: typeof codexbarRow === 'function' ? codexbarRow : null," +
      " codexbarProblem: typeof codexbarProblem === 'function' ? codexbarProblem : null" +
      " };",
    sandbox,
  );
  return sandbox.window.__statusMeterTest;
}

function registeredComponentSlots() {
  let plugin;
  const sandbox = {
    window: {
      registerKandevPlugin(_id, definition) {
        plugin = definition;
      },
    },
  };
  vm.runInNewContext(bundleSource(), sandbox);
  const slots = [];
  plugin.initialize(
    {
      registerComponent(slot) {
        slots.push(slot);
      },
    },
    {},
  );
  return slots;
}

function topbarStyleText() {
  let plugin;
  const styles = [];
  const document = {
    head: {
      appendChild(style) {
        styles.push(style);
      },
    },
    createElement() {
      return { id: "", textContent: "", parentNode: null };
    },
    getElementById() {
      return null;
    },
  };
  const sandbox = {
    Date,
    Math,
    String,
    isFinite,
    document,
    window: {
      registerKandevPlugin(_id, definition) {
        plugin = definition;
      },
    },
  };
  vm.runInNewContext(bundleSource(), sandbox);
  plugin.initialize({ registerComponent() {} }, {});
  return styles.map((style) => style.textContent).join("\n");
}

test("status-bar display defaults off and accepts every explicit presentation", () => {
  const { statusBarMode, statusBarParts } = statusMeterHelpers();

  assert.equal(typeof statusBarMode, "function");
  assert.equal(typeof statusBarParts, "function");
  assert.equal(statusBarMode(), "off");
  assert.equal(statusBarMode("off"), "off");
  assert.equal(statusBarMode("percentage"), "percentage");
  assert.equal(statusBarMode("meter"), "meter");
  assert.equal(statusBarMode("both"), "both");
  assert.equal(statusBarMode("unexpected"), "off");
  assert.equal(JSON.stringify(statusBarParts("off")), '{"meter":false,"percentage":false}');
  assert.equal(JSON.stringify(statusBarParts("percentage")), '{"meter":false,"percentage":true}');
  assert.equal(JSON.stringify(statusBarParts("meter")), '{"meter":true,"percentage":false}');
  assert.equal(JSON.stringify(statusBarParts("both")), '{"meter":true,"percentage":true}');
});

test("status meter keeps configured provider order and skips unavailable usage", () => {
  const { statusMeterProviders } = statusMeterHelpers();

  assert.equal(typeof statusMeterProviders, "function");
  const providers = statusMeterProviders({
    pill_providers: ["codex", "missing", "claude"],
    providers: [
      { provider: "claude", windows: [] },
      { provider: "codex", windows: [] },
    ],
  });

  assert.equal(providers.map((provider) => provider.provider).join(","), "codex,claude");
});

test("status meter exposes used, remaining, and reset for its main window", () => {
  const { statusMeterDetail } = statusMeterHelpers();

  assert.equal(typeof statusMeterDetail, "function");
  const detail = statusMeterDetail({
    windows: [
      { label: "5-hour", utilization_pct: 43, reset_description: "ResetsTomorrow" },
      { label: "Scoped", utilization_pct: 99, scoped: true },
    ],
  });

  assert.equal(detail.pct, 43);
  assert.equal(detail.used, "43% used");
  assert.equal(detail.remaining, "57% remaining");
  assert.equal(detail.reset, "Resets Tomorrow");
  assert.equal(detail.label, "5-hour");
});

test("registers provider usage in the global status bar right slot only", () => {
  const slots = registeredComponentSlots();

  assert.ok(slots.includes("app-status-bar-right"));
  assert.ok(!slots.includes("app-status-bar-left"));
});

test("chat topbar control uses desktop and phone geometry", () => {
  const styles = topbarStyleText();

  assert.match(styles, /#provider-usage-topbar[^}]*height:28px/);
  assert.match(
    styles,
    /@media \(max-width:639px\)\{#provider-usage-topbar[^}]*height:44px/,
  );
});

function element(type, props, ...children) {
  return { type, props: props || {}, children };
}

function everyElement(node, predicate, out = []) {
  if (Array.isArray(node)) {
    node.forEach((child) => everyElement(child, predicate, out));
    return out;
  }
  if (!node || typeof node !== "object") return out;
  if (predicate(node)) out.push(node);
  everyElement(node.children, predicate, out);
  return out;
}

function renderedText(node) {
  if (Array.isArray(node)) return node.map(renderedText).join("");
  if (node == null || typeof node === "boolean") return "";
  if (typeof node !== "object") return String(node);
  return renderedText(node.children);
}

const resetCreditNow = Date.parse("2026-09-09T12:00:00Z");

function codexPanel(resetCredits, now = resetCreditNow) {
  const { providerPanel } = statusMeterHelpers(now);
  return providerPanel({ jsx: element }, {
    provider: "codex",
    windows: [{ label: "5-hour", utilization_pct: 42, reset_at: "2026-09-09T15:00:00Z" }],
    reset_credits: resetCredits,
  }, 75, 90, "2026-09-09T12:00:00Z", () => {}, true);
}

test("Codex panel shows available manual resets and sorted expirations", () => {
  const panel = codexPanel({
    available_count: 5,
    credits: [
      { status: "available", expires_at: "2026-09-12T12:00:00Z" },
      { status: "redeemed", expires_at: "2026-09-11T12:00:00Z" },
      { status: "available", expires_at: "2026-09-08T12:00:00Z" },
      { status: "available" },
      { status: "available", expires_at: "2026-09-10T12:00:00Z" },
      { status: "unknown", expires_at: "2026-09-15T12:00:00Z" },
    ],
  });
  const text = renderedText(panel);
  assert.match(text, /Manual resets/);
  assert.match(text, /3 available/);
  const list = everyElement(panel, (node) => node.type === "ul")[0];
  const expiries = everyElement(list, (node) => node.type === "time");
  assert.deepEqual(expiries.map((node) => node.props.dateTime), [
    "2026-09-10T12:00:00Z", "2026-09-12T12:00:00Z",
  ]);
  assert.match(renderedText(list), /1 day.*3 days.*Expiry not reported/);
  assert.match(text, /42% usedresets in 3h 0m/, "quota countdown stays separate");
});

test("Codex panel distinguishes absent reset data from zero and count-only reports", () => {
  assert.doesNotMatch(renderedText(codexPanel()), /Manual resets/);
  assert.match(renderedText(codexPanel({ available_count: 0 })), /Manual resets0 available/);
  const text = renderedText(codexPanel({ available_count: 2 }));
  assert.match(text, /Manual resets2 available/);
  assert.doesNotMatch(text, /Expires|Expiry/, "no invented expirations for a summary-only report");
  for (const available_count of [0, 2]) {
    assert.equal(everyElement(codexPanel({ available_count }), (node) => node.type === "details").length, 0,
      "no empty disclosure when only a count is known");
  }
});

test("Codex panel stops counting a credit at its expiry between polls", () => {
  const resetCredits = {
    available_count: 1,
    credits: [{ status: "available", expires_at: "2026-09-09T13:00:00Z" }],
  };
  assert.match(renderedText(codexPanel(resetCredits)), /1 available.*Next expires.*1 hour/);
  const text = renderedText(codexPanel(resetCredits, Date.parse("2026-09-09T13:00:00Z")));
  assert.match(text, /Manual resets0 available/);
  assert.doesNotMatch(text, /Expires/);
});

test("Codex panel renders Windows reset summary with next expiry", () => {
  const text = renderedText(codexPanel({
    summary: "2 reset credits available",
    next_expires_at: "2026-09-10T12:00:00Z",
  }));
  assert.match(text, /Manual resets2 reset credits availableNext expires.*1 day/);
  assert.doesNotMatch(text, /0% used/);
});

test("phone Status drawer includes Codex manual reset details", () => {
  const { statusMeterDrawerRow } = statusMeterHelpers(resetCreditNow);
  const row = statusMeterDrawerRow({ jsx: element }, {
    provider: "codex",
    windows: [{ label: "weekly", utilization_pct: 42 }],
    reset_credits: {
      available_count: 1,
      credits: [{ status: "available", expires_at: "2026-09-10T12:00:00Z" }],
    },
  }, 75, 90);
  assert.match(renderedText(row), /Manual resets1 availableNext expires.*1 day/);
  assert.match(row.props.className, /items-start/, "provider icon stays aligned with the provider name");
});

test("manual resets keep count and nearest expiry in a collapsed native disclosure", () => {
  const panel = codexPanel({
    available_count: 2,
    credits: [
      { status: "available", expires_at: "2026-10-05T12:00:00Z" },
      { status: "available", expires_at: "2026-09-21T12:00:00Z" },
    ],
  });
  const disclosure = everyElement(panel, (node) => node.type === "details")[0];
  assert.ok(disclosure, "native disclosure provides keyboard/touch semantics");
  assert.ok(!disclosure.props.open, "inventory starts collapsed");
  const summary = everyElement(disclosure, (node) => node.type === "summary")[0];
  assert.ok(summary, "summary remains visible when the inventory is collapsed");
  assert.match(renderedText(summary), /Manual resets2 availableNext expires.*12 days/);
  assert.doesNotMatch(renderedText(summary), /26 days/);
  assert.equal(summary.props.style.minHeight, "44px", "entire summary is a phone-sized tap target");
  const list = everyElement(disclosure, (node) => node.type === "ul")[0];
  assert.equal(everyElement(list, (node) => node.type === "li").length, 2);
  assert.match(renderedText(list), /12 days.*26 days/);
});

test("manual reset details expose localized dates and exact timestamps without granular day countdowns", () => {
  const at = "2026-10-05T04:20:13Z";
  const panel = codexPanel({ credits: [{ status: "available", expires_at: at }] });
  const list = everyElement(panel, (node) => node.type === "ul")[0];
  const time = everyElement(list, (node) => node.type === "time")[0];
  const date = new Date(at);
  const short = date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  const exact = date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "long" });
  assert.equal(time.props.dateTime, at);
  assert.ok(renderedText(list).includes(short), "calendar date is visible");
  assert.ok(renderedText(list).includes(exact), "exact local timestamp is visible on touch too");
  assert.match(renderedText(list), /25 days/);
  assert.doesNotMatch(renderedText(list), /25d|16h|Expires in/);
});

test("manual reset summary uses hours or minutes for imminent expiry", () => {
  for (const [at, expected] of [
    ["2026-09-09T14:30:00Z", /2 hours/],
    ["2026-09-09T12:15:00Z", /15 minutes/],
    ["2026-09-09T12:01:00Z", /1 minute/],
    ["2026-09-09T12:00:30Z", /under 1 minute/],
  ]) {
    const panel = codexPanel({ credits: [{ status: "available", expires_at: at }] });
    const summary = everyElement(panel, (node) => node.type === "summary")[0];
    assert.ok(summary);
    assert.match(renderedText(summary), expected);
  }
});

test("manual reset disclosure stays honest when every expiry is unknown", () => {
  const panel = codexPanel({ credits: [{ status: "available" }, { status: "available", expires_at: "invalid" }] });
  const summary = everyElement(panel, (node) => node.type === "summary")[0];
  assert.ok(summary);
  assert.match(renderedText(summary), /2 availableExpiry not reported/);
  assert.doesNotMatch(renderedText(summary), /Next expires/);
  assert.equal(everyElement(panel, (node) => node.type === "li").length, 2);
  assert.equal(everyElement(panel, (node) => node.type === "time").length, 0);
});

test("manual reset disclosure has scoped focus and reduced-motion styles", () => {
  const styles = topbarStyleText();
  assert.match(styles, /\.provider-reset-credits summary:focus-visible[^}]*outline:/);
  assert.match(styles, /\.provider-reset-credits summary::-webkit-details-marker[^}]*display:none/);
  assert.match(styles, /\.provider-reset-credits details\[open\][^}]*transform:rotate\(180deg\)/);
  assert.match(styles, /@media \(prefers-reduced-motion:reduce\)[\s\S]*transition:none/);
});

test("status-bar item renders percentage, meter, or both literally", () => {
  const { statusMeterBarItem } = statusMeterHelpers();
  const usage = {
    provider: "codex",
    windows: [{ label: "weekly", utilization_pct: 29, reset_description: "6d 16h" }],
  };

  const percentage = statusMeterBarItem({ jsx: element }, usage, 75, 90, "full", "percentage");
  const meter = statusMeterBarItem({ jsx: element }, usage, 75, 90, "full", "meter");
  const both = statusMeterBarItem({ jsx: element }, usage, 75, 90, "full", "both");
  const tracks = (tree) =>
    everyElement(tree, (node) => String(node.props.className || "").includes("bg-muted"));

  assert.match(renderedText(percentage), /29%/);
  assert.equal(tracks(percentage).length, 0);
  assert.doesNotMatch(renderedText(meter), /29%/);
  assert.equal(tracks(meter).length, 1);
  assert.equal(meter.props.style.width, "170px");
  assert.match(renderedText(both), /29%/);
  assert.equal(tracks(both).length, 1);
  assert.equal(both.props.style.width, "170px");
});

test("status-bar popover opens above its bottom-bar trigger", () => {
  const { usagePopoverPosition } = statusMeterHelpers();

  assert.equal(typeof usagePopoverPosition, "function");
  const position = usagePopoverPosition({ top: 876, right: 1300, bottom: 900 }, 1440, 900, "above");
  assert.equal(position.bottom, 24);
  assert.equal(position.left, 1028);
});

test("status contribution retries startup discovery then keeps config live", () => {
  const { statusRefreshDelay } = statusMeterHelpers();

  assert.equal(typeof statusRefreshDelay, "function");
  assert.equal(statusRefreshDelay(null), 2_000);
  assert.equal(statusRefreshDelay({ status_bar_mode: "off" }), 60_000);
  assert.equal(statusRefreshDelay({ status_bar_mode: "both" }), 60_000);
});

test("renders provider brand icons at full foreground brightness and selects tabs only on click", () => {
  const { providerIcon, tabStrip } = statusMeterHelpers();
  const selected = [];

  const icon = providerIcon(element, "codex", 13, "Codex / OpenAI");
  const tabs = tabStrip(
    { jsx: element },
    [{ provider: "claude" }, { provider: "codex" }],
    0,
    "claude",
    (index) => selected.push(index),
  );
  const inactiveProviderTab = tabs.children[0][1];

  assert.equal(icon.props.style.color, "var(--foreground)");
  assert.equal(icon.props.style.opacity, 1);
  assert.equal(inactiveProviderTab.props.style.opacity, 1);
  assert.equal(inactiveProviderTab.props.onMouseEnter, undefined);
  inactiveProviderTab.props.onClick();
  assert.deepEqual(selected, [1]);
});

test("renders the real opencode brand mark, not the monogram fallback", () => {
  const { providerIcon } = statusMeterHelpers();
  const icon = providerIcon(element, "opencodego", 13, "OpenCode");
  assert.equal(icon.props.viewBox, "0 0 24 30", "uses the opencode logo canvas");
  assert.match(icon.props.dangerouslySetInnerHTML.__html, /fill-rule="evenodd"/);
});

test("top-bar provider preference persists and falls back to current then first available", () => {
  const { readTopBarProviderPreference, saveTopBarProviderPreference, topBarSelectedProvider } = statusMeterHelpers();
  const storage = new Map();
  const localStorage = {
    getItem(key) {
      return storage.has(key) ? storage.get(key) : null;
    },
    setItem(key, value) {
      storage.set(key, value);
    },
  };
  const providers = [{ provider: "claude" }, { provider: "grok" }];

  assert.equal(typeof readTopBarProviderPreference, "function");
  assert.equal(typeof saveTopBarProviderPreference, "function");
  assert.equal(typeof topBarSelectedProvider, "function");
  assert.equal(readTopBarProviderPreference(localStorage), "");
  saveTopBarProviderPreference("grok", localStorage);
  assert.equal(readTopBarProviderPreference(localStorage), "grok");
  assert.equal(topBarSelectedProvider(providers, "claude", "grok").provider, "grok");
  assert.equal(topBarSelectedProvider(providers, "claude", "missing").provider, "claude");
  assert.equal(topBarSelectedProvider(providers, "missing", "missing").provider, "claude");
});

test("top-bar pill immediately renders the user-selected provider", () => {
  const { pillContent } = statusMeterHelpers();
  const pill = pillContent(
    { jsx: element },
    {
      pill_providers: ["claude"],
      providers: [
        { provider: "claude", windows: [{ utilization_pct: 17 }] },
        { provider: "grok", windows: [{ utilization_pct: 61 }] },
      ],
    },
    "grok",
  );

  assert.match(renderedText(pill), /61%/);
  assert.doesNotMatch(renderedText(pill), /17%/);
  assert.equal(pill.children[0][0].props.title, "Grok · 61% used");
});

test("uses matching type geometry for percentage and reset countdown", () => {
  const { statusMeterBarItem } = statusMeterHelpers();
  const item = statusMeterBarItem(
    { jsx: element },
    {
      provider: "codex",
      windows: [{ label: "weekly", utilization_pct: 29, reset_description: "6d 16h" }],
    },
    75,
    90,
    "full",
    "both",
  );
  const reset = item.children.at(-1);

  assert.match(reset.props.className, /text-\[11px\]/);
  assert.match(reset.props.className, /font-medium/);
  assert.equal(reset.props.style.display, "inline-flex");
  assert.equal(reset.props.style.alignItems, "center");
  assert.equal(reset.props.style.alignSelf, "stretch");
  assert.equal(reset.props.style.lineHeight, 1);
});

test("settings status card explains a failed codexbar install, not just its error", () => {
  const { codexbarRow } = statusMeterHelpers();
  const row = codexbarRow(element, {
    installed: false,
    source: "download",
    stage: "resolve",
    error: "downloading codexbar: unexpected status 403 fetching https://github.com/…",
    hint: "The one-time download of codexbar v0.45.2 failed. Check this host's outbound access to github.com.",
  });
  const text = renderedText(row);

  assert.match(text, /source: download/, "shows which resolution path was tried");
  assert.match(text, /unexpected status 403/, "keeps the raw cause");
  assert.match(text, /outbound access to github\.com/, "adds the operator's next step");
  assert.match(text, /missing/, "resolution failure reads as missing");
});

test("settings status card separates a broken binary from a missing one", () => {
  const { codexbarRow } = statusMeterHelpers();
  const probeFailure = codexbarRow(element, {
    installed: false,
    source: "settings",
    stage: "probe",
    command: "/opt/codexbar",
    error: "exit status 1: config file is corrupt",
    hint: 'Check that the path exists and is executable, or clear the field in "codexbar · CLI command".',
  });
  const working = codexbarRow(element, {
    installed: true,
    source: "path",
    version: "0.45.2",
    command: "/usr/local/bin/codexbar",
  });

  const failureText = renderedText(probeFailure);
  assert.match(failureText, /not working/, "a binary that exists but fails is not 'missing'");
  assert.match(failureText, /\/opt\/codexbar/, "names the command it ran");
  assert.match(failureText, /config file is corrupt/, "surfaces stderr from the failed probe");

  const workingText = renderedText(working);
  assert.match(workingText, /v0\.45\.2 · source: PATH/);
  assert.match(workingText, /\/usr\/local\/bin\/codexbar/, "shows which binary is in use");
  assert.doesNotMatch(workingText, /missing|not working/);
});

test("hover panel repeats the codexbar failure reason", () => {
  const { codexbarProblem } = statusMeterHelpers();

  assert.equal(codexbarProblem({ installed: true, version: "0.45.2" }), "");
  assert.equal(codexbarProblem(null), "");
  assert.equal(codexbarProblem({ installed: false, error: "raw", hint: "do this" }), "do this");
  assert.equal(codexbarProblem({ installed: false, error: "raw" }), "raw", "falls back to the raw error");
});
