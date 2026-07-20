// Provider Usage — kandev plugin UI bundle.
//
// Hand-written, NO-BUILD plain-JS ES module (same model as the other kandev
// plugins). Served by kandev from the extracted package at
// GET /api/plugins/kandev-provider-usage/ui/bundle.js and dynamically imported.
// Uses the SHARED host React instance via host.React / host.jsx — never bundles
// its own React.
//
// Two surfaces, both fed by this plugin's Go backend (which shells out to the
// codexbar CLI and returns fully-computed payloads — colors are derived here
// from the backend-supplied warn/high thresholds, everything else is rendered
// as-is):
//   1. Settings page ("Provider Usage") — subscription utilization for every
//      provider, from GET webhooks/providers.
//   2. Chat-bar icon (chat-input-actions slot) — utilization for the provider
//      backing the current session, from GET webhooks/session.

// ---- palette (readable in light & dark) -----------------------------------
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
};

function providerLabel(id) {
  if (PROVIDER_LABELS[id]) return PROVIDER_LABELS[id];
  var s = String(id || "provider");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// tierColor maps a utilization percentage to a colour using the backend-supplied
// thresholds: green below warn, amber at/above warn, red at/above high.
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

// fmtReset renders a compact relative countdown from the reset_at timestamp
// (cleaner than codexbar's space-stripped reset_description, which is only used
// as a fallback when no timestamp is available).
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

// ---- shared: one utilization window as a labelled bar ----------------------
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
      h(
        "span",
        { style: { fontVariantNumeric: "tabular-nums", fontWeight: 700, color: color } },
        fmtPct(pct),
      ),
    ),
    h(
      "div",
      { style: { height: "7px", borderRadius: "9999px", background: COLOR.track, overflow: "hidden" } },
      h("div", {
        style: {
          height: "100%",
          width: Math.max(0, Math.min(100, pct)) + "%",
          background: color,
          borderRadius: "9999px",
          transition: "width 240ms ease",
        },
      }),
    ),
    h("div", { style: { fontSize: "10.5px", opacity: 0.6 } }, fmtReset(w)),
  );
}

// ---- gauge icon ------------------------------------------------------------
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

// ==========================================================================
//  Settings page
// ==========================================================================
function makeSetupNotice(host, status, onRetry, retrying) {
  var h = host.jsx;
  var ui = host.ui;
  return h(
    ui.Alert,
    { id: "provider-usage-setup", variant: "destructive" },
    h(ui.AlertTitle, null, "codexbar CLI is not available"),
    h(
      ui.AlertDescription,
      { style: { display: "flex", flexDirection: "column", gap: "8px" } },
      h(
        "div",
        null,
        "Provider Usage reads subscription utilization from the ",
        h("code", null, "codexbar"),
        " CLI. It couldn't be run",
        status && status.command ? h("span", null, " (tried ", h("code", null, status.command), ").") : ".",
      ),
      status && status.error ? h("div", { style: { opacity: 0.8, fontSize: "12px" } }, status.error) : null,
      h(
        "div",
        { style: { fontSize: "12px", opacity: 0.85 } },
        "Leave the command empty in Settings → Plugins → Provider Usage to auto-download a pinned codexbar build, or set an explicit path.",
      ),
      h(
        ui.Button,
        { variant: "outline", size: "sm", disabled: retrying, onClick: onRetry, style: { cursor: "pointer", alignSelf: "flex-start" } },
        retrying ? "Retrying…" : "Retry",
      ),
    ),
  );
}

function providerCard(host, p, warn, high) {
  var h = host.jsx;
  var ui = host.ui;
  var windows = p.windows || [];
  var badges = [];
  if (p.plan) badges.push(h(ui.Badge, { key: "plan", variant: "secondary" }, p.plan));
  if (p.source) badges.push(h(ui.Badge, { key: "src", variant: "outline" }, p.source));

  var paceLines = [];
  if (p.pace_primary && p.pace_primary.summary) paceLines.push(p.pace_primary.summary);
  if (p.pace_secondary && p.pace_secondary.summary) paceLines.push(p.pace_secondary.summary);

  return h(
    ui.Card,
    { key: p.provider, style: { overflow: "hidden" } },
    h(
      ui.CardHeader,
      null,
      h(
        ui.CardTitle,
        { style: { display: "flex", alignItems: "center", gap: "8px" } },
        gaugeIcon(h, 16, COLOR.accent),
        providerLabel(p.provider),
        h("span", { style: { display: "inline-flex", gap: "6px", marginLeft: "auto" } }, badges),
      ),
    ),
    h(
      ui.CardContent,
      { style: { display: "flex", flexDirection: "column", gap: "14px" } },
      windows.length
        ? windows.map(function (w, i) {
            return h("div", { key: i }, usageBar(h, w, warn, high));
          })
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
    ),
  );
}

