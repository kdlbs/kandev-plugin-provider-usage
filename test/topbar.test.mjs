import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

// Exercise the registered component with deterministic hooks, requests and timers.
// No provider CLI, browser storage, or real clock is used.
function mount(slot = "main-top-bar", slotProps, saved = "") {
  const hooks = [], pending = [], intervals = new Map(), timeouts = new Map();
  const registrations = new Map();
  let definition, cursor = 0, timerID = 0, writes = 0;
  const React = {
    useState(initial) {
      const i = cursor++;
      if (!(i in hooks)) hooks[i] = typeof initial === "function" ? initial() : initial;
      return [hooks[i], value => { writes++; hooks[i] = typeof value === "function" ? value(hooks[i]) : value; }];
    },
    useRef(initial) {
      const i = cursor++;
      return hooks[i] ||= { current: initial };
    },
    useEffect(effect, deps) {
      const i = cursor++, old = hooks[i];
      if (!old || deps.some((value, n) => value !== old.deps[n])) {
        old?.cleanup?.();
        hooks[i] = { deps };
        pending.push(() => { hooks[i].cleanup = effect(); });
      }
    },
  };
  const requests = [];
  const host = {
    React, jsx: (type, props, ...children) => ({ type, props: props || {}, children }),
    ui: { Button: "button", Card: "article" },
    api: { fetch(url, init) { return new Promise((resolve, reject) => requests.push({ url, init, resolve: (data, ok = true) => resolve({ ok, json: () => Promise.resolve(data) }), reject })); } },
  };
  const source = readFileSync(new URL("../ui/bundle.js", import.meta.url), "utf8");
  vm.runInNewContext(source + (slot === "cursor-team-settings" ? "\nwindow.registerTestComponent(makeCursorTeamSettings);" : ""), {
    fetch: host.api.fetch,
    AbortController,
    window: { registerKandevPlugin(_id, plugin) { definition = plugin; },
      registerTestComponent(factory) { registrations.set(slot, factory(host)); },
      localStorage: { getItem: () => saved, setItem: (_key, value) => { saved = value; } }, innerWidth: 390, innerHeight: 844 },
    setInterval: fn => { intervals.set(++timerID, fn); return timerID; }, clearInterval: id => intervals.delete(id),
    setTimeout: fn => { timeouts.set(++timerID, fn); return timerID; }, clearTimeout: id => timeouts.delete(id),
  });
  definition.initialize({ registerComponent: (name, component) => registrations.set(name, component) }, host);
  assert.ok(registrations.has(slot), `${slot} is registered`);
  function render(nextProps = slotProps) {
    slotProps = nextProps;
    cursor = 0;
    const tree = registrations.get(slot)({ slotProps, onSaved: slotProps?.onSaved });
    if (tree?.props.ref) tree.props.ref.current = { getBoundingClientRect: () => ({ top: 20, bottom: 48, right: 380 }) };
    pending.splice(0).forEach(fn => fn());
    return tree;
  }
  return { render, requests, intervals, timeouts, registrations,
    get saved() { return saved; }, get writes() { return writes; },
    unmount() { hooks.forEach(hook => hook?.cleanup?.()); },
  };
}
function nodes(tree, predicate) {
  if (Array.isArray(tree)) return tree.flatMap(child => nodes(child, predicate));
  if (!tree || typeof tree !== "object") return [];
  return [...(predicate(tree) ? [tree] : []), ...nodes(tree.children, predicate)];
}
function text(tree) {
  if (Array.isArray(tree)) return tree.map(text).join("");
  if (tree == null || typeof tree === "boolean") return "";
  return typeof tree === "object" ? text(tree.children) : String(tree);
}
const snapshot = { current_provider: "", pill_providers: [], providers: [
  { provider: "claude", windows: [{ utilization_pct: 17 }] },
  { provider: "codex", windows: [{ utilization_pct: 61 }] },
] };
const flush = () => new Promise(resolve => setImmediate(resolve));
const trigger = tree => nodes(tree, n => n.props["aria-label"] === "Provider usage")[0];

async function loadSettings(app, data, config = {}) {
  app.render();
  app.requests.find(r => r.url === "webhooks/providers").resolve(data);
  app.requests.find(r => r.url === "config").resolve({ config });
  app.requests.find(r => r.url === "webhooks/discovery").resolve({ detected_providers: data.detected_providers || [] });
  await flush();
  return app.render();
}
const settingsInput = (tree, key) => nodes(tree, n => n.props.id === "provider-setting-" + key)[0];
const saveSettings = tree => nodes(tree, n => n.type === "button" && text(n) === "Save settings")[0];

