// Provider Usage — kandev plugin UI bundle.
//
// Hand-written, NO-BUILD plain-JS ES module (shared host React via host.jsx —
// never bundles its own React). Registers three components:
//   • "chat-top-bar" — a gauge in the session top bar that, on hover, opens a
//     panel cycling through every provider's subscription utilization, starting
//     with the provider that backs the current session.
//   • "app-status-bar-right" — an opt-in percentage, meter, or combined usage
//     display adapting to desktop/tablet status and the phone Status drawer.
//   • "plugin-settings" — an integration-status card on the plugin's own
//     settings page (Settings → Plugins → Provider Usage) showing whether the
//     codexbar CLI resolved and whether the Augment API is reachable.
//
// All data comes from this plugin's Go backend: status surfaces read the
// "overview" webhook, the settings card the "providers" webhook. Both return
// the poller's warm snapshot computed server-side — this bundle only renders it.

// AUTO_REFRESH_MS is how often the UI silently re-reads the backend's warm
// snapshot so an open panel / settings card / top-bar pill keeps up with the
// server-side poller (which refreshes every provider, Augment included). The
// re-read hits the cached snapshot (no `refresh=1`), so it's cheap.
var AUTO_REFRESH_MS = 60 * 1000;
var DISCOVERY_RETRY_MS = 2 * 1000;

function statusRefreshDelay(data) {
  return data ? AUTO_REFRESH_MS : DISCOVERY_RETRY_MS;
}

// ---- palette --------------------------------------------------------------
// Calm by default: normal usage is a soft indigo (not green), and only genuinely
// high usage warms to amber / muted coral (never a hard red). Text stays neutral
// so the panel reads calm — the bar fill carries the signal.
var COLOR = {
  base: "#8085e6", // normal — soft indigo
  warn: "#e0a95e", // >= warn — soft amber
  high: "#d97b6c", // >= high — muted coral (not red)
  accent: "#7c82e8", // UI accents (active tab)
  accentBg: "rgba(124,130,232,0.13)",
  track: "rgba(130,140,160,0.16)",
};

// Status display is independent from the always-available session top-bar UI.
// Unknown and missing values stay off so upgrades never add global chrome.
function statusBarMode(value) {
  var normalized = String(value || "").trim().toLowerCase();
  return normalized === "percentage" || normalized === "meter" || normalized === "both" ? normalized : "off";
}

function statusBarParts(value) {
  var mode = statusBarMode(value);
  return {
    meter: mode === "meter" || mode === "both",
    percentage: mode === "percentage" || mode === "both",
  };
}

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

// Monogram fallback only — used for a provider with no real brand mark in
// PROVIDER_SVG below. A small rounded square in a brand-ish hue.
var PROVIDER_ICON = {
  claude: { mono: "Cl", bg: "#d97757" },
  codex: { mono: "Cx", bg: "#10a37f" },
  gemini: { mono: "Ge", bg: "#4285f4" },
  grok: { mono: "Gk", bg: "#1f2937" },
  copilot: { mono: "Co", bg: "#6e5494" },
  cursor: { mono: "Cu", bg: "#111827" },
  augment: { mono: "Au", bg: "#6152d9" },
  opencode: { mono: "Oc", bg: "#f59e0b" },
  amp: { mono: "Am", bg: "#8b5cf6" },
};

function providerIconSpec(id) {
  if (PROVIDER_ICON[id]) return PROVIDER_ICON[id];
  var s = providerShort(id);
  return { mono: (s.slice(0, 2) || "?").replace(/^./, function (c) { return c.toUpperCase(); }), bg: "#64748b" };
}

function providerMonoIcon(h, id, size, accessibleLabel) {
  var spec = providerIconSpec(id);
  var s = size || 15;
  return h(
    "span",
    {
      role: accessibleLabel ? "img" : undefined,
      "aria-label": accessibleLabel || undefined,
      "aria-hidden": accessibleLabel ? undefined : "true",
      style: {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        width: s + "px",
        height: s + "px",
        borderRadius: "4px",
        background: spec.bg,
        color: "#fff",
        fontSize: Math.round(s * 0.52) + "px",
        fontWeight: 700,
        lineHeight: 1,
        letterSpacing: "-0.02em",
        flex: "0 0 auto",
      },
    },
    spec.mono,
  );
}

