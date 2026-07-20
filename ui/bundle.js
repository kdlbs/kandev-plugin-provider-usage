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

// ---- one utilization window: label, thin bar, "X% used · resets …" --------
function cleanWindow(h, w, warn, high, pace) {
  var pct = typeof w.utilization_pct === "number" ? w.utilization_pct : 0;
  var color = tierColor(pct, warn, high);
  var reset = fmtReset(w);
  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "6px" } },
    h("div", { style: { fontWeight: 700, fontSize: "13px" } }, w.label || "window"),
    h(
      "div",
      { style: { height: "4px", borderRadius: "9999px", background: COLOR.track, overflow: "hidden" } },
      h("div", { style: { height: "100%", width: Math.max(0, Math.min(100, pct)) + "%", background: color, borderRadius: "9999px", transition: "width 240ms ease" } }),
    ),
    h(
      "div",
      { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "12px", fontSize: "11px" } },
      h("span", { style: { color: color, fontWeight: 600, fontVariantNumeric: "tabular-nums" } }, fmtPct(pct) + " used"),
      reset ? h("span", { style: { opacity: 0.55 } }, reset) : null,
    ),
    pace ? h("div", { style: { fontSize: "10.5px", opacity: 0.5, marginTop: "-1px" } }, pace) : null,
  );
}

// paceText condenses a codexbar pace side into one muted line.
function paceText(pace) {
  if (!pace || !pace.summary) return "";
  return "Pace: " + pace.summary;
}

// relTime renders an RFC3339 timestamp as "just now" / "3m ago" / "2h ago".
function relTime(iso) {
  if (!iso) return "";
  var ms = Date.now() - new Date(iso).getTime();
  if (!isFinite(ms)) return "";
  var mins = Math.floor(ms / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return mins + "m ago";
  var hrs = Math.floor(mins / 60);
  if (hrs < 24) return hrs + "h ago";
  return Math.floor(hrs / 24) + "d ago";
}

// providerShort is the compact tab label (e.g. "Codex / OpenAI" -> "Codex").
function providerShort(id) {
  return providerLabel(id).split(" / ")[0];
}

// ---- one provider's panel (name + plan + updated, then window bars) --------
function providerPanel(host, p, warn, high, generatedAt, reload) {
  var h = host.jsx;
  var windows = p.windows || [];
  var paceFor = [p.pace_primary, p.pace_secondary];

  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "14px" } },
    // header: name (+ plan right), then "updated · refresh"
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "2px" } },
      h(
        "div",
        { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "10px" } },
        h("span", { style: { fontWeight: 700, fontSize: "15px" } }, providerLabel(p.provider)),
        p.plan ? h("span", { style: { fontSize: "12px", opacity: 0.6, textAlign: "right" } }, p.plan) : null,
      ),
      h(
        "div",
        { style: { fontSize: "11px", opacity: 0.55, display: "flex", gap: "6px", alignItems: "center" } },
        h("span", null, "Updated " + relTime(generatedAt)),
        h("span", { style: { opacity: 0.5 } }, "·"),
        h("button", { type: "button", onClick: reload, style: { border: "none", background: "transparent", padding: 0, cursor: "pointer", color: "inherit", opacity: 0.85, fontSize: "11px" } }, "Refresh"),
      ),
    ),
    // Augment-style raw consumption context, when present.
    p.detail ? h("div", { style: { fontSize: "12px", opacity: 0.7 } }, p.detail) : null,
    // windows
    windows.length
      ? h(
          "div",
          { style: { display: "flex", flexDirection: "column", gap: "14px" } },
          windows.map(function (w, i) {
            return h("div", { key: i }, cleanWindow(h, w, warn, high, i < 2 ? paceText(paceFor[i]) : ""));
          }),
        )
      : p.detail
        ? null
        : h("div", { style: { fontSize: "12px", opacity: 0.55 } }, "No rate-limit windows reported."),
  );
}