test("settings is usable while remote usage is still pending and rescan stays local", async () => {
  const app = mount("plugin-settings"); app.render();
  app.requests.find(r => r.url === "webhooks/discovery").resolve({ detected_providers: [{ provider: "codex", via: "cli" }, { provider: "augment", via: "cli" }] });
  app.requests.find(r => r.url === "config").resolve({ config: {} }); await flush();
  let tree = app.render();
  assert.equal(tree.props["data-ready"], true);
  assert.ok(settingsInput(tree, "augment_api_token"));
  assert.match(text(tree), /Checking usage/);
  assert.match(text(tree), /Analytics API token/);
  const count = app.requests.length;
  [...app.intervals.values()][0]();
  assert.equal(app.requests.length, count, "background timers must not queue duplicate slow requests");
  nodes(tree, n => n.type === "button" && text(n) === "Rescan")[0].props.onClick();
  assert.equal(app.requests.at(-1).url, "webhooks/discovery");
  settingsInput(tree, "codexbar_poll_minutes").props.onChange({ target: { value: "7" } });
  tree = app.render();
  assert.equal(saveSettings(tree).props.disabled, false);
  app.unmount();
  const writes = app.writes;
  app.requests.find(r => r.url === "webhooks/providers").resolve(snapshot); await flush();
  assert.equal(app.writes, writes);
});

test("provider sections show quota summaries, reset times and source without losing controls", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, { providers: [{ provider: "codex", plan: "pro", source: "oauth", fetched_at: new Date().toISOString(), windows: [
    { label: "5-hour", utilization_pct: 18, reset_description: "Resets in 3 hours" },
    { label: "weekly", utilization_pct: 37, reset_description: "Resets in 4 days" },
  ] }] });
  const section = nodes(tree, n => n.props["data-provider-settings"] === "codex")[0];
  const summary = nodes(section, n => n.type === "summary")[0];
  assert.match(text(summary), /37% used/);
  assert.match(text(summary), /Pro/);
  assert.match(text(section), /Resets in 4 days/);
  assert.match(text(section), /OAuth/);
  assert.equal(nodes(section, n => n.props.role === "meter").length, 2);
  assert.equal(nodes(section, n => n.props.type === "checkbox").length, 1);
  app.unmount();
});

test("saving releases a slow usage read before the host restarts and ignores its late response", async () => {
  const app = mount("plugin-settings"); app.render();
  const pendingUsage = app.requests.find(r => r.url === "webhooks/providers");
  app.requests.find(r => r.url === "webhooks/discovery").resolve({ detected_providers: [{ provider: "codex", via: "cli" }] });
  app.requests.find(r => r.url === "config").resolve({ config: {} }); await flush();
  settingsInput(app.render(), "codexbar_poll_minutes").props.onChange({ target: { value: "7" } });
  saveSettings(app.render()).props.onClick();
  assert.equal(pendingUsage.init.signal.aborted, true, "a pending webhook must not hold the host's restart lock");
  const read = app.requests.at(-1), count = app.requests.length;
  [...app.intervals.values()][0]();
  assert.equal(app.requests.length, count, "do not start another read during the save");
  pendingUsage.resolve({ providers: [{ provider: "codex", plan: "stale" }] }); await flush();
  assert.doesNotMatch(text(app.render()), /stale/);
  read.resolve({ config: {} }); await flush();
  app.requests.at(-1).resolve({ updated: true }); await flush();
  const refreshed = app.requests.at(-1);
  assert.equal(refreshed.url, "webhooks/providers");
  assert.equal(refreshed.init.signal.aborted, false);
  refreshed.resolve(snapshot); await flush();
  assert.match(text(app.render()), /Settings saved/);
  assert.equal(settingsInput(app.render(), "codexbar_poll_minutes").props.value, "7");
  app.unmount();
});

