// Provider Usage — kandev plugin UI bundle.
//
// Hand-written, NO-BUILD plain-JS ES module (shared host React via host.jsx —
// never bundles its own React). Registers ONE component into the "chat-top-bar"
// slot: a gauge in the session top bar that, on hover, opens a panel cycling
// through every provider's subscription utilization — starting with the
// provider that backs the current session.
//
// All data comes from this plugin's Go backend via the "overview" webhook,
// which returns the poller's warm snapshot (every provider) plus the resolved
// current-session provider, computed server-side. This bundle only renders it.

// ---- palette --------------------------------------------------------------
var COLOR = {
  green: "#10b981",
  amber: "#f59e0b",
  red: "#ef4444",
  accent: "#6366f1",
  track: "rgba(148,163,184,0.25)",
};

var PROVIDER_LABELS = {
  claude: "Claude",
  codex: "Codex / OpenAI",
  gemini: "Gemini",
  copilot: "GitHub Copilot",
  cursor: "Cursor",
  grok: "Grok",
  opencode: "OpenCode",
  amp: "Amp",
  augment: "Augment",
};

function providerLabel(id) {
  if (PROVIDER_LABELS[id]) return PROVIDER_LABELS[id];
  var s = String(id || "provider");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// tierColor maps a utilization percentage to a colour using backend thresholds.
function tierColor(pct, warn, high) {
  var w = typeof warn === "number" ? warn : 75;
  var hi = typeof high === "number" ? high : 90;
  if (pct >= hi) return COLOR.red;
  if (pct >= w) return COLOR.amber;
  return COLOR.green;
}

function fmtPct(n) {
  var v = typeof n === "number" && isFinite(n) ? n : 0;
  return (v >= 10 || v === 0 ? Math.round(v) : v.toFixed(1)) + "%";
}

// fmtReset renders a compact relative countdown from reset_at (fallback: the
// provider's own reset string, de-run-together).
function fmtReset(w) {
  if (w.reset_at) {
    var ms = new Date(w.reset_at).getTime() - Date.now();
    if (isFinite(ms)) {
      if (ms <= 0) return "resets now";
      var mins = Math.floor(ms / 60000);
      var days = Math.floor(mins / 1440);
      var hours = Math.floor((mins % 1440) / 60);
      var rem = mins % 60;
      if (days > 0) return "resets in " + days + "d " + hours + "h";
      if (hours > 0) return "resets in " + hours + "h " + rem + "m";
      return "resets in " + rem + "m";
    }
  }
  if (w.reset_description) return String(w.reset_description).replace(/([a-z])([A-Z0-9])/g, "$1 $2").trim();
  return "";
}

// peakPct returns the highest window utilization for a provider usage object.
function peakPct(usage) {
  var windows = (usage && usage.windows) || [];
  var max = 0;
  for (var i = 0; i < windows.length; i++) {
    var p = windows[i].utilization_pct;
    if (typeof p === "number" && p > max) max = p;
  }
  return max;
}

// ---- icons ----------------------------------------------------------------
function gaugeIcon(h, size, color) {
  var s = size || 16;
  return h(
    "svg",
    {
      xmlns: "http://www.w3.org/2000/svg",
      width: s,
      height: s,
      viewBox: "0 0 24 24",
      fill: "none",
      stroke: color || "currentColor",
      strokeWidth: 2,
      strokeLinecap: "round",
      strokeLinejoin: "round",
      "aria-hidden": "true",
    },
    h("path", { d: "M12 14 L16 10" }),
    h("path", { d: "M3.34 19a10 10 0 1 1 17.32 0" }),
  );
}

function chevron(h, dir) {
  return h(
    "svg",
    { xmlns: "http://www.w3.org/2000/svg", width: 16, height: 16, viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: 2.5, strokeLinecap: "round", strokeLinejoin: "round" },
    h("path", { d: dir === "left" ? "M15 18l-6-6 6-6" : "M9 18l6-6-6-6" }),
  );
}

// ---- one utilization window as a labelled bar -----------------------------
function usageBar(h, w, warn, high) {
  var pct = typeof w.utilization_pct === "number" ? w.utilization_pct : 0;
  var color = tierColor(pct, warn, high);
  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "3px" } },
    h(
      "div",
      { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "12px", fontSize: "12px" } },
      h("span", { style: { fontWeight: 600 } }, w.label || "window"),
      h("span", { style: { fontVariantNumeric: "tabular-nums", fontWeight: 700, color: color } }, fmtPct(pct)),
    ),
    h(
      "div",
      { style: { height: "7px", borderRadius: "9999px", background: COLOR.track, overflow: "hidden" } },
      h("div", {
        style: { height: "100%", width: Math.max(0, Math.min(100, pct)) + "%", background: color, borderRadius: "9999px", transition: "width 240ms ease" },
      }),
    ),
    h("div", { style: { fontSize: "10.5px", opacity: 0.6 } }, fmtReset(w)),
  );
}