// ---- provider tab strip ---------------------------------------------------
function tabStrip(host, providers, active, current, onSelect) {
  var h = host.jsx;
  return h(
    "div",
    { style: { display: "flex", gap: "2px", overflowX: "auto", paddingBottom: "8px", borderBottom: "1px solid " + COLOR.track, marginBottom: "2px" } },
    providers.map(function (p, i) {
      var isActive = i === active;
      var isCurrent = current && p.provider === current;
      return h(
        "button",
        {
          key: p.provider,
          type: "button",
          title: providerLabel(p.provider) + (isCurrent ? " (this session)" : ""),
          onClick: function () { onSelect(i); },
          onMouseEnter: function () { onSelect(i); },
          style: {
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: "4px",
            padding: "5px 9px",
            border: "none",
            borderRadius: "9px",
            cursor: "pointer",
            background: isActive ? "rgba(99,102,241,0.14)" : "transparent",
            color: isActive ? COLOR.accent : "inherit",
            opacity: isActive ? 1 : 0.62,
            minWidth: "46px",
          },
        },
        h("span", { style: { width: "7px", height: "7px", borderRadius: "9999px", background: isActive ? COLOR.accent : (isCurrent ? COLOR.green : COLOR.track) } }),
        h("span", { style: { fontSize: "10.5px", fontWeight: isActive ? 700 : 500, whiteSpace: "nowrap" } }, providerShort(p.provider)),
      );
    }),
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

// ---- the panel body (tabs + selected provider) ----------------------------
function panelBody(host, state, index, setIndex, reload) {
  var h = host.jsx;
  var ui = host.ui;
  var wrap = function (body) {
    return h("div", { style: { display: "flex", flexDirection: "column", gap: "12px", width: "300px" } }, body);
  };

  if (state.loading && !state.data) {
    return wrap(h("div", { style: { fontSize: "12px", opacity: 0.7, display: "inline-flex", alignItems: "center", gap: "6px", padding: "4px 0" } }, ui.Spinner ? h(ui.Spinner, { style: { width: "13px", height: "13px" } }) : null, "Loading usage…"));
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
  return wrap(
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "12px" } },
      providers.length > 1 ? tabStrip(host, providers, i, d.current_provider, setIndex) : null,
      providerPanel(host, p, d.warn_threshold, d.high_threshold, d.generated_at, reload),
    ),
  );
}

// ---- the chat-top-bar component -------------------------------------------
// Self-contained hover panel: its own open state and a position:fixed panel
// (anchored to the trigger's rect) so it works regardless of whether the slot
// sits inside a Radix TooltipProvider, and escapes any overflow clipping on the
// top bar. Clicking the gauge also toggles it.
var PANEL_WIDTH = 324;

function makeTopBarStatus(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;

  return function TopBarUsage(props) {
    var ctx = (props && props.slotProps) || {};
    var stateHook = React.useState({ loading: false, data: null, error: null });
    var state = stateHook[0];
    var setState = stateHook[1];
    var indexHook = React.useState(0);
    var index = indexHook[0];
    var setIndex = indexHook[1];
    var openHook = React.useState(false);
    var open = openHook[0];
    var setOpen = openHook[1];
    var posHook = React.useState({ top: 0, left: 0 });
    var pos = posHook[0];
    var setPos = posHook[1];
    var loadedForRef = React.useRef(null);
    var wrapRef = React.useRef(null);
    var closeTimer = React.useRef(null);

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

    React.useEffect(function () { loadedForRef.current = null; load(false); }, [ctx.activeSessionId]);

    function reposition() {
      var el = wrapRef.current;
      if (!el || !el.getBoundingClientRect) return;
      var r = el.getBoundingClientRect();
      var left = Math.max(8, Math.min(r.right - PANEL_WIDTH, window.innerWidth - PANEL_WIDTH - 8));
      // No vertical gap: the fixed container starts at the trigger's bottom and
      // bridges to the card with transparent padding, so the mouse never leaves
      // the hover area on the way down.
      setPos({ top: r.bottom, left: left });
    }
    function cancelClose() { if (closeTimer.current) { clearTimeout(closeTimer.current); closeTimer.current = null; } }
    function openNow() { cancelClose(); reposition(); setOpen(true); load(false); }
    function scheduleClose() { cancelClose(); closeTimer.current = setTimeout(function () { setOpen(false); }, 260); }

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
      "div",
      { ref: wrapRef, style: { display: "inline-flex" }, onMouseEnter: openNow, onMouseLeave: scheduleClose },
      h(
        ui.Button,
        {
          id: "provider-usage-topbar",
          type: "button",
          variant: "ghost",
          size: inline ? "sm" : "icon",
          className: (inline ? "h-7 px-1.5 gap-1 " : "h-7 w-7 ") + "cursor-pointer text-muted-foreground hover:text-foreground hover:bg-primary/10",
          "aria-label": "Provider usage",
          onFocus: openNow,
          onClick: function () { if (open) { setOpen(false); } else { openNow(); } },
        },
        gaugeIcon(h, 15, iconColor),
        inline,
      ),
      open
        ? h(
            "div",
            {
              onMouseEnter: cancelClose,
              onMouseLeave: scheduleClose,
              style: { position: "fixed", top: pos.top + "px", left: pos.left + "px", zIndex: 9999, paddingTop: "8px" },
            },
            h(
              ui.Card,
              { style: { padding: "12px", boxShadow: "0 12px 34px rgba(0,0,0,0.30)" } },
              panelBody(host, state, index, setIndex, function () { load(true); }),
            ),
          )
        : null,
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