// Real brand marks, monochrome (currentColor) for a sober greyscale look. Marks
// stay at full foreground brightness while adjacent labels carry muted/active
// emphasis. Each entry is { vb, inner } (source viewBox + inner SVG).
var PROVIDER_SVG = {"claude":{"vb":"0 0 24 24","inner":"<path d=\"M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z\"></path>"},"codex":{"vb":"0 0 24 24","inner":"<path d=\"M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543-2.616c.619-.357 1.356-.523 2.117-.523 2.854 0 4.662 2.212 4.662 4.566 0 .167 0 .357-.024.547l-4.71-2.759a.797.797 0 00-.856 0l-5.97 3.473zm10.609 8.8V12.06c0-.333-.143-.57-.429-.737l-5.97-3.473 1.95-1.118a.433.433 0 01.476 0l4.543 2.617c1.309.76 2.189 2.378 2.189 3.948 0 1.808-1.07 3.473-2.76 4.163zM7.802 12.703l-1.95-1.142c-.167-.095-.239-.238-.239-.428V5.899c0-2.545 1.95-4.472 4.591-4.472 1 0 1.927.333 2.712.928L8.23 5.067c-.285.166-.428.404-.428.737v6.898zM12 15.128l-2.795-1.57v-3.33L12 8.658l2.795 1.57v3.33L12 15.128zm1.796 7.23c-1 0-1.927-.332-2.712-.927l4.686-2.712c.285-.166.428-.404.428-.737v-6.898l1.974 1.142c.167.095.238.238.238.428v5.233c0 2.545-1.974 4.472-4.614 4.472zm-5.637-5.303l-4.544-2.617c-1.308-.761-2.188-2.378-2.188-3.948A4.482 4.482 0 014.21 6.327v5.423c0 .333.143.571.428.738l5.947 3.449-1.95 1.118a.432.432 0 01-.476 0zm-.262 3.9c-2.688 0-4.662-2.021-4.662-4.519 0-.19.024-.38.047-.57l4.686 2.71c.286.167.571.167.856 0l5.97-3.448v2.26c0 .19-.07.333-.237.428l-4.543 2.616c-.619.357-1.356.523-2.117.523zm5.899 2.83a5.947 5.947 0 005.827-4.756C22.287 18.339 24 15.84 24 13.296c0-1.665-.713-3.282-1.998-4.448.119-.5.19-.999.19-1.498 0-3.401-2.759-5.947-5.946-5.947-.642 0-1.26.095-1.88.31A5.962 5.962 0 0010.205 0a5.947 5.947 0 00-5.827 4.757C1.713 5.447 0 7.945 0 10.49c0 1.666.713 3.283 1.998 4.448-.119.5-.19 1-.19 1.499 0 3.401 2.759 5.946 5.946 5.946.642 0 1.26-.095 1.88-.309a5.96 5.96 0 004.162 1.713z\"></path>"},"gemini":{"vb":"0 0 24 24","inner":"<path d=\"M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z\"></path>"},"copilot":{"vb":"0 0 24 24","inner":"<path d=\"M9 23l.073-.001a2.53 2.53 0 01-2.347-1.838l-.697-2.433a2.529 2.529 0 00-2.426-1.839h-.497l-.104-.002c-4.485 0-2.935-5.278-1.75-9.225l.162-.525C2.412 3.99 3.883 1 6.25 1h8.86c1.12 0 2.106.745 2.422 1.829l.715 2.453a2.53 2.53 0 002.247 1.823l.147.005.534.001c3.557.115 3.088 3.745 2.156 7.206l-.113.413c-.154.548-.315 1.089-.47 1.607l-.163.525C21.588 20.01 20.116 23 17.75 23h-8.75zm8.22-15.89l-3.856.001a2.526 2.526 0 00-2.35 1.615L9.21 15.04a2.529 2.529 0 01-2.43 1.847l3.853.002c1.056 0 1.992-.661 2.361-1.644l1.796-6.287a2.529 2.529 0 012.43-1.848z\"></path>"},"amp":{"vb":"0 0 24 24","inner":"<path d=\"M15.087 23.18L12.03 24l-2.097-7.823-5.738 5.738-2.251-2.251 5.718-5.719-7.769-2.082.82-3.057 11.294 3.08 3.08 11.295z\"></path><path d=\"M19.505 18.762l-3.057.82-2.564-9.573-9.572-2.564.819-3.057 11.295 3.079 3.08 11.295z\"></path><path d=\"M23.893 14.374l-3.057.82-2.565-9.572L8.7 3.057 9.52 0l11.295 3.08 3.079 11.294z\"></path>"},"cursor":{"vb":"0 0 24 24","inner":"<path d=\"M22.106 5.68L12.5.135a.998.998 0 00-.998 0L1.893 5.68a.84.84 0 00-.419.726v11.186c0 .3.16.577.42.727l9.607 5.547a.999.999 0 00.998 0l9.608-5.547a.84.84 0 00.42-.727V6.407a.84.84 0 00-.42-.726zm-.603 1.176L12.228 22.92c-.063.108-.228.064-.228-.061V12.34a.59.59 0 00-.295-.51l-9.11-5.26c-.107-.062-.063-.228.062-.228h18.55c.264 0 .428.286.296.514z\"></path>"},"grok":{"vb":"0 0 24 24","inner":"<path d=\"M9.27 15.29l7.978-5.897c.391-.29.95-.177 1.137.272.98 2.369.542 5.215-1.41 7.169-1.951 1.954-4.667 2.382-7.149 1.406l-2.711 1.257c3.889 2.661 8.611 2.003 11.562-.953 2.341-2.344 3.066-5.539 2.388-8.42l.006.007c-.983-4.232.242-5.924 2.75-9.383.06-.082.12-.164.179-.248l-3.301 3.305v-.01L9.267 15.292M7.623 16.723c-2.792-2.67-2.31-6.801.071-9.184 1.761-1.763 4.647-2.483 7.166-1.425l2.705-1.25a7.808 7.808 0 00-1.829-1A8.975 8.975 0 005.984 5.83c-2.533 2.536-3.33 6.436-1.962 9.764 1.022 2.487-.653 4.246-2.34 6.022-.599.63-1.199 1.259-1.682 1.925l7.62-6.815\"></path>"},"opencode":{"vb":"0 0 24 24","inner":"<path d=\"M16 6H8v12h8V6zm4 16H4V2h16v20z\"></path>"},"augment":{"vb":"0 0 512 512","inner":"<path d=\"M78.844 464.762c-8.453 0-15.573-1.451-21.359-4.339-5.77-2.888-10.144-7.289-13.076-13.095-2.932-5.807-4.436-12.912-4.436-21.255v-86.028c0-10.605-2.125-18.321-6.329-23.135-4.234-4.798-11.742-7.334-22.507-7.579-3.35 0-6.034-1.253-8.066-3.804C1.008 303.005 0 300.087 0 296.832c0-3.53 1.008-6.448 3.071-8.725 2.048-2.277 4.762-3.53 8.066-3.774 10.765-.26 18.273-2.781 22.507-7.579 4.235-4.798 6.329-12.392 6.329-22.752v-86.028c0-12.637 3.35-22.249 10.005-28.804 6.654-6.555 16.287-9.856 28.866-9.856H181.5c3.862 0 7.042 1.146 9.617 3.408 2.559 2.277 3.862 5.195 3.862 8.694 0 3.301-1.086 6.128-3.257 8.542-2.172 2.414-5.057 3.622-8.671 3.622H87.732c-5.413 0-9.508 1.39-12.316 4.171-2.823 2.781-4.234 7.075-4.234 12.912v86.425c0 7.579-1.551 14.455-4.623 20.644-3.07 6.204-7.181 11.063-12.316 14.623-5.134 3.53-11.137 5.302-18.07 5.302v-1.528c6.933 0 12.936 1.773 18.07 5.303 5.135 3.529 9.245 8.404 12.316 14.623 3.072 6.188 4.623 13.064 4.623 20.643v86.808c0 5.837 1.411 10.115 4.234 12.911 2.823 2.812 6.934 4.172 12.316 4.172h95.318c3.583 0 6.468 1.207 8.671 3.606 2.202 2.414 3.257 5.257 3.257 8.542s-1.272 6.097-3.862 8.511c-2.575 2.414-5.771 3.606-9.617 3.606H78.844v-.092ZM330.501 464.768c-3.862 0-7.042-1.207-9.617-3.606-2.575-2.414-3.863-5.256-3.863-8.511 0-3.255 1.086-6.128 3.258-8.542 2.171-2.414 5.057-3.606 8.671-3.606h95.317c5.414 0 9.509-1.36 12.316-4.171 2.823-2.781 4.235-7.075 4.235-12.912v-86.808c0-7.579 1.551-14.455 4.622-20.643 3.071-6.204 7.182-11.063 12.316-14.623 5.134-3.53 11.137-5.303 18.071-5.303v1.528c-6.934 0-12.937-1.772-18.071-5.302-5.134-3.53-9.245-8.404-12.316-14.623-3.071-6.189-4.622-13.065-4.622-20.644v-86.425c0-5.807-1.412-10.1-4.235-12.912-2.823-2.781-6.933-4.171-12.316-4.171H328.95c-3.583 0-6.469-1.208-8.671-3.622-2.172-2.384-3.258-5.241-3.258-8.542 0-3.529 1.272-6.417 3.863-8.694 2.559-2.277 5.755-3.407 9.617-3.407h102.654c12.58 0 22.181 3.3 28.867 9.855 6.685 6.556 10.005 16.167 10.005 28.804v86.028c0 10.36 2.125 17.969 6.328 22.752 4.235 4.798 11.742 7.334 22.507 7.579 3.351.244 6.034 1.497 8.066 3.774 2.063 2.277 3.071 5.195 3.071 8.725 0 3.301-1.008 6.189-3.071 8.695-2.032 2.521-4.762 3.804-8.066 3.804-10.765.245-18.257 2.781-22.507 7.579-4.234 4.798-6.328 12.5-6.328 23.135v86.028c0 8.358-1.474 15.418-4.437 21.255-2.962 5.837-7.305 10.176-13.076 13.095-5.785 2.888-12.905 4.339-21.359 4.339H330.501v.092Z\"></path><path d=\"M356.885 329.738c18.691 0 33.846-14.929 33.846-33.342 0-18.412-15.155-33.341-33.846-33.341-18.691 0-33.846 14.929-33.846 33.341 0 18.413 15.155 33.342 33.846 33.342ZM167.305 329.738c18.691 0 33.846-14.929 33.846-33.342 0-18.412-15.155-33.341-33.846-33.341-18.691 0-33.846 14.929-33.846 33.341 0 18.413 15.155 33.342 33.846 33.342ZM244.477 32.846l-2.59 68.135c0 3.82-3.661 5.73-10.983 5.73-7.321 0-10.982-1.91-10.982-5.73-.651-16.976-1.178-30.148-1.613-39.484-.217-9.55-.434-16.35-.651-20.384-.217-4.034-.326-6.479-.326-7.32v-1.268c0-4.874 4.529-7.319 13.572-7.319 9.044 0 13.573 2.552 13.573 7.64Zm54.941 0-2.59 68.135c0 3.82-3.661 5.73-10.982 5.73-7.322 0-10.982-1.91-10.982-5.73-.652-16.976-1.179-30.148-1.613-39.484-.218-9.55-.435-16.35-.652-20.384-.217-4.034-.326-6.479-.326-7.32v-1.268c0-4.874 4.53-7.319 13.573-7.319s13.572 2.552 13.572 7.64Z\"></path>"}}; /* __BRAND_ICONS__ */