test("a failed CLI update clears an interrupted usage check and allows retry", async () => {
  const app = mount("plugin-settings");
  await loadSettings(app, { ...snapshot, codexbar: { installed: true, source: "download" } });
  [...app.intervals.values()][0]();
  const pendingUsage = app.requests.at(-1);
  nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar")[0].props.onClick();
  app.requests.at(-1).reject(new Error("Update unavailable")); await flush();
  const tree = app.render();
  assert.equal(nodes(tree, n => n.type === "button" && text(n) === "Refresh usage")[0].props.disabled, false);
  pendingUsage.resolve({ providers: [] }); await flush();
  assert.equal(nodes(app.render(), n => n.props["data-provider-settings"]).length, 2);
  app.unmount();
});

test("settings groups only discovered providers, keeping a signed-out installed provider", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, {
    ...snapshot, detected_providers: [{ provider: "cursor", via: "app" }, { provider: "copilot", via: "cli" }],
    unavailable: [{ provider: "copilot", message: "Sign in to Copilot" }, { provider: "augment", message: "Not installed" }],
  }, { cursor_cookie_header: "********" });
  const sections = nodes(tree, n => n.props["data-provider-settings"]);
  assert.deepEqual(sections.map(n => n.props["data-provider-settings"]), ["claude", "codex", "cursor", "copilot"]);
  assert.equal(nodes(tree, n => n.type === "function").length, 0);
  assert.ok(settingsInput(sections.find(n => n.props["data-provider-settings"] === "cursor"), "cursor_cookie_header"));
  assert.equal(settingsInput(sections.find(n => n.props["data-provider-settings"] === "cursor"), "cursor_agent_keychain").props.value, "off");
  assert.match(text(sections.find(n => n.props["data-provider-settings"] === "copilot")), /Sign in to Copilot/);
  assert.equal(settingsInput(tree, "augment_api_token"), undefined);
  assert.equal(tree.props["data-ready"], true);
  app.unmount();
});

test("settings never adds a Cursor section or team request when Cursor is absent", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, snapshot);
  assert.equal(nodes(tree, n => n.props["data-provider-settings"] === "cursor").length, 0);
  assert.equal(settingsInput(tree, "cursor_cookie_header"), undefined);
  assert.equal(app.requests.some(r => r.url === "webhooks/cursor-teams"), false);
  assert.equal(nodes(tree, n => typeof n.type === "function" && n.type.name === "CursorTeamSettings").length, 0);
  app.unmount();
});

test("configuration save merges edited fields into latest settings and preserves masked secrets", async () => {
  const app = mount("plugin-settings");
  let tree = await loadSettings(app, { ...snapshot, detected_providers: [{ provider: "cursor", via: "app" }] }, {
    cursor_cookie_header: "********", cursor_agent_keychain: "off", augment_api_token: "********", codexbar_poll_minutes: 5, display_status_bar_mode: "off",
  });
  assert.equal(settingsInput(tree, "cursor_cookie_header").props.value, "");
  assert.equal(settingsInput(tree, "cursor_cookie_header").props.placeholder, "Saved securely");
  settingsInput(tree, "cursor_agent_keychain").props.onChange({ target: { value: "on" } });
  settingsInput(tree, "codexbar_poll_minutes").props.onChange({ target: { value: "8" } });
  saveSettings(app.render()).props.onClick();
  saveSettings(tree).props.onClick();
  assert.equal(app.requests.at(-1).url, "config");
  const count = app.requests.length;
  app.requests.at(-1).resolve({ config: {
    cursor_cookie_header: "********", cursor_agent_keychain: "off", augment_api_token: "********", codexbar_poll_minutes: 5, display_status_bar_mode: "both", unrelated: "keep",
  } }); await flush();
  assert.equal(app.requests.length, count + 1);
  const request = app.requests.at(-1);
  assert.equal(request.url, "/api/plugins/kandev-provider-usage");
  assert.equal(request.init.method, "PATCH");
  assert.equal(request.init.credentials, "include");
  assert.deepEqual(JSON.parse(request.init.body).config, {
    cursor_cookie_header: "********", cursor_agent_keychain: "on", augment_api_token: "********", codexbar_poll_minutes: 8, display_status_bar_mode: "both", unrelated: "keep",
  });
  request.resolve({ updated: true }); await flush();
  tree = app.render();
  assert.equal(saveSettings(tree).props.disabled, true);
  assert.equal(settingsInput(tree, "display_status_bar_mode").props.value, "both");
  assert.match(text(tree), /Settings saved/);
  app.unmount();
});

const statusProvider = (tree, id) => nodes(settingsInput(tree, "display_pill_providers"), n => n.type === "input" && n.props.value === id)[0];

