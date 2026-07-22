import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

function bundleSource() {
  return readFileSync(new URL("../ui/bundle.js", import.meta.url), "utf8");
}

function statusMeterHelpers() {
  const sandbox = {
    window: { registerKandevPlugin() {} },
    Date,
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
      " providerIcon: typeof providerIcon === 'function' ? providerIcon : null," +
      " tabStrip: typeof tabStrip === 'function' ? tabStrip : null" +
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

test("renders provider brand icons at full foreground brightness", () => {
  const { providerIcon, tabStrip } = statusMeterHelpers();

  const icon = providerIcon(element, "codex", 13, "Codex / OpenAI");
  const tabs = tabStrip(
    { jsx: element },
    [{ provider: "claude" }, { provider: "codex" }],
    0,
    "claude",
    () => {},
  );
  const inactiveProviderTab = tabs.children[0][1];

  assert.equal(icon.props.style.color, "var(--foreground)");
  assert.equal(icon.props.style.opacity, 1);
  assert.equal(inactiveProviderTab.props.style.opacity, 1);
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