// providerIcon renders the real brand mark when known, else the monogram chip.
function providerIcon(h, id, size, accessibleLabel) {
  var brand = PROVIDER_SVG[id];
  if (!brand) return providerMonoIcon(h, id, size, accessibleLabel);
  var s = size || 15;
  return h("svg", {
    xmlns: "http://www.w3.org/2000/svg",
    width: s,
    height: s,
    viewBox: brand.vb || "0 0 24 24",
    fill: "currentColor",
    role: accessibleLabel ? "img" : undefined,
    "aria-label": accessibleLabel || undefined,
    "aria-hidden": accessibleLabel ? undefined : "true",
    style: { flex: "0 0 auto", color: "var(--foreground)", opacity: 1 },
    dangerouslySetInnerHTML: { __html: brand.inner },
  });
}

// tierColor maps a utilization percentage to a bar colour using backend
// thresholds — soft indigo normally, warming only when high.
function tierColor(pct, warn, high) {
  var w = typeof warn === "number" ? warn : 75;
  var hi = typeof high === "number" ? high : 90;
  if (pct >= hi) return COLOR.high;
  if (pct >= w) return COLOR.warn;
  return COLOR.base;
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

// peakPct returns the highest window utilization for a provider, ignoring scoped
// windows (e.g. "Fable only") so a niche cap doesn't dominate the glance value.
function peakPct(usage) {
  var windows = (usage && usage.windows) || [];
  var max = 0;
  var any = false;
  for (var i = 0; i < windows.length; i++) {
    if (windows[i].scoped) continue;
    var p = windows[i].utilization_pct;
    if (typeof p === "number") {
      any = true;
      if (p > max) max = p;
    }
  }
  if (any) return max;
  // All windows scoped (rare) — fall back to the overall max.
  for (var j = 0; j < windows.length; j++) {
    var q = windows[j].utilization_pct;
    if (typeof q === "number" && q > max) max = q;
  }
  return max;
}

// statusMeterDetail keeps the compact meter honest: its percentage, remaining
// capacity, and reset all describe the same highest non-scoped window.
function statusMeterDetail(usage) {
  var windows = (usage && usage.windows) || [];
  var selected = null;
  var selectedPct = -1;
  function consider(window, allowScoped) {
    if (!window || (!allowScoped && window.scoped)) return;
    var pct = typeof window.utilization_pct === "number" ? window.utilization_pct : 0;
    if (pct > selectedPct) {
      selected = window;
      selectedPct = pct;
    }
  }
  for (var i = 0; i < windows.length; i++) consider(windows[i], false);
  if (!selected) {
    for (var j = 0; j < windows.length; j++) consider(windows[j], true);
  }
  var pct = Math.max(0, Math.min(100, selectedPct < 0 ? 0 : selectedPct));
  return {
    window: selected,
    label: (selected && selected.label) || "usage",
    pct: pct,
    used: fmtPct(pct) + " used",
    remaining: fmtPct(100 - pct) + " remaining",
    reset: selected ? fmtReset(selected) : "",
  };
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
      h("span", { style: { fontWeight: 600, fontVariantNumeric: "tabular-nums", opacity: 0.9 } }, fmtPct(pct) + " used"),
      reset ? h("span", { style: { opacity: 0.5 } }, reset) : null,
    ),
    pace ? h("div", { style: { fontSize: "10.5px", opacity: 0.45, marginTop: "-1px" } }, pace) : null,
  );
}