test("status provider checkboxes create and persist a subset without losing other settings", async () => {
  const app = mount("plugin-settings");
  const config = { display_status_bar_mode: "both", display_pill_providers: "all", cursor_cookie_header: "********" };
  let tree = await loadSettings(app, snapshot, config);
  assert.equal(statusProvider(tree, "all").props.checked, true);
  statusProvider(tree, "claude").props.onChange({ target: { checked: true } });
  statusProvider(app.render(), "codex").props.onChange({ target: { checked: true } });
  tree = app.render();
  assert.equal(statusProvider(tree, "all").props.checked, false);
  assert.equal(statusProvider(tree, "claude").props.checked, true);
  assert.equal(statusProvider(tree, "codex").props.checked, true);
  saveSettings(tree).props.onClick();
  app.requests.at(-1).resolve({ config: { ...config, augment_api_token: "********" } }); await flush();
  const saved = JSON.parse(app.requests.at(-1).init.body).config;
  assert.equal(saved.display_pill_providers, "claude,codex");
  assert.equal(saved.cursor_cookie_header, "********");
  assert.equal(saved.augment_api_token, "********");
  app.requests.at(-1).resolve({ updated: true }); await flush();
  statusProvider(app.render(), "claude").props.onChange({ target: { checked: false } });
  nodes(app.render(), n => n.type === "button" && text(n) === "Discard changes")[0].props.onClick();
  assert.equal(statusProvider(app.render(), "claude").props.checked, true);
  app.unmount();
  const reopened = mount("plugin-settings");
  tree = await loadSettings(reopened, snapshot, saved);
  assert.equal(statusProvider(tree, "claude").props.checked, true);
  assert.equal(statusProvider(tree, "codex").props.checked, true);
  reopened.unmount();
});

test("status provider choices retain undetected saved providers and support current and all", async () => {
  const app = mount("plugin-settings");
  const config = { display_status_bar_mode: "both", display_pill_providers: "amp,current,codex" };
  let tree = await loadSettings(app, snapshot, config);
  assert.equal(statusProvider(tree, "amp").props.checked, true);
  assert.equal(statusProvider(tree, "current").props.checked, true);
  statusProvider(tree, "claude").props.onChange({ target: { checked: true } });
  saveSettings(app.render()).props.onClick();
  app.requests.at(-1).resolve({ config }); await flush();
  assert.equal(JSON.parse(app.requests.at(-1).init.body).config.display_pill_providers, "amp,current,codex,claude");
  app.requests.at(-1).resolve({ updated: true }); await flush();
  statusProvider(app.render(), "all").props.onChange({ target: { checked: true } });
  tree = app.render();
  assert.equal(statusProvider(tree, "all").props.checked, true);
  assert.equal(statusProvider(tree, "codex").props.checked, false);
  statusProvider(tree, "all").props.onChange({ target: { checked: false } });
  tree = app.render();
  assert.equal(statusProvider(tree, "current").props.checked, true);
  saveSettings(tree).props.onClick();
  app.requests.at(-1).resolve({ config }); await flush();
  assert.equal(JSON.parse(app.requests.at(-1).init.body).config.display_pill_providers, undefined, "empty selection uses the existing current-session default");
  app.unmount();
});

test("settings show Cursor warnings once while retaining other provider warnings", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, { providers: [
    { provider: "cursor", windows: [], detail_warning: "Only selected-team usage is shown." },
    { provider: "codex", windows: [], detail_warning: "Additional quota unavailable." },
  ] });
  for (const message of ["Only selected-team usage is shown.", "Additional quota unavailable."]) {
    assert.equal(nodes(tree, n => n.type === "p" && text(n) === message).length, 1);
  }
  app.unmount();
});

test("settings reports save failures without losing drafts, and permits discarding them", async () => {
  const app = mount("plugin-settings");
  let tree = await loadSettings(app, snapshot);
  settingsInput(tree, "codexbar_poll_minutes").props.onChange({ target: { value: "9" } });
  saveSettings(app.render()).props.onClick();
  app.requests.at(-1).resolve({ config: {} }); await flush();
  app.requests.at(-1).resolve({ error: "Storage unavailable" }, false); await flush();
  tree = app.render();
  assert.match(text(tree), /Storage unavailable/);
  assert.equal(settingsInput(tree, "codexbar_poll_minutes").props.value, "9");
  assert.equal(saveSettings(tree).props.disabled, false);
  nodes(tree, n => n.type === "button" && text(n) === "Discard changes")[0].props.onClick();
  assert.equal(settingsInput(app.render(), "codexbar_poll_minutes").props.value, "5");
  app.unmount();
});