function unavailableList(host, entries) {
  if (!entries || !entries.length) return null;
  var h = host.jsx;
  var ui = host.ui;
  return h(
    ui.Card,
    null,
    h(ui.CardHeader, null, h(ui.CardTitle, { style: { fontSize: "13px", opacity: 0.8 } }, "Unavailable providers")),
    h(
      ui.CardContent,
      { style: { display: "flex", flexDirection: "column", gap: "6px" } },
      entries.map(function (e, i) {
        return h(
          "div",
          { key: i, style: { display: "flex", gap: "10px", fontSize: "12px", opacity: 0.75 } },
          h("span", { style: { fontWeight: 600, minWidth: "110px" } }, providerLabel(e.provider)),
          h("span", { style: { opacity: 0.85 } }, e.message),
        );
      }),
    ),
  );
}

function makeProvidersPage(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;

  return function ProvidersPage() {
    var st = React.useState({ loading: true, data: null, error: null });
    var state = st[0];
    var setState = st[1];

    function load(refresh) {
      setState(function (s) {
        return { loading: true, data: refresh ? s.data : null, error: null };
      });
      host.api
        .fetch("webhooks/providers" + (refresh ? "?refresh=1" : ""))
        .then(function (r) {
          return r.json();
        })
        .then(function (data) {
          setState({ loading: false, data: data, error: null });
        })
        .catch(function (err) {
          setState({ loading: false, data: null, error: String(err && err.message ? err.message : err) });
        });
    }

    React.useEffect(function () {
      load(false);
    }, []);

    var data = state.data;
    var warn = data ? data.warn_threshold : 75;
    var high = data ? data.high_threshold : 90;
    var content;

    if (state.loading && !data) {
      content = h(
        "div",
        { style: { display: "flex", flexDirection: "column", gap: "12px" } },
        h(ui.Skeleton, { style: { height: "120px", borderRadius: "12px" } }),
        h(ui.Skeleton, { style: { height: "120px", borderRadius: "12px" } }),
      );
    } else if (state.error) {
      content = h(ui.Alert, { variant: "destructive" }, h(ui.AlertTitle, null, "Couldn't load usage"), h(ui.AlertDescription, null, state.error));
    } else if (data && data.codexbar && data.codexbar.installed === false) {
      content = makeSetupNotice(host, data.codexbar, function () {
        load(true);
      }, state.loading);
    } else {
      var providers = (data && data.providers) || [];
      content = h(
        "div",
        { style: { display: "flex", flexDirection: "column", gap: "14px" } },
        providers.length
          ? h(
              "div",
              { style: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(300px, 1fr))", gap: "14px" } },
              providers.map(function (p) {
                return providerCard(host, p, warn, high);
              }),
            )
          : h(
              ui.Alert,
              null,
              h(ui.AlertTitle, null, "No provider usage yet"),
              h(ui.AlertDescription, null, "codexbar didn't report utilization for any configured provider. Sign in to an agent CLI (e.g. Claude or Codex) on this machine."),
            ),
        unavailableList(host, (data && data.unavailable) || []),
      );
    }

    return h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "16px", padding: "4px 2px 24px" } },
      h(
        "div",
        { style: { display: "flex", alignItems: "center", justifyContent: "space-between", gap: "12px" } },
        h(
          "div",
          { style: { fontSize: "12px", opacity: 0.6 } },
          data && data.generated_at ? "Updated " + new Date(data.generated_at).toLocaleTimeString() : "",
        ),
        h(
          ui.Button,
          { variant: "outline", size: "sm", disabled: state.loading, onClick: function () { load(true); }, style: { cursor: "pointer" } },
          state.loading ? "Refreshing…" : "Refresh",
        ),
      ),
      content,
    );
  };
}

// ==========================================================================
//  Chat-bar icon (chat-input-actions slot)
// ==========================================================================
function popoverHeader(h) {
  return h(
    "div",
    { style: { display: "flex", alignItems: "center", gap: "6px", opacity: 0.7, fontSize: "10px", fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase" } },
    gaugeIcon(h, 13, COLOR.accent),
    h("span", null, "Provider usage"),
  );
}

function stateShell(h, body) {
  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "8px", minWidth: "200px" } },
    popoverHeader(h),
    h("div", { style: { fontSize: "12px", opacity: 0.75, lineHeight: 1.35 } }, body),
  );
}