// paceText condenses a codexbar pace side to its first clause, e.g.
// "58% in reserve | Expected 65% used | Lasts until reset" -> "58% in reserve".
function paceText(pace) {
  if (!pace || !pace.summary) return "";
  return String(pace.summary).split("|")[0].trim();
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
function providerPanel(host, p, warn, high, generatedAt, reload, isCurrent) {
  var h = host.jsx;
  var windows = p.windows || [];
  var paceFor = [p.pace_primary, p.pace_secondary];

  return h(
    "div",
    { style: { display: "flex", flexDirection: "column", gap: "13px" } },
    // header: name (+ this-session marker) with plan on the right, then meta row
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "2px" } },
      h(
        "div",
        { style: { display: "flex", justifyContent: "space-between", alignItems: "baseline", gap: "10px" } },
        h(
          "span",
          { style: { display: "inline-flex", alignItems: "baseline", gap: "7px", minWidth: 0 } },
          h("span", { style: { fontWeight: 700, fontSize: "14px" } }, providerLabel(p.provider)),
          isCurrent ? h("span", { style: { fontSize: "10px", color: COLOR.accent, opacity: 0.85 } }, "this session") : null,
        ),
        p.plan ? h("span", { style: { fontSize: "11.5px", opacity: 0.55, textAlign: "right", whiteSpace: "nowrap" } }, p.plan) : null,
      ),
      h(
        "div",
        { style: { fontSize: "10.5px", opacity: 0.5, display: "flex", gap: "6px", alignItems: "center" } },
        h("span", null, "Updated " + relTime(generatedAt)),
        h("span", { style: { opacity: 0.5 } }, "·"),
        h("button", { type: "button", onClick: reload, style: { border: "none", background: "transparent", padding: 0, cursor: "pointer", color: "inherit", opacity: 0.9, fontSize: "10.5px" } }, "Refresh"),
      ),
    ),
    windows.length
      ? h(
          "div",
          { style: { display: "flex", flexDirection: "column", gap: "13px" } },
          windows.map(function (w, i) {
            return h("div", { key: i }, cleanWindow(h, w, warn, high, i < 2 ? paceText(paceFor[i]) : ""));
          }),
        )
      : p.detail
        ? null
        : h("div", { style: { fontSize: "12px", opacity: 0.55 } }, "No rate-limit windows reported."),
    // Augment consumption + pace, sober, below the bar.
    p.detail
      ? h(
          "div",
          { style: { display: "flex", flexDirection: "column", gap: "2px", marginTop: windows.length ? "-2px" : "0" } },
          h("div", { style: { fontSize: "11.5px", opacity: 0.75, fontVariantNumeric: "tabular-nums" } }, p.detail),
          p.detail_extra ? h("div", { style: { fontSize: "10.5px", opacity: 0.45, fontVariantNumeric: "tabular-nums" } }, p.detail_extra) : null,
        )
      : null,
  );
}