test("saved secrets can be replaced or cleared explicitly and are masked after saving", async () => {
  const app = mount("plugin-settings");
  let tree = await loadSettings(app, { ...snapshot, detected_providers: [{ provider: "cursor", via: "app" }] }, { cursor_cookie_header: "********" });
  settingsInput(tree, "cursor_cookie_header").props.onChange({ target: { value: "new-fixture-cookie" } });
  saveSettings(app.render()).props.onClick();
  app.requests.at(-1).resolve({ config: { cursor_cookie_header: "********", augment_api_token: "********" } }); await flush();
  assert.equal(JSON.parse(app.requests.at(-1).init.body).config.cursor_cookie_header, "new-fixture-cookie");
  app.requests.at(-1).resolve({ updated: true }); await flush();
  tree = app.render();
  assert.equal(settingsInput(tree, "cursor_cookie_header").props.value, "");
  assert.equal(settingsInput(tree, "cursor_cookie_header").props.placeholder, "Saved securely");
  nodes(tree, n => n.type === "button" && text(n) === "Clear saved session cookie")[0].props.onClick();
  saveSettings(app.render()).props.onClick();
  app.requests.at(-1).resolve({ config: { cursor_cookie_header: "********", augment_api_token: "********" } }); await flush();
  const config = JSON.parse(app.requests.at(-1).init.body).config;
  assert.equal(config.cursor_cookie_header, undefined);
  assert.equal(config.augment_api_token, "********");
  app.unmount();
});

test("settings edits survive rescanning and do not accept invalid numeric values", async () => {
  const app = mount("plugin-settings");
  let tree = await loadSettings(app, snapshot);
  settingsInput(tree, "codexbar_poll_minutes").props.onChange({ target: { value: "0" } });
  nodes(app.render(), n => n.type === "button" && text(n) === "Rescan")[0].props.onClick();
  app.requests.at(-1).resolve(snapshot); await flush();
  tree = app.render();
  assert.equal(settingsInput(tree, "codexbar_poll_minutes").props.value, "0");
  const count = app.requests.length;
  saveSettings(tree).props.onClick();
  assert.equal(app.requests.length, count);
  assert.match(text(app.render()), /Enter a valid value/);
  app.unmount();
});

test("provider switches preserve other exclusions and expand an existing poll allowlist", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, { ...snapshot, detected_providers: [{ provider: "cursor", via: "app" }] }, { codexbar_providers: "claude", disabled_providers: "cursor,grok" });
  const cursorSection = nodes(tree, n => n.props["data-provider-settings"] === "cursor")[0];
  const enabled = nodes(cursorSection, n => n.type === "input" && n.props.type === "checkbox")[0];
  assert.equal(enabled.props.checked, false);
  enabled.props.onChange({ target: { checked: true } });
  saveSettings(app.render()).props.onClick();
  app.requests.at(-1).resolve({ config: { codexbar_providers: "claude", disabled_providers: "cursor,grok,amp" } }); await flush();
  const config = JSON.parse(app.requests.at(-1).init.body).config;
  assert.equal(config.disabled_providers, "grok,amp");
  assert.equal(config.codexbar_providers, "claude,cursor");
  app.unmount();
});

test("settings leaves the schema fallback available when the custom editor cannot load", async () => {
  const app = mount("plugin-settings"); app.render();
  app.requests.find(r => r.url === "config").reject(new Error("No connection"));
  app.requests.find(r => r.url === "webhooks/providers").resolve(snapshot); await flush();
  const tree = app.render();
  assert.equal(tree.props["data-ready"], false);
  assert.equal(saveSettings(tree), undefined);
  assert.match(text(tree), /Use the settings form below/);
  app.unmount();
});

test("settings stops a pending read before issuing PATCH if the editor unmounts", async () => {
  const app = mount("plugin-settings");
  const tree = await loadSettings(app, snapshot);
  settingsInput(tree, "codexbar_poll_minutes").props.onChange({ target: { value: "7" } });
  saveSettings(app.render()).props.onClick();
  app.unmount();
  const writes = app.writes, requests = app.requests.length;
  app.requests.at(-1).resolve({ config: {} }); await flush();
  assert.equal(app.writes, writes);
  assert.equal(app.requests.length, requests);
});