function sessionPopover(host, state) {
  var h = host.jsx;
  var ui = host.ui;
  if (state.loading) {
    return stateShell(
      h,
      h(
        "span",
        { style: { display: "inline-flex", alignItems: "center", gap: "6px" } },
        ui.Spinner ? h(ui.Spinner, { style: { width: "13px", height: "13px" } }) : null,
        "Loading usage…",
      ),
    );
  }
  if (state.error) return stateShell(h, "Couldn't load usage: " + state.error);
  var d = state.data;
  if (!d) return stateShell(h, "Hover to load provider usage");
  if (d.codexbar && d.codexbar.installed === false) {
    return stateShell(h, "codexbar isn't available — configure it in Settings → Plugins → Provider Usage.");
  }
  if (!d.provider) return stateShell(h, "No known usage provider for this session's agent.");
  if (d.error) return stateShell(h, providerLabel(d.provider) + ": " + d.error);
  if (!d.usage || !(d.usage.windows || []).length) {
    return stateShell(h, "No rate-limit windows reported for " + providerLabel(d.provider) + " yet.");
  }

  var u = d.usage;
  var rows = [
    h(
      "div",
      { style: { display: "flex", alignItems: "center", gap: "8px" } },
      popoverHeader(h),
      h("span", { style: { marginLeft: "auto", fontSize: "11px", fontWeight: 700 } }, providerLabel(u.provider)),
    ),
  ];
  if (u.plan) {
    rows.push(h("div", { style: { fontSize: "10.5px", opacity: 0.6, marginTop: "-2px" } }, "Plan: " + u.plan));
  }
  rows.push(
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "10px", marginTop: "2px" } },
      u.windows.map(function (w, i) {
        return h("div", { key: i }, usageBar(h, w, d.warn_threshold, d.high_threshold));
      }),
    ),
  );
  return h("div", { style: { display: "flex", flexDirection: "column", gap: "6px", minWidth: "210px" } }, rows);
}

function inlinePeak(h, d) {
  if (!d || !d.usage || d.codexbar && d.codexbar.installed === false) return null;
  var pct = peakPct(d.usage);
  return h(
    "span",
    { style: { marginLeft: "3px", fontSize: "11px", fontWeight: 600, fontVariantNumeric: "tabular-nums", color: tierColor(pct, d.warn_threshold, d.high_threshold) } },
    fmtPct(pct),
  );
}

function makeSessionAction(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;
  var Button = ui.Button;
  var Tooltip = ui.Tooltip;
  var TooltipTrigger = ui.TooltipTrigger;
  var TooltipContent = ui.TooltipContent;

  return function ProviderUsageAction(props) {
    var ctx = (props && props.slotProps) || {};
    var st = React.useState({ loading: false, data: null, error: null });
    var state = st[0];
    var setState = st[1];
    var loadedForRef = React.useRef(null);

    function load(force) {
      var active = ctx.activeSessionId;
      if (!active) return;
      if (!force && loadedForRef.current === active && (state.data || state.loading)) return;
      loadedForRef.current = active;
      setState({ loading: true, data: null, error: null });
      var qs =
        "webhooks/session?task_id=" +
        encodeURIComponent(ctx.taskId || "") +
        "&active=" +
        encodeURIComponent(active) +
        (force ? "&refresh=1" : "");
      host.api
        .fetch(qs)
        .then(function (r) {
          return r.json();
        })
        .then(function (data) {
          setState({ loading: false, data: data, error: null });
        })
        .catch(function (err) {
          setState({ loading: false, data: null, error: String(err && err.message ? err.message : err) });
        });
    }

    var loaded = !state.loading && !state.error ? state.data : null;
    var hasUsage = loaded && loaded.usage && (loaded.usage.windows || []).length;
    var iconColor = hasUsage ? tierColor(peakPct(loaded.usage), loaded.warn_threshold, loaded.high_threshold) : undefined;

    return h(
      Tooltip,
      null,
      h(
        TooltipTrigger,
        { asChild: true },
        h(
          Button,
          {
            id: "provider-usage-action",
            type: "button",
            variant: "ghost",
            size: hasUsage ? "sm" : "icon",
            className: (hasUsage ? "h-7 px-1.5 " : "h-7 w-7 ") + "cursor-pointer text-muted-foreground hover:text-foreground hover:bg-primary/10",
            "aria-label": "Provider usage",
            onMouseEnter: function () { load(false); },
            onFocus: function () { load(false); },
            onClick: function () { load(true); },
          },
          gaugeIcon(h, 16, iconColor),
          inlinePeak(h, loaded),
        ),
      ),
      h(TooltipContent, { side: "top", align: "end", className: "px-3 py-2.5" }, sessionPopover(host, state)),
    );
  };
}

// ==========================================================================
window.registerKandevPlugin("kandev-provider-usage", {
  initialize: function (registry, host) {
    registry.registerNavItem({
      id: "provider-usage",
      label: "Provider Usage",
      path: "/provider-usage",
      icon: "chart",
      section: "integrations",
    });
    registry.registerRoute("/provider-usage", makeProvidersPage(host), {
      topbar: { subtitle: "Subscription utilization per provider, via the codexbar CLI" },
    });
    registry.registerComponent("chat-input-actions", makeSessionAction(host));
  },
});