// ---- provider tab strip (text pills) --------------------------------------
function tabStrip(host, providers, active, current, onSelect) {
  var h = host.jsx;
  return h(
    "div",
    { style: { display: "flex", gap: "3px", overflowX: "auto", paddingBottom: "9px", borderBottom: "1px solid " + COLOR.track } },
    providers.map(function (p, i) {
      var isActive = i === active;
      var isCurrent = current && p.provider === current;
      return h(
        "button",
        {
          key: p.provider,
          type: "button",
          title: providerLabel(p.provider) + (isCurrent ? " · this session" : ""),
          onClick: function () { onSelect(i); },
          onMouseEnter: function () { onSelect(i); },
          style: {
            display: "inline-flex",
            alignItems: "center",
            gap: "5px",
            padding: "3px 8px",
            border: "none",
            borderRadius: "7px",
            cursor: "pointer",
            whiteSpace: "nowrap",
            fontSize: "11px",
            fontWeight: isActive ? 600 : 500,
            background: isActive ? COLOR.accentBg : "transparent",
            color: isActive ? COLOR.accent : "inherit",
            opacity: 1,
          },
        },
        providerIcon(h, p.provider, 13),
        h("span", { style: { opacity: isActive ? 1 : 0.62 } }, providerShort(p.provider)),
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
    return h("div", { style: { display: "flex", flexDirection: "column", gap: "12px", width: "244px" } }, body);
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
      providerPanel(host, p, d.warn_threshold, d.high_threshold, d.generated_at, reload, d.current_provider && p.provider === d.current_provider),
    ),
  );
}

// ---- the chat-top-bar component -------------------------------------------
// Self-contained hover panel: its own open state and a position:fixed panel
// (anchored to the trigger's rect) so it works regardless of whether the slot
// sits inside a Radix TooltipProvider, and escapes any overflow clipping on the
// top bar. Clicking the gauge also toggles it.
var PANEL_WIDTH = 272;

function usagePopoverPosition(rect, viewportWidth, viewportHeight, placement) {
  var left = Math.max(8, Math.min(rect.right - PANEL_WIDTH, viewportWidth - PANEL_WIDTH - 8));
  if (placement === "above") {
    return { bottom: Math.max(0, viewportHeight - rect.top), left: left };
  }
  return { top: rect.bottom, left: left };
}

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

    // fetchOverview reads the overview webhook. backendRefresh forces the server
    // to re-run codexbar (expensive); silent skips the loading flash and keeps
    // the current data/index so a periodic re-read doesn't flicker the panel.
    function fetchOverview(opts) {
      opts = opts || {};
      var active = ctx.activeSessionId || "";
      loadedForRef.current = active;
      if (!opts.silent) {
        setState(function (s) { return { loading: true, data: opts.backendRefresh ? s.data : null, error: null }; });
      }
      var qs =
        "webhooks/overview?task_id=" +
        encodeURIComponent(ctx.taskId || "") +
        "&active=" +
        encodeURIComponent(active) +
        (opts.backendRefresh ? "&refresh=1" : "");
      host.api
        .fetch(qs)
        .then(function (r) { return r.json(); })
        .then(function (data) {
          setState({ loading: false, data: data, error: null });
          if (!opts.silent) setIndex(0);
        })
        .catch(function (err) {
          if (opts.silent) return; // keep the last good render on a transient poll failure
          setState({ loading: false, data: null, error: String(err && err.message ? err.message : err) });
        });
    }

    function load(force) {
      var active = ctx.activeSessionId || "";
      if (!force && loadedForRef.current === active && state.data) return;
      fetchOverview({ backendRefresh: force });
    }

    React.useEffect(function () { loadedForRef.current = null; load(false); }, [ctx.activeSessionId]);

    // Keep the pill / open panel in step with the backend poller by silently
    // re-reading the warm snapshot on an interval.
    React.useEffect(function () {
      var id = setInterval(function () { fetchOverview({ silent: true }); }, AUTO_REFRESH_MS);
      return function () { clearInterval(id); };
    }, [ctx.activeSessionId, ctx.taskId]);

    function reposition() {
      var el = wrapRef.current;
      if (!el || !el.getBoundingClientRect) return;
      var r = el.getBoundingClientRect();
      // No vertical gap: the fixed container starts at the trigger's bottom and
      // bridges to the card with transparent padding, so the mouse never leaves
      // the hover area on the way down.
      setPos(usagePopoverPosition(r, window.innerWidth, window.innerHeight, "below"));
    }
    function cancelClose() { if (closeTimer.current) { clearTimeout(closeTimer.current); closeTimer.current = null; } }
    function openNow() { cancelClose(); reposition(); setOpen(true); load(false); }
    function scheduleClose() { cancelClose(); closeTimer.current = setTimeout(function () { setOpen(false); }, 260); }

    // Inline: the configured pill providers as [icon %] segments, once loaded.
    var d = state.data;
    var pill = pillContent(host, d);

    return h(
      "div",
      { ref: wrapRef, style: { display: "inline-flex" }, onMouseEnter: openNow, onMouseLeave: scheduleClose },
      h(
        ui.Button,
        {
          id: "provider-usage-topbar",
          type: "button",
          variant: "outline",
          size: "sm",
          className: (pill ? "h-6 gap-1.5 px-2 " : "h-6 w-6 px-0 ") + "rounded-md text-xs font-medium text-muted-foreground hover:text-foreground",
          "aria-label": "Provider usage",
          onFocus: openNow,
          onClick: function () { if (open) { setOpen(false); } else { openNow(); } },
        },
        pill || gaugeIcon(h, 14),
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
              { style: { padding: "13px 14px", boxShadow: "0 10px 28px rgba(15,20,40,0.20)" } },
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

// statusMeterProviders follows the existing pill configuration so the optional
// status-surface contribution never introduces a second provider picker.
function statusMeterProviders(data) {
  var ids = (data && data.pill_providers) || [];
  var providers = (data && data.providers) || [];
  var out = [];
  ids.forEach(function (id) {
    var usage = providerByName(providers, id);
    if (usage) out.push(usage);
  });
  return out;
}

// pillContent renders the configured pill providers as [icon %] segments with a
// thin separator between them. Returns null until data is loaded / no providers.
function pillContent(host, d) {
  var h = host.jsx;
  var ids = (d && d.pill_providers) || [];
  var segs = [];
  ids.forEach(function (id) {
    var pu = providerByName(d.providers, id);
    if (!pu) return;
    var pct = fmtPct(peakPct(pu));
    if (segs.length) {
      segs.push(h("span", { key: "sep" + id, style: { width: "1px", alignSelf: "stretch", background: "currentColor", opacity: 0.22, margin: "1px 0" } }));
    }
    segs.push(
      h(
        "span",
        { key: "seg" + id, title: providerLabel(id) + " · " + pct + " used", style: { display: "inline-flex", alignItems: "center", gap: "4px" } },
        providerIcon(h, id, 14),
        h("span", { style: { fontVariantNumeric: "tabular-nums", fontWeight: 600 } }, pct),
      ),
    );
  });
  if (!segs.length) return null;
  return h("span", { style: { display: "inline-flex", alignItems: "center", gap: "6px" } }, segs);
}

// ---- optional global app-status-bar contribution --------------------------
// The host renders one of two presentations: a 24px desktop/tablet bar or a
// phone drawer. This component deliberately owns no task/executor state; the
// active IDs only let the backend place the current provider first when the
// shared provider configuration includes `current`.
function statusMeterTitle(usage, detail) {
  var bits = [providerLabel(usage.provider)];
  if (usage.detail) bits.push(usage.detail);
  if (detail.window) {
    bits.push(detail.label + " · " + detail.used);
    bits.push(detail.remaining);
    if (detail.reset) bits.push(detail.reset);
  }
  return bits.join(" · ");
}

function statusMeterTrack(h, detail, warn, high) {
  return h(
    "span",
    {
      "aria-hidden": "true",
      className: "block min-w-0 flex-1 overflow-hidden rounded-full bg-muted",
      style: { height: "3px" },
    },
    h("span", {
      style: {
        display: "block",
        width: detail.pct + "%",
        height: "100%",
        borderRadius: "9999px",
        background: tierColor(detail.pct, warn, high),
        transition: "width 240ms ease",
      },
    }),
  );
}

function statusMeterBarItem(host, usage, warn, high, density, mode) {
  var h = host.jsx;
  var detail = statusMeterDetail(usage);
  var full = density !== "compact";
  var parts = statusBarParts(mode);
  var meterWidth = full ? "170px" : "96px";
  var percentageMaxWidth = full ? "126px" : "54px";
  var title = statusMeterTitle(usage, detail);
  return h(
    "div",
    {
      key: usage.provider,
      role: "group",
      "aria-label": title,
      draggable: false,
      className: "flex h-full min-w-0 flex-1 items-center gap-1 text-muted-foreground",
      style: {
        width: parts.meter ? meterWidth : "auto",
        maxWidth: parts.meter ? meterWidth : percentageMaxWidth,
        minWidth: 0,
        userSelect: "none",
        WebkitUserSelect: "none",
        cursor: "default",
      },
    },
    providerIcon(h, usage.provider, 13, providerLabel(usage.provider)),
    full
      ? h(
          "span",
          { className: "min-w-0 truncate text-[11px] font-medium text-foreground/90" },
          providerShort(usage.provider),
        )
      : null,
    parts.meter ? statusMeterTrack(h, detail, warn, high) : null,
    parts.percentage
      ? h(
          "span",
          {
            className:
              "inline-flex h-full shrink-0 items-center text-[11px] font-medium leading-none tabular-nums text-foreground/90",
            style: {
              display: "inline-flex",
              alignItems: "center",
              alignSelf: "stretch",
              lineHeight: 1,
              whiteSpace: "nowrap",
            },
          },
          fmtPct(detail.pct),
        )
      : null,
    full && detail.reset
      ? h(
          "span",
          {
            className:
              "inline-flex h-full shrink-0 items-center text-[11px] font-medium leading-none tabular-nums text-muted-foreground",
            style: {
              display: "inline-flex",
              alignItems: "center",
              alignSelf: "stretch",
              lineHeight: 1,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              maxWidth: "52px",
            },
          },
          detail.reset.replace(/^resets in\s*/i, ""),
        )
      : null,
  );
}

function statusMeterBar(host, data, ctx, mode) {
  var h = host.jsx;
  var providers = statusMeterProviders(data);
  if (!providers.length) return null;
  return h(
    "div",
    {
      className: "flex h-full min-w-0 max-w-full items-center gap-1 overflow-hidden text-muted-foreground",
      style: { userSelect: "none", WebkitUserSelect: "none" },
    },
    providers.map(function (usage) {
      return statusMeterBarItem(host, usage, data.warn_threshold, data.high_threshold, ctx.density, mode);
    }),
  );
}

function statusMeterDrawerRow(host, usage, warn, high) {
  var h = host.jsx;
  var detail = statusMeterDetail(usage);
  var title = statusMeterTitle(usage, detail);
  var usageText = detail.window ? detail.used + " · " + detail.remaining : usage.detail || "Usage unavailable";
  return h(
    "div",
    {
      key: usage.provider,
      role: "group",
      "aria-label": title,
      className: "flex w-full min-w-0 items-center gap-3",
      style: { minHeight: "44px", userSelect: "none", WebkitUserSelect: "none" },
    },
    providerIcon(h, usage.provider, 18, providerLabel(usage.provider)),
    h(
      "div",
      { className: "flex min-w-0 flex-1 flex-col gap-1" },
      h(
        "div",
        { className: "flex min-w-0 items-baseline justify-between gap-3" },
        h("span", { className: "min-w-0 truncate text-sm font-medium text-foreground" }, providerLabel(usage.provider)),
        detail.window
          ? h("span", { className: "shrink-0 text-xs font-medium tabular-nums text-foreground" }, fmtPct(detail.pct))
          : null,
      ),
      h(
        "div",
        { className: "min-w-0 truncate text-xs text-muted-foreground" },
        usageText + (detail.reset ? " · " + detail.reset : ""),
      ),
      detail.window ? statusMeterTrack(h, detail, warn, high) : null,
    ),
  );
}

function statusMeterDrawer(host, data) {
  var h = host.jsx;
  var providers = statusMeterProviders(data);
  if (!providers.length) return null;
  return h(
    "section",
    { className: "flex w-full min-w-0 flex-col", "aria-label": "Provider usage" },
    h("div", { className: "pb-1 text-xs font-medium text-muted-foreground" }, "Provider usage"),
    providers.map(function (usage) {
      return statusMeterDrawerRow(host, usage, data.warn_threshold, data.high_threshold);
    }),
  );
}

function makeAppStatusBarUsage(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;

  return function AppStatusBarUsage(props) {
    var ctx = (props && props.slotProps) || {};
    var stateHook = React.useState({ loading: true, data: null, error: null });
    var state = stateHook[0];
    var setState = stateHook[1];
    var indexHook = React.useState(0);
    var index = indexHook[0];
    var setIndex = indexHook[1];
    var openHook = React.useState(false);
    var open = openHook[0];
    var setOpen = openHook[1];
    var posHook = React.useState({ bottom: 0, left: 0 });
    var pos = posHook[0];
    var setPos = posHook[1];
    var triggerRef = React.useRef(null);
    var closeTimer = React.useRef(null);
    var contextVersion = React.useRef(0);

    function fetchOverview(opts) {
      opts = opts || {};
      var version = contextVersion.current;
      var active = ctx.activeSessionId || "";
      var taskID = ctx.activeTaskId || "";
      if (!opts.silent) {
        setState(function (current) {
          return { loading: true, data: opts.backendRefresh ? current.data : null, error: null };
        });
      }
      host.api
        .fetch(
          "webhooks/overview?task_id=" +
            encodeURIComponent(taskID) +
            "&active=" +
            encodeURIComponent(active) +
            (opts.backendRefresh ? "&refresh=1" : ""),
        )
        .then(function (response) { return response.json(); })
        .then(function (data) {
          if (version !== contextVersion.current) return;
          setState({ loading: false, data: data, error: null });
          if (!opts.silent) setIndex(0);
        })
        .catch(function (error) {
          if (opts.silent || version !== contextVersion.current) return;
          setState({ loading: false, data: null, error: String(error && error.message ? error.message : error) });
        });
    }

    React.useEffect(function () {
      contextVersion.current += 1;
      fetchOverview();
      return function () { contextVersion.current += 1; };
    }, [ctx.activeSessionId, ctx.activeTaskId]);

    var data = state.data;
    var mode = statusBarMode(data && data.status_bar_mode);

    // A missing first response retries quickly through plugin startup. Once a
    // snapshot exists, even off mode rechecks the cheap cache once a minute so
    // a saved presentation change appears without a page reload.
    var refreshDelay = statusRefreshDelay(data);
    React.useEffect(function () {
      var id = setInterval(function () {
        fetchOverview({ silent: true });
      }, refreshDelay);
      return function () { clearInterval(id); };
    }, [refreshDelay, ctx.activeSessionId, ctx.activeTaskId]);

    React.useEffect(function () {
      return function () {
        if (closeTimer.current) clearTimeout(closeTimer.current);
      };
    }, []);

    function cancelClose() {
      if (!closeTimer.current) return;
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }

    function reposition() {
      var el = triggerRef.current;
      if (!el || !el.getBoundingClientRect) return;
      setPos(usagePopoverPosition(el.getBoundingClientRect(), window.innerWidth, window.innerHeight, "above"));
    }

    function openNow() {
      cancelClose();
      reposition();
      setOpen(true);
    }

    function scheduleClose() {
      cancelClose();
      closeTimer.current = setTimeout(function () { setOpen(false); }, 260);
    }

    if (mode === "off" || !data) return null;
    if (ctx.presentation === "mobile-drawer") return statusMeterDrawer(host, data);

    var bar = statusMeterBar(host, data, ctx, mode);
    if (!bar) return null;
    return h(
      "div",
      {
        className: "inline-flex h-full min-w-0",
        onMouseEnter: openNow,
        onMouseLeave: scheduleClose,
      },
      h(
        "div",
        {
          ref: triggerRef,
          role: "button",
          tabIndex: 0,
          "aria-label": "Provider usage details",
          "aria-haspopup": "dialog",
          "aria-expanded": open,
          className: "inline-flex h-full min-w-0 items-center outline-none focus-visible:ring-1 focus-visible:ring-ring",
          onFocus: openNow,
          onClick: function () { if (open) setOpen(false); else openNow(); },
          onKeyDown: function (event) {
            if (event.key === "Escape") setOpen(false);
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              if (open) setOpen(false); else openNow();
            }
          },
        },
        bar,
      ),
      open
        ? h(
            "div",
            {
              role: "dialog",
              "aria-label": "Provider usage",
              "data-provider-usage-popover": "status-bar",
              onMouseEnter: cancelClose,
              onMouseLeave: scheduleClose,
              onFocus: cancelClose,
              style: {
                position: "fixed",
                bottom: pos.bottom + "px",
                left: pos.left + "px",
                zIndex: 9999,
                paddingBottom: "8px",
              },
            },
            h(
              ui.Card,
              { style: { padding: "13px 14px", boxShadow: "0 10px 28px rgba(15,20,40,0.20)" } },
              panelBody(host, state, index, setIndex, function () { fetchOverview({ backendRefresh: true }); }),
            ),
          )
        : null,
    );
  };
}

// ---- the plugin-settings status card --------------------------------------
// Renders inline on Settings → Plugins → Provider Usage (host "plugin-settings"
// slot) so the operator can tell at a glance whether the codexbar CLI resolved
// and whether the Augment API is reachable. Everything comes from the warm
// "providers" snapshot — codexbar install status plus per-provider errors
// (Augment included) — so no extra backend endpoint is needed.

// Health uses a muted green / coral pair (never a hard red), matching the calm
// palette used by the usage bars.
var OK_COLOR = "#5aa86f";
// Compact "source: X" label for the codexbar row (how the CLI resolved).
var SOURCE_LABEL = { settings: "settings", path: "PATH", download: "download" };

function healthGlyph(h, ok) {
  var common = {
    width: 12,
    height: 12,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 3,
    strokeLinecap: "round",
    strokeLinejoin: "round",
    "aria-hidden": "true",
  };
  return ok
    ? h("svg", common, h("path", { d: "M20 6 L9 17 L4 12" }))
    : h("svg", common, h("path", { d: "M6 6 L18 18" }), h("path", { d: "M18 6 L6 18" }));
}

// statusIcon: a bordered circle with a check (ok) or cross (bad), tinted by health.
function statusIcon(h, ok) {
  var color = ok ? OK_COLOR : COLOR.high;
  return h(
    "span",
    {
      "aria-hidden": "true",
      style: {
        width: "22px",
        height: "22px",
        borderRadius: "9999px",
        flex: "0 0 auto",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        border: "1.5px solid " + color,
        background: ok ? "rgba(90,168,111,0.12)" : "rgba(217,123,108,0.12)",
        color: color,
      },
    },
    healthGlyph(h, ok),
  );
}

// statusBadge: the right-aligned outlined pill ("working" / "success" / "missing" / "error").
function statusBadge(h, ok, label) {
  var color = ok ? OK_COLOR : COLOR.high;
  return h(
    "span",
    {
      style: {
        fontSize: "11px",
        fontWeight: 600,
        padding: "2px 9px",
        borderRadius: "9999px",
        whiteSpace: "nowrap",
        flex: "0 0 auto",
        color: color,
        border: "1px solid " + (ok ? "rgba(90,168,111,0.5)" : "rgba(217,123,108,0.5)"),
        background: ok ? "rgba(90,168,111,0.10)" : "rgba(217,123,108,0.10)",
      },
    },
    label,
  );
}

// statusRow: icon · (title + mono detail) · badge. opts = { ok, title, detail, badge }.
function statusRow(h, opts) {
  return h(
    "div",
    { style: { display: "flex", alignItems: "center", gap: "11px" } },
    statusIcon(h, opts.ok),
    h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "2px", minWidth: 0, flex: "1 1 auto" } },
      h("div", { style: { fontSize: "13.5px", fontWeight: 700 } }, opts.title),
      opts.detail
        ? h(
            "div",
            {
              style: {
                fontSize: "11.5px",
                opacity: 0.55,
                fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
                wordBreak: "break-word",
              },
            },
            opts.detail,
          )
        : null,
    ),
    opts.badge ? statusBadge(h, opts.ok, opts.badge) : null,
  );
}

