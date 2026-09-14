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
  vm.runInNewContext(readFileSync(new URL("../ui/bundle.js", import.meta.url), "utf8"), {
    window: { registerKandevPlugin(_id, plugin) { definition = plugin; },
      localStorage: { getItem: () => saved, setItem: (_key, value) => { saved = value; } }, innerWidth: 390, innerHeight: 844 },
    setInterval: fn => { intervals.set(++timerID, fn); return timerID; }, clearInterval: id => intervals.delete(id),
    setTimeout: fn => { timeouts.set(++timerID, fn); return timerID; }, clearTimeout: id => timeouts.delete(id),
  });
  definition.initialize({ registerComponent: (name, component) => registrations.set(name, component) }, host);
  assert.ok(registrations.has(slot), `${slot} is registered`);
  function render(nextProps = slotProps) {
    slotProps = nextProps;
    cursor = 0;
    const tree = registrations.get(slot)({ slotProps });
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
    app.render(); app.requests[0].resolve(data); await flush();
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
for (const source of ["settings", "path"]) {
  test(`settings explains external ${source} updates`, async () => {
    const app = mount("plugin-settings"); app.render();
    app.requests[0].resolve({ ...snapshot, codexbar: { installed: true, source } }); await flush();
    assert.equal(nodes(app.render(), n => n.type === "button" && text(n) === "Update CodexBar").length, 0);
    assert.match(text(app.render()), /[Uu]pdate.*(externally|package manager)/);
    app.unmount();
  });
}