// ---- one provider's panel -------------------------------------------------
function providerPanel(host, p, warn, high, isCurrent) {
  var h = host.jsx;
  var ui = host.ui;
  var windows = p.windows || [];
  var badges = [];
  if (isCurrent) badges.push(h(ui.Badge, { key: "cur", variant: "default" }, "this session"));
  if (p.plan) badges.push(h(ui.Badge, { key: "plan", variant: "secondary" }, p.plan));
  if (p.source) badges.push(h(ui.Badge, { key: "src", variant: "outline" }, p.source));

  var paceLines = [];
  if (p.pace_primary && p.pace_primary.summary) paceLines.push(p.pace_primary.summary);
  if (p.pace_secondary && p.pace_secondary.summary) paceLines.push(p.pace_secondary.summary);

  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "12px" } },
    h(
      "div",
      { style: { display: "flex", alignItems: "center", gap: "8px", flexWrap: "wrap" } },
      gaugeIcon(h, 16, COLOR.accent),
      h("span", { style: { fontWeight: 700, fontSize: "14px" } }, providerLabel(p.provider)),
      h("span", { style: { display: "inline-flex", gap: "5px", marginLeft: "auto", flexWrap: "wrap", justifyContent: "flex-end" } }, badges),
    ),
    p.detail
      ? h("div", { style: { fontSize: "17px", fontWeight: 700, color: COLOR.accent, fontVariantNumeric: "tabular-nums" } }, p.detail)
      : null,
    windows.length
      ? h(
          "div",
          { style: { display: "flex", flexDirection: "column", gap: "10px" } },
          windows.map(function (w, i) {
            return h("div", { key: i }, usageBar(h, w, warn, high));
          }),
        )
      : p.detail
        ? null
        : h("div", { style: { fontSize: "12px", opacity: 0.6 } }, "No rate-limit windows reported."),
    paceLines.length
      ? h(
          "div",
          { style: { fontSize: "11px", opacity: 0.65, lineHeight: 1.4, borderTop: "1px solid " + COLOR.track, paddingTop: "8px" } },
          paceLines.map(function (line, i) {
            return h("div", { key: i }, line);
          }),
        )
      : null,
  );
}

// reorderProviders puts the current session's provider first.
function reorderProviders(providers, current) {
  var list = (providers || []).slice();
  if (!current) return list;
  var idx = -1;
  for (var i = 0; i < list.length; i++) {
    if (list[i].provider === current) {
      idx = i;
      break;
    }
  }
  if (idx > 0) {
    var found = list.splice(idx, 1)[0];
    list.unshift(found);
  }
  return list;
}

// ---- carousel dots --------------------------------------------------------
function dots(host, count, active, onPick) {
  var h = host.jsx;
  var out = [];
  for (var i = 0; i < count; i++) {
    (function (i) {
      out.push(
        h("button", {
          key: i,
          type: "button",
          "aria-label": "Provider " + (i + 1),
          onClick: function () { onPick(i); },
          onMouseEnter: function () { onPick(i); }, // hover-to-cycle, robust inside a tooltip
          style: {
            width: i === active ? "16px" : "6px",
            height: "6px",
            padding: 0,
            border: "none",
            borderRadius: "9999px",
            cursor: "pointer",
            background: i === active ? COLOR.accent : COLOR.track,
            transition: "width 160ms ease",
          },
        }),
      );
    })(i);
  }
  return h("div", { style: { display: "flex", alignItems: "center", gap: "5px" } }, out);
}

// ---- the panel body (loading / error / setup / carousel) ------------------
function panelBody(host, state, index, setIndex, reload) {
  var h = host.jsx;
  var ui = host.ui;
  var wrap = function (body) {
    return h("div", { style: { display: "flex", flexDirection: "column", gap: "8px", width: "300px" } }, header(), body);
  };
  function header() {
    return h(
      "div",
      { style: { display: "flex", alignItems: "center", gap: "6px", opacity: 0.7, fontSize: "10px", fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase" } },
      gaugeIcon(h, 13, COLOR.accent),
      h("span", null, "Provider usage"),
      h(
        "button",
        {
          type: "button",
          title: "Refresh",
          onClick: reload,
          style: { marginLeft: "auto", border: "none", background: "transparent", cursor: "pointer", color: "inherit", opacity: 0.7, fontSize: "10px", letterSpacing: "0.04em" },
        },
        state.loading ? "…" : "REFRESH",
      ),
    );
  }

  if (state.loading && !state.data) {
    return wrap(h("div", { style: { fontSize: "12px", opacity: 0.7, display: "inline-flex", alignItems: "center", gap: "6px" } }, ui.Spinner ? h(ui.Spinner, { style: { width: "13px", height: "13px" } }) : null, "Loading usage…"));
  }
  if (state.error) return wrap(h("div", { style: { fontSize: "12px", opacity: 0.8 } }, "Couldn't load usage: " + state.error));
  var d = state.data;
  if (!d) return wrap(h("div", { style: { fontSize: "12px", opacity: 0.7 } }, "Hover to load provider usage"));

  var providers = reorderProviders(d.providers, d.current_provider);
  if (!providers.length) {
    var msg =
      d.codexbar && d.codexbar.installed === false
        ? "codexbar isn't available — set it in Settings → Plugins → Provider Usage."
        : "No provider usage yet. Sign in to an agent CLI (Claude, Codex, …) on this machine.";
    return wrap(h("div", { style: { fontSize: "12px", opacity: 0.75, lineHeight: 1.4 } }, msg));
  }

  var i = Math.min(index, providers.length - 1);
  var p = providers[i];
  var isCurrent = d.current_provider && p.provider === d.current_provider;
  var nav = h(
    "div",
    { style: { display: "flex", alignItems: "center", justifyContent: "space-between", gap: "8px", borderTop: "1px solid " + COLOR.track, paddingTop: "8px" } },
    navButton(host, "left", function () { setIndex((i - 1 + providers.length) % providers.length); }, providers.length),
    dots(host, providers.length, i, function (n) { setIndex(n); }),
    navButton(host, "right", function () { setIndex((i + 1) % providers.length); }, providers.length),
  );

  return wrap(
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "12px" } },
      providerPanel(host, p, d.warn_threshold, d.high_threshold, isCurrent),
      providers.length > 1 ? nav : null,
    ),
  );
}