// codexbarRow: ✅ "working" with version + how it resolved, else ❌ "missing" with the probe error.
function codexbarRow(h, st) {
  st = st || {};
  if (st.installed) {
    var bits = [];
    if (st.version) bits.push("v" + st.version);
    bits.push("source: " + (SOURCE_LABEL[st.source] || st.source || "unknown"));
    return statusRow(h, { ok: true, title: "codexbar CLI", detail: bits.join(" · "), badge: "working" });
  }
  return statusRow(h, {
    ok: false,
    title: "codexbar CLI",
    detail: st.error || "not found — set a path in the settings above",
    badge: "missing",
  });
}

// augmentRow: shown only when Augment is configured — ✅ "success" with the month's
// consumption when it made it into the provider list, ❌ "error" (with the API
// message) when it landed in `unavailable`, otherwise null (no token/email set).
function augmentRow(h, d) {
  var providers = (d && d.providers) || [];
  for (var i = 0; i < providers.length; i++) {
    if (providers[i].provider === "augment") {
      return statusRow(h, {
        ok: true,
        title: "Augment Analytics API",
        detail: providers[i].detail || "authenticated",
        badge: "success",
      });
    }
  }
  var unavailable = (d && d.unavailable) || [];
  for (var j = 0; j < unavailable.length; j++) {
    if (unavailable[j].provider === "augment") {
      return statusRow(h, {
        ok: false,
        title: "Augment Analytics API",
        detail: unavailable[j].message || "unreachable",
        badge: "error",
      });
    }
  }
  return null;
}