test("registers shared, Dockview, status and settings surfaces", () => {
  assert.deepEqual([...mount().registrations.keys()].sort(), ["app-status-bar-right", "chat-top-bar", "main-top-bar", "plugin-settings"]);
});

for (const slotProps of [undefined, { workspaceId: "workspace", currentPage: "kanban", presentation: "desktop" }, { currentPage: "tasks", presentation: "mobile" }]) {
  test(`unscoped toolbar selects saved provider and refreshes (${JSON.stringify(slotProps)})`, async () => {
    const app = mount("main-top-bar", slotProps, "codex");
    app.render();
    assert.equal(app.requests[0].url, "webhooks/overview?task_id=&active=");
    app.requests[0].resolve(snapshot); await flush();
    assert.match(text(app.render()), /61%/);
    trigger(app.render()).props.onClick();
    const opened = app.render();
    nodes(opened, n => n.type === "button" && text(n) === "Refresh")[0].props.onClick();
    assert.equal(app.requests.at(-1).url, "webhooks/overview?task_id=&active=&refresh=1");
    app.requests.at(-1).resolve(snapshot); await flush();
    const tabs = nodes(app.render(), n => n.type === "button" && n.props.title?.includes("Claude"));
    tabs[0].props.onClick();
    assert.equal(app.saved, "claude");
    assert.match(text(app.render()), /17%/);
    app.unmount();
    assert.equal(app.intervals.size, 0);
  });
}