function navButton(host, dir, onClick, count) {
  var h = host.jsx;
  return h(
    "button",
    {
      type: "button",
      "aria-label": dir === "left" ? "Previous provider" : "Next provider",
      disabled: count <= 1,
      onClick: onClick,
      style: {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        width: "22px",
        height: "22px",
        border: "1px solid " + COLOR.track,
        borderRadius: "6px",
        background: "transparent",
        color: "inherit",
        cursor: count <= 1 ? "default" : "pointer",
        opacity: count <= 1 ? 0.4 : 0.8,
      },
    },
    chevron(h, dir),
  );
}

// ---- the chat-top-bar component -------------------------------------------
// Uses the host Tooltip (portaled to <body>, so the top bar's overflow can't
// clip it) as a hoverable container; its content stays open while hovered, so
// the prev/next/dot controls are clickable.
function makeTopBarStatus(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;
  var Tooltip = ui.Tooltip;
  var TooltipTrigger = ui.TooltipTrigger;
  var TooltipContent = ui.TooltipContent;

  return function TopBarUsage(props) {
    var ctx = (props && props.slotProps) || {};
    var stateHook = React.useState({ loading: false, data: null, error: null });
    var state = stateHook[0];
    var setState = stateHook[1];
    var indexHook = React.useState(0);
    var index = indexHook[0];
    var setIndex = indexHook[1];
    var loadedForRef = React.useRef(null);

    function load(force) {
      var active = ctx.activeSessionId || "";
      if (!force && loadedForRef.current === active && state.data) return;
      loadedForRef.current = active;
      setState(function (s) { return { loading: true, data: force ? s.data : null, error: null }; });
      var qs =
        "webhooks/overview?task_id=" +
        encodeURIComponent(ctx.taskId || "") +
        "&active=" +
        encodeURIComponent(active) +
        (force ? "&refresh=1" : "");
      host.api
        .fetch(qs)
        .then(function (r) { return r.json(); })
        .then(function (data) { setState({ loading: false, data: data, error: null }); setIndex(0); })
        .catch(function (err) { setState({ loading: false, data: null, error: String(err && err.message ? err.message : err) }); });
    }

    // Load once the bound session is known, and whenever it changes.
    React.useEffect(function () { loadedForRef.current = null; load(false); }, [ctx.activeSessionId]);

    // Inline: the current provider's peak %, coloured, once loaded.
    var d = state.data;
    var current = d && d.current_provider ? providerByName(d.providers, d.current_provider) : null;
    var inline = null;
    var iconColor;
    if (current && (current.windows || []).length) {
      var pk = peakPct(current);
      iconColor = tierColor(pk, d.warn_threshold, d.high_threshold);
      inline = h("span", { style: { fontSize: "11px", fontWeight: 600, fontVariantNumeric: "tabular-nums", color: iconColor } }, fmtPct(pk));
    }

    return h(
      Tooltip,
      null,
      h(
        TooltipTrigger,
        { asChild: true },
        h(
          ui.Button,
          {
            id: "provider-usage-topbar",
            type: "button",
            variant: "ghost",
            size: inline ? "sm" : "icon",
            className: (inline ? "h-7 px-1.5 gap-1 " : "h-7 w-7 ") + "cursor-pointer text-muted-foreground hover:text-foreground hover:bg-primary/10",
            "aria-label": "Provider usage",
            onMouseEnter: function () { load(false); },
            onFocus: function () { load(false); },
          },
          gaugeIcon(h, 15, iconColor),
          inline,
        ),
      ),
      h(
        TooltipContent,
        { side: "bottom", align: "end", className: "p-3" },
        panelBody(host, state, index, setIndex, function () { load(true); }),
      ),
    );
  };
}

function providerByName(providers, name) {
  var list = providers || [];
  for (var i = 0; i < list.length; i++) if (list[i].provider === name) return list[i];
  return null;
}

// ==========================================================================
window.registerKandevPlugin("kandev-provider-usage", {
  initialize: function (registry, host) {
    registry.registerComponent("chat-top-bar", makeTopBarStatus(host));
  },
});