// refreshGlyph: a circular-arrows icon for the header re-check affordance.
function refreshGlyph(h) {
  return h(
    "svg",
    {
      width: 13,
      height: 13,
      viewBox: "0 0 24 24",
      fill: "none",
      stroke: "currentColor",
      strokeWidth: 2,
      strokeLinecap: "round",
      strokeLinejoin: "round",
      "aria-hidden": "true",
      style: { flex: "0 0 auto" },
    },
    h("path", { d: "M3 12a9 9 0 0 1 15-6.7L21 8" }),
    h("path", { d: "M21 3v5h-5" }),
    h("path", { d: "M21 12a9 9 0 0 1-15 6.7L3 16" }),
    h("path", { d: "M3 21v-5h5" }),
  );
}

function settingsStatusBody(host, state, reload) {
  var h = host.jsx;
  var d = state.data;

  var when = state.loading ? "checking…" : d && d.generated_at ? "refreshed " + relTime(d.generated_at) : "";
  var header = h(
    "div",
    { style: { display: "flex", justifyContent: "space-between", alignItems: "center", gap: "10px", marginBottom: "12px" } },
    h("div", { style: { fontSize: "15px", fontWeight: 700 } }, "Integration status"),
    h(
      "button",
      {
        type: "button",
        onClick: reload,
        disabled: state.loading,
        title: "Re-check integrations",
        style: {
          display: "inline-flex",
          alignItems: "center",
          gap: "5px",
          border: "none",
          background: "transparent",
          padding: 0,
          cursor: state.loading ? "default" : "pointer",
          color: "inherit",
          opacity: state.loading ? 0.45 : 0.7,
          fontSize: "11.5px",
        },
      },
      refreshGlyph(h),
      when ? h("span", null, when) : null,
    ),
  );

  var body;
  if (state.error) {
    body = h("div", { style: { fontSize: "12px", color: COLOR.high } }, "Couldn't load status: " + state.error);
  } else if (!d) {
    body = h("div", { style: { fontSize: "12px", opacity: 0.6 } }, "Checking integrations…");
  } else {
    body = h(
      "div",
      { style: { display: "flex", flexDirection: "column", gap: "14px" } },
      codexbarRow(h, d.codexbar),
      augmentRow(h, d),
    );
  }
  return h("div", null, header, body);
}