test("unscoped fallback, empty, error and silent poll recovery", async () => {
  const app = mount("main-top-bar", {}, "missing");
  app.render(); app.requests[0].resolve(snapshot); await flush();
  assert.match(text(app.render()), /17%/);
  trigger(app.render()).props.onClick(); app.render();
  [...app.intervals.values()][0](); app.requests.at(-1).reject(new Error("offline")); await flush();
  assert.match(text(app.render()), /17%/);
  nodes(app.render(), n => n.type === "button" && text(n) === "Refresh")[0].props.onClick();
  app.requests.at(-1).reject(new Error("offline")); await flush();
  assert.match(text(app.render()), /Couldn't load usage: offline/);
  [...app.intervals.values()][0](); app.requests.at(-1).resolve({ providers: [] }); await flush();
  assert.match(text(app.render()), /No provider usage yet/);
  app.unmount();
});

test("Dockview retains scoped requests and current-provider fallback", async () => {
  const app = mount("chat-top-bar", { taskId: "task/1", activeSessionId: "session/1" });
  app.render();
  assert.equal(app.requests[0].url, "webhooks/overview?task_id=task%2F1&active=session%2F1");
  app.requests[0].resolve({ ...snapshot, current_provider: "codex" }); await flush();
  assert.match(text(app.render()), /61%/);
  app.unmount();
});

test("mobile menu expands in flow and opens on tap without focus toggling it closed", () => {
  const app = mount("main-top-bar", { presentation: "mobile" });
  let tree = app.render();
  trigger(tree).props.onFocus?.(); tree = app.render();
  trigger(tree).props.onClick(); tree = app.render();
  assert.equal(trigger(tree).props["aria-expanded"], true);
  assert.equal(nodes(tree, n => n.props.style?.position === "fixed").length, 0);
  assert.equal(nodes(tree, n => n.type === "article").length, 1);
  tree.props.onMouseLeave?.();
  assert.equal(app.timeouts.size, 0, "touch panel does not depend on hover timers");
  app.unmount();
});

test("navigation clears hover timeout and ignores an in-flight response", async () => {
  const app = mount();
  const tree = app.render();
  tree.props.onMouseLeave();
  app.unmount();
  assert.equal(app.intervals.size, 0);
  assert.equal(app.timeouts.size, 0);
  const writes = app.writes;
  app.requests[0].resolve(snapshot); await flush();
  assert.equal(app.writes, writes, "unmounted toolbar must not update state");
});


test("changing toolbar context discards stale responses and replaces the poll timer", async () => {
  const app = mount("chat-top-bar", { taskId: "old", activeSessionId: "old-session" });
  app.render();
  app.render({ taskId: "new", activeSessionId: "new-session" });
  assert.equal(app.intervals.size, 1);
  assert.equal(app.requests[1].url, "webhooks/overview?task_id=new&active=new-session");
  app.requests[1].resolve({ ...snapshot, current_provider: "codex" }); await flush();
  app.requests[0].resolve({ ...snapshot, current_provider: "claude" }); await flush();
  assert.match(text(app.render()), /61%/);
  app.unmount();
  assert.equal(app.intervals.size, 0);
});

for (const fails of [false, true]) {
  test(`settings update uses POST, keeps status visible and reports ${fails ? "failure" : "success"}`, async () => {
    const app = mount("plugin-settings");
    const data = { ...snapshot, codexbar: { installed: true, version: "0.45.2", source: "download", command: "/old/CodexBarCLI" } };
    app.render(); app.requests[0].resolve(data);
    app.requests.find(r => r.url === "config").resolve({ config: {} }); await flush();
    const update = nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar")[0];
    assert.ok(update);
    update.props.onClick();
    assert.equal(app.requests.at(-1).url, "webhooks/update");
    assert.equal(app.requests.at(-1).init.method, "POST");
    assert.match(text(app.render()), /Updating/);
    assert.match(text(app.render()), /0.45.2/);
    assert.ok(nodes(app.render(), n => n.type === "button").every(n => n.props.disabled));
    // A timer poll must not clear the busy state or start another request.
    const count = app.requests.length;
    [...app.intervals.values()][0]();
    assert.equal(app.requests.length, count);
    if (fails) {
      app.requests.at(-1).resolve({ error: "checksum mismatch" }, false); await flush();
      assert.match(text(app.render()), /checksum mismatch/);
      assert.match(text(app.render()), /0.45.2/);
    } else {
      app.requests.at(-1).resolve({ codexbar: { ...data.codexbar, version: "0.60.2", command: "/new/CodexBarCLI" }, message: "CodexBar is up to date (0.60.2)." }); await flush();
      assert.match(text(app.render()), /0.60.2/);
      assert.match(text(app.render()), /up to date/);
    }
    assert.equal(nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar")[0].props.disabled, false);
    app.unmount();
  });
}

const teamList = { selected_team_id: "30677937", teams: [
  { id: "30677936", name: "Other team" }, { id: "30677937", name: "Selected team" },
] };
const picker = tree => nodes(tree, n => n.type === "select")[0];
const saveTeam = tree => nodes(tree, n => n.type === "button" && text(n) === "Save team")[0];
const reloadTeams = tree => nodes(tree, n => n.type === "button" && text(n) === "Reload teams")[0];

test("Cursor settings loads team names, preserves the saved choice, and saves only the team ID", async () => {
  let refreshes = 0;
  const app = mount("cursor-team-settings", { onSaved() { refreshes++; } });
  assert.equal(picker(app.render()).props.disabled, true);
  assert.equal(app.requests[0].url, "webhooks/cursor-teams");
  app.requests[0].resolve(teamList); await flush();
  let tree = app.render();
  assert.equal(picker(tree).props.value, "30677937");
  assert.deepEqual(nodes(tree, n => n.type === "option").map(text), ["Account usage (automatic)", "Other team", "Selected team"]);
  assert.equal(saveTeam(tree).props.disabled, true);
  picker(tree).props.onChange({ target: { value: "30677936" } });
  tree = app.render(); saveTeam(tree).props.onClick();
  saveTeam(tree).props.onClick(); // rapid repeat must not send duplicate writes
  assert.equal(app.requests.length, 2);
  assert.equal(app.requests[1].url, "webhooks/cursor-team");
  assert.equal(app.requests[1].init.method, "POST");
  assert.deepEqual(JSON.parse(app.requests[1].init.body), { team_id: "30677936" });
  assert.equal(picker(app.render()).props.disabled, true);
  app.requests[1].resolve({ selected_team_id: "30677936" }); await flush();
  tree = app.render();
  assert.equal(picker(tree).props.value, "30677936");
  assert.equal(saveTeam(tree).props.disabled, true);
  assert.equal(refreshes, 1);
  assert.match(text(tree), /Team saved/);
  app.unmount();
});

test("Cursor settings distinguishes duplicate names and retains an unavailable saved team", async () => {
  const app = mount("cursor-team-settings"); app.render();
  app.requests[0].resolve({ selected_team_id: "99", teams: [{ id: "1", name: "Same" }, { id: "2", name: "Same" }] }); await flush();
  let tree = app.render();
  assert.equal(picker(tree).props.value, "99");
  assert.match(text(tree), /Unavailable team \(99\)/);
  assert.match(text(tree), /Same \(1\)Same \(2\)/);
  assert.equal(saveTeam(tree).props.disabled, true);
  picker(tree).props.onChange({ target: { value: "" } });
  tree = app.render(); saveTeam(tree).props.onClick();
  assert.deepEqual(JSON.parse(app.requests[1].init.body), { team_id: "" });
  app.requests[1].resolve({ selected_team_id: "" }); await flush();
  assert.doesNotMatch(text(app.render()), /Unavailable team/);
  app.unmount();
});

test("Cursor settings handles empty membership, initial auth failure and retry", async () => {
  const app = mount("cursor-team-settings"); app.render();
  app.requests[0].resolve({ error: "Sign in to Cursor." }, false); await flush();
  let tree = app.render();
  assert.match(text(tree), /Sign in to Cursor/);
  assert.equal(picker(tree).props.disabled, true);
  assert.equal(saveTeam(tree).props.disabled, true);
  reloadTeams(tree).props.onClick();
  app.requests[1].resolve({ selected_team_id: "", teams: [] }); await flush();
  tree = app.render();
  assert.match(text(tree), /No teams were found/);
  assert.equal(nodes(tree, n => n.type === "option").length, 1);
  assert.equal(picker(tree).props.value, "");
  app.unmount();
});

test("Cursor settings failed save keeps the draft and saved selection distinct until retry succeeds", async () => {
  const app = mount("cursor-team-settings"); app.render();
  app.requests[0].resolve(teamList); await flush();
  picker(app.render()).props.onChange({ target: { value: "30677936" } });
  saveTeam(app.render()).props.onClick();
  app.requests[1].resolve({ error: "Could not save. Previous selection is unchanged." }, false); await flush();
  let tree = app.render();
  assert.match(text(tree), /Previous selection is unchanged/);
  assert.equal(picker(tree).props.value, "30677936");
  assert.equal(saveTeam(tree).props.disabled, false);
  picker(tree).props.onChange({ target: { value: "30677937" } });
  assert.equal(saveTeam(app.render()).props.disabled, true, "failed write didn't change the saved team");
  picker(app.render()).props.onChange({ target: { value: "30677936" } });
  saveTeam(app.render()).props.onClick();
  app.requests[2].resolve({ selected_team_id: "30677936" }); await flush();
  assert.match(text(app.render()), /Team saved/);
  app.unmount();
});

for (const saving of [false, true]) {
  test(`Cursor settings ignores ${saving ? "save" : "load"} completion after unmount`, async () => {
    const app = mount("cursor-team-settings"); app.render();
    if (saving) {
      app.requests[0].resolve(teamList); await flush();
      picker(app.render()).props.onChange({ target: { value: "30677936" } });
      saveTeam(app.render()).props.onClick();
    }
    app.unmount();
    const writes = app.writes;
    app.requests.at(-1).resolve(saving ? { selected_team_id: "30677936" } : teamList); await flush();
    assert.equal(app.writes, writes);
  });
}
for (const source of ["settings", "path"]) {
  test(`settings explains external ${source} updates`, async () => {
    const app = mount("plugin-settings"); app.render();
    app.requests.find(r => r.url === "config").resolve({ config: {} });
    app.requests[0].resolve({ ...snapshot, codexbar: { installed: true, source } }); await flush();
    assert.equal(nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar").length, 0);
    assert.match(text(app.render()), /[Uu]pdate.*(externally|package manager)/);
    app.unmount();
  });
}

for (const fails of [false, true]) {
  test(`settings ignores update ${fails ? "failure" : "success"} after unmount`, async () => {
    const app = mount("plugin-settings");
    app.render();
    app.requests[0].resolve({ ...snapshot, codexbar: { installed: true, source: "download" } }); await flush();
    app.requests.find(r => r.url === "config").resolve({ config: {} }); await flush();
    nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar")[0].props.onClick();
    app.unmount();
    const writes = app.writes, requests = app.requests.length;
    if (fails) app.requests.at(-1).reject(new Error("offline"));
    else app.requests.at(-1).resolve({ codexbar: { installed: true, source: "download", version: "1.0.0" } });
    await flush();
    assert.equal(app.writes, writes, "no state writes after unmount");
    assert.equal(app.requests.length, requests, "no providers request after unmount");
  });
}