function makeSettingsStatus(host) {
  var React = host.React;
  var h = host.jsx;
  var ui = host.ui;

  return function SettingsStatus(props) {
    var ctx = (props && props.slotProps) || {};
    // The host scopes this slot to the plugin whose page is open; guard defensively
    // so we never render on another plugin's settings page.
    if (ctx.pluginId && ctx.pluginId !== "kandev-provider-usage") return null;

    var stateHook = React.useState({ loading: true, data: null, error: null });
    var state = stateHook[0];
    var setState = stateHook[1];

    // force re-runs codexbar server-side; silent re-reads the warm snapshot
    // without a loading flash so the card refreshes in place.
    function fetchProviders(opts) {
      opts = opts || {};
      if (!opts.silent) {
        setState(function (s) { return { loading: true, data: s.data, error: null }; });
      }
      host.api
        .fetch("webhooks/providers" + (opts.backendRefresh ? "?refresh=1" : ""))
        .then(function (r) { return r.json(); })
        .then(function (data) { setState({ loading: false, data: data, error: null }); })
        .catch(function (err) {
          if (opts.silent) return; // keep the last good render on a transient poll failure
          setState({ loading: false, data: null, error: String(err && err.message ? err.message : err) });
        });
    }

    function load(force) { fetchProviders({ backendRefresh: force }); }

    React.useEffect(function () { load(false); }, []);

    // Reflect the backend poller's updates (Augment consumption included) while
    // the card stays open, by silently re-reading the warm snapshot.
    React.useEffect(function () {
      var id = setInterval(function () { fetchProviders({ silent: true }); }, AUTO_REFRESH_MS);
      return function () { clearInterval(id); };
    }, []);

    return h(ui.Card, { style: { padding: "16px 18px" } }, settingsStatusBody(host, state, function () { load(true); }));
  };
}

// ==========================================================================
window.registerKandevPlugin("kandev-provider-usage", {
  initialize: function (registry, host) {
    registry.registerComponent("chat-top-bar", makeTopBarStatus(host));
    registry.registerComponent("app-status-bar-right", makeAppStatusBarUsage(host));
    registry.registerComponent("plugin-settings", makeSettingsStatus(host));
  },
});
