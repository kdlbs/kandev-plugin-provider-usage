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

function providerMonoIcon(h, id, size) {
  var spec = providerIconSpec(id);
  var s = size || 15;
  return h(
    "span",
    {
      "aria-hidden": "true",
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

// Real brand marks. Each entry is { vb, inner }: the source viewBox and inner
// SVG markup (brand-colour where the brand has one, else currentColor for
// monochrome marks). Gradient ids are namespaced per provider. Injected below.
var PROVIDER_SVG = {"claude":{"vb":"0 0 24 24","inner":"<path d=\"M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.79-.048-2.698-.073-2.339-.097-2.266-.122-.571-.121L0 11.784l.055-.352.48-.321.686.06 1.52.103 2.278.158 1.652.097 2.449.255h.389l.055-.157-.134-.098-.103-.097-2.358-1.596-2.552-1.688-1.336-.972-.724-.491-.364-.462-.158-1.008.656-.722.881.06.225.061.893.686 1.908 1.476 2.491 1.833.365.304.145-.103.019-.073-.164-.274-1.355-2.446-1.446-2.49-.644-1.032-.17-.619a2.97 2.97 0 01-.104-.729L6.283.134 6.696 0l.996.134.42.364.62 1.414 1.002 2.229 1.555 3.03.456.898.243.832.091.255h.158V9.01l.128-1.706.237-2.095.23-2.695.08-.76.376-.91.747-.492.584.28.48.685-.067.444-.286 1.851-.559 2.903-.364 1.942h.212l.243-.242.985-1.306 1.652-2.064.73-.82.85-.904.547-.431h1.033l.76 1.129-.34 1.166-1.064 1.347-.881 1.142-1.264 1.7-.79 1.36.073.11.188-.02 2.856-.606 1.543-.28 1.841-.315.833.388.091.395-.328.807-1.969.486-2.309.462-3.439.813-.042.03.049.061 1.549.146.662.036h1.622l3.02.225.79.522.474.638-.079.485-1.215.62-1.64-.389-3.829-.91-1.312-.329h-.182v.11l1.093 1.068 2.006 1.81 2.509 2.33.127.578-.322.455-.34-.049-2.205-1.657-.851-.747-1.926-1.62h-.128v.17l.444.649 2.345 3.521.122 1.08-.17.353-.608.213-.668-.122-1.374-1.925-1.415-2.167-1.143-1.943-.14.08-.674 7.254-.316.37-.729.28-.607-.461-.322-.747.322-1.476.389-1.924.315-1.53.286-1.9.17-.632-.012-.042-.14.018-1.434 1.967-2.18 2.945-1.726 1.845-.414.164-.717-.37.067-.662.401-.589 2.388-3.036 1.44-1.882.93-1.086-.006-.158h-.055L4.132 18.56l-1.13.146-.487-.456.061-.746.231-.243 1.908-1.312-.006.006z\" fill=\"#D97757\" fill-rule=\"nonzero\"></path>"},"codex":{"vb":"0 0 24 24","inner":"<path d=\"M19.503 0H4.496A4.496 4.496 0 000 4.496v15.007A4.496 4.496 0 004.496 24h15.007A4.496 4.496 0 0024 19.503V4.496A4.496 4.496 0 0019.503 0z\" fill=\"#fff\"></path><path d=\"M9.064 3.344a4.578 4.578 0 012.285-.312c1 .115 1.891.54 2.673 1.275.01.01.024.017.037.021a.09.09 0 00.043 0 4.55 4.55 0 013.046.275l.047.022.116.057a4.581 4.581 0 012.188 2.399c.209.51.313 1.041.315 1.595a4.24 4.24 0 01-.134 1.223.123.123 0 00.03.115c.594.607.988 1.33 1.183 2.17.289 1.425-.007 2.71-.887 3.854l-.136.166a4.548 4.548 0 01-2.201 1.388.123.123 0 00-.081.076c-.191.551-.383 1.023-.74 1.494-.9 1.187-2.222 1.846-3.711 1.838-1.187-.006-2.239-.44-3.157-1.302a.107.107 0 00-.105-.024c-.388.125-.78.143-1.204.138a4.441 4.441 0 01-1.945-.466 4.544 4.544 0 01-1.61-1.335c-.152-.202-.303-.392-.414-.617a5.81 5.81 0 01-.37-.961 4.582 4.582 0 01-.014-2.298.124.124 0 00.006-.056.085.085 0 00-.027-.048 4.467 4.467 0 01-1.034-1.651 3.896 3.896 0 01-.251-1.192 5.189 5.189 0 01.141-1.6c.337-1.112.982-1.985 1.933-2.618.212-.141.413-.251.601-.33.215-.089.43-.164.646-.227a.098.098 0 00.065-.066 4.51 4.51 0 01.829-1.615 4.535 4.535 0 011.837-1.388zm3.482 10.565a.637.637 0 000 1.272h3.636a.637.637 0 100-1.272h-3.636zM8.462 9.23a.637.637 0 00-1.106.631l1.272 2.224-1.266 2.136a.636.636 0 101.095.649l1.454-2.455a.636.636 0 00.005-.64L8.462 9.23z\" fill=\"url(#codex_lobe-icons-codex-_R_0_)\"></path><defs><linearGradient gradientUnits=\"userSpaceOnUse\" id=\"codex_lobe-icons-codex-_R_0_\" x1=\"12\" x2=\"12\" y1=\"3\" y2=\"21\"><stop stop-color=\"#B1A7FF\"></stop><stop offset=\".5\" stop-color=\"#7A9DFF\"></stop><stop offset=\"1\" stop-color=\"#3941FF\"></stop></linearGradient></defs>"},"gemini":{"vb":"0 0 24 24","inner":"<path d=\"M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z\" fill=\"#3186FF\"></path><path d=\"M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z\" fill=\"url(#gemini_lobe-icons-gemini-0-_R_0_)\"></path><path d=\"M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z\" fill=\"url(#gemini_lobe-icons-gemini-1-_R_0_)\"></path><path d=\"M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.111 14.111 0 01-3.678-6.452.503.503 0 00-.975 0 14.134 14.134 0 01-3.679 6.452 14.155 14.155 0 01-4.45 3.001c-.65.28-1.318.505-2.002.678a.502.502 0 000 .975c.684.172 1.35.397 2.002.677a14.147 14.147 0 014.45 3.001 14.112 14.112 0 013.679 6.453.502.502 0 00.975 0c.172-.685.397-1.351.677-2.003a14.145 14.145 0 013.001-4.45 14.113 14.113 0 016.453-3.678.503.503 0 000-.975 13.245 13.245 0 01-2.003-.678z\" fill=\"url(#gemini_lobe-icons-gemini-2-_R_0_)\"></path><defs><linearGradient gradientUnits=\"userSpaceOnUse\" id=\"gemini_lobe-icons-gemini-0-_R_0_\" x1=\"7\" x2=\"11\" y1=\"15.5\" y2=\"12\"><stop stop-color=\"#08B962\"></stop><stop offset=\"1\" stop-color=\"#08B962\" stop-opacity=\"0\"></stop></linearGradient><linearGradient gradientUnits=\"userSpaceOnUse\" id=\"gemini_lobe-icons-gemini-1-_R_0_\" x1=\"8\" x2=\"11.5\" y1=\"5.5\" y2=\"11\"><stop stop-color=\"#F94543\"></stop><stop offset=\"1\" stop-color=\"#F94543\" stop-opacity=\"0\"></stop></linearGradient><linearGradient gradientUnits=\"userSpaceOnUse\" id=\"gemini_lobe-icons-gemini-2-_R_0_\" x1=\"3.5\" x2=\"17.5\" y1=\"13.5\" y2=\"12\"><stop stop-color=\"#FABC12\"></stop><stop offset=\".46\" stop-color=\"#FABC12\" stop-opacity=\"0\"></stop></linearGradient></defs>"},"copilot":{"vb":"0 0 24 24","inner":"<path d=\"M17.533 1.829A2.528 2.528 0 0015.11 0h-.737a2.531 2.531 0 00-2.484 2.087l-1.263 6.937.314-1.08a2.528 2.528 0 012.424-1.833h4.284l1.797.706 1.731-.706h-.505a2.528 2.528 0 01-2.423-1.829l-.715-2.453z\" fill=\"url(#copilot_lobe-icons-copilot-0-_R_0_)\" transform=\"translate(0 1)\"></path><path d=\"M6.726 20.16A2.528 2.528 0 009.152 22h1.566c1.37 0 2.49-1.1 2.525-2.48l.17-6.69-.357 1.228a2.528 2.528 0 01-2.423 1.83h-4.32l-1.54-.842-1.667.843h.497c1.124 0 2.113.75 2.426 1.84l.697 2.432z\" fill=\"url(#copilot_lobe-icons-copilot-1-_R_0_)\" transform=\"translate(0 1)\"></path><path d=\"M15 0H6.252c-2.5 0-4 3.331-5 6.662-1.184 3.947-2.734 9.225 1.75 9.225H6.78c1.13 0 2.12-.753 2.43-1.847.657-2.317 1.809-6.359 2.713-9.436.46-1.563.842-2.906 1.43-3.742A1.97 1.97 0 0115 0\" fill=\"url(#copilot_lobe-icons-copilot-2-_R_0_)\" transform=\"translate(0 1)\"></path><path d=\"M15 0H6.252c-2.5 0-4 3.331-5 6.662-1.184 3.947-2.734 9.225 1.75 9.225H6.78c1.13 0 2.12-.753 2.43-1.847.657-2.317 1.809-6.359 2.713-9.436.46-1.563.842-2.906 1.43-3.742A1.97 1.97 0 0115 0\" fill=\"url(#copilot_lobe-icons-copilot-3-_R_0_)\" transform=\"translate(0 1)\"></path><path d=\"M9 22h8.749c2.5 0 4-3.332 5-6.663 1.184-3.948 2.734-9.227-1.75-9.227H17.22c-1.129 0-2.12.754-2.43 1.848a1149.2 1149.2 0 01-2.713 9.437c-.46 1.564-.842 2.907-1.43 3.743A1.97 1.97 0 019 22\" fill=\"url(#copilot_lobe-icons-copilot-4-_R_0_)\" transform=\"translate(0 1)\"></path><path d=\"M9 22h8.749c2.5 0 4-3.332 5-6.663 1.184-3.948 2.734-9.227-1.75-9.227H17.22c-1.129 0-2.12.754-2.43 1.848a1149.2 1149.2 0 01-2.713 9.437c-.46 1.564-.842 2.907-1.43 3.743A1.97 1.97 0 019 22\" fill=\"url(#copilot_lobe-icons-copilot-5-_R_0_)\" transform=\"translate(0 1)\"></path><defs><radialGradient cx=\"85.44%\" cy=\"100.653%\" fx=\"85.44%\" fy=\"100.653%\" gradientTransform=\"scale(-.8553 -1) rotate(50.927 2.041 -1.946)\" id=\"copilot_lobe-icons-copilot-0-_R_0_\" r=\"105.116%\"><stop offset=\"9.6%\" stop-color=\"#00AEFF\"></stop><stop offset=\"77.3%\" stop-color=\"#2253CE\"></stop><stop offset=\"100%\" stop-color=\"#0736C4\"></stop></radialGradient><radialGradient cx=\"18.143%\" cy=\"32.928%\" fx=\"18.143%\" fy=\"32.928%\" gradientTransform=\"scale(.8897 1) rotate(52.069 .193 .352)\" id=\"copilot_lobe-icons-copilot-1-_R_0_\" r=\"95.612%\"><stop offset=\"0%\" stop-color=\"#FFB657\"></stop><stop offset=\"63.4%\" stop-color=\"#FF5F3D\"></stop><stop offset=\"92.3%\" stop-color=\"#C02B3C\"></stop></radialGradient><radialGradient cx=\"82.987%\" cy=\"-9.792%\" fx=\"82.987%\" fy=\"-9.792%\" gradientTransform=\"scale(-1 -.9441) rotate(-70.872 .142 1.17)\" id=\"copilot_lobe-icons-copilot-4-_R_0_\" r=\"140.622%\"><stop offset=\"6.6%\" stop-color=\"#8C48FF\"></stop><stop offset=\"50%\" stop-color=\"#F2598A\"></stop><stop offset=\"89.6%\" stop-color=\"#FFB152\"></stop></radialGradient><linearGradient id=\"copilot_lobe-icons-copilot-2-_R_0_\" x1=\"39.465%\" x2=\"46.884%\" y1=\"12.117%\" y2=\"103.774%\"><stop offset=\"15.6%\" stop-color=\"#0D91E1\"></stop><stop offset=\"48.7%\" stop-color=\"#52B471\"></stop><stop offset=\"65.2%\" stop-color=\"#98BD42\"></stop><stop offset=\"93.7%\" stop-color=\"#FFC800\"></stop></linearGradient><linearGradient id=\"copilot_lobe-icons-copilot-3-_R_0_\" x1=\"45.949%\" x2=\"50%\" y1=\"0%\" y2=\"100%\"><stop offset=\"0%\" stop-color=\"#3DCBFF\"></stop><stop offset=\"24.7%\" stop-color=\"#0588F7\" stop-opacity=\"0\"></stop></linearGradient><linearGradient id=\"copilot_lobe-icons-copilot-5-_R_0_\" x1=\"83.507%\" x2=\"83.453%\" y1=\"-6.106%\" y2=\"21.131%\"><stop offset=\"5.8%\" stop-color=\"#F8ADFA\"></stop><stop offset=\"70.8%\" stop-color=\"#A86EDD\" stop-opacity=\"0\"></stop></linearGradient></defs>"},"amp":{"vb":"0 0 24 24","inner":"<path d=\"M15.087 23.18L12.03 24l-2.097-7.823-5.738 5.738-2.251-2.251 5.718-5.719-7.769-2.082.82-3.057 11.294 3.08 3.08 11.295z\" fill=\"#F34E3F\"></path><path d=\"M19.505 18.762l-3.057.82-2.564-9.573-9.572-2.564.819-3.057 11.295 3.079 3.08 11.295z\" fill=\"#F34E3F\"></path><path d=\"M23.893 14.374l-3.057.82-2.565-9.572L8.7 3.057 9.52 0l11.295 3.08 3.079 11.294z\" fill=\"#F34E3F\"></path>"},"cursor":{"vb":"0 0 24 24","inner":"<path d=\"M22.106 5.68L12.5.135a.998.998 0 00-.998 0L1.893 5.68a.84.84 0 00-.419.726v11.186c0 .3.16.577.42.727l9.607 5.547a.999.999 0 00.998 0l9.608-5.547a.84.84 0 00.42-.727V6.407a.84.84 0 00-.42-.726zm-.603 1.176L12.228 22.92c-.063.108-.228.064-.228-.061V12.34a.59.59 0 00-.295-.51l-9.11-5.26c-.107-.062-.063-.228.062-.228h18.55c.264 0 .428.286.296.514z\"></path>"},"grok":{"vb":"0 0 24 24","inner":"<path d=\"M9.27 15.29l7.978-5.897c.391-.29.95-.177 1.137.272.98 2.369.542 5.215-1.41 7.169-1.951 1.954-4.667 2.382-7.149 1.406l-2.711 1.257c3.889 2.661 8.611 2.003 11.562-.953 2.341-2.344 3.066-5.539 2.388-8.42l.006.007c-.983-4.232.242-5.924 2.75-9.383.06-.082.12-.164.179-.248l-3.301 3.305v-.01L9.267 15.292M7.623 16.723c-2.792-2.67-2.31-6.801.071-9.184 1.761-1.763 4.647-2.483 7.166-1.425l2.705-1.25a7.808 7.808 0 00-1.829-1A8.975 8.975 0 005.984 5.83c-2.533 2.536-3.33 6.436-1.962 9.764 1.022 2.487-.653 4.246-2.34 6.022-.599.63-1.199 1.259-1.682 1.925l7.62-6.815\"></path>"},"opencode":{"vb":"0 0 24 24","inner":"<path d=\"M16 6H8v12h8V6zm4 16H4V2h16v20z\"></path>"},"augment":{"vb":"0 0 512 512","inner":"<path d=\"M78.844 464.762c-8.453 0-15.573-1.451-21.359-4.339-5.77-2.888-10.144-7.289-13.076-13.095-2.932-5.807-4.436-12.912-4.436-21.255v-86.028c0-10.605-2.125-18.321-6.329-23.135-4.234-4.798-11.742-7.334-22.507-7.579-3.35 0-6.034-1.253-8.066-3.804C1.008 303.005 0 300.087 0 296.832c0-3.53 1.008-6.448 3.071-8.725 2.048-2.277 4.762-3.53 8.066-3.774 10.765-.26 18.273-2.781 22.507-7.579 4.235-4.798 6.329-12.392 6.329-22.752v-86.028c0-12.637 3.35-22.249 10.005-28.804 6.654-6.555 16.287-9.856 28.866-9.856H181.5c3.862 0 7.042 1.146 9.617 3.408 2.559 2.277 3.862 5.195 3.862 8.694 0 3.301-1.086 6.128-3.257 8.542-2.172 2.414-5.057 3.622-8.671 3.622H87.732c-5.413 0-9.508 1.39-12.316 4.171-2.823 2.781-4.234 7.075-4.234 12.912v86.425c0 7.579-1.551 14.455-4.623 20.644-3.07 6.204-7.181 11.063-12.316 14.623-5.134 3.53-11.137 5.302-18.07 5.302v-1.528c6.933 0 12.936 1.773 18.07 5.303 5.135 3.529 9.245 8.404 12.316 14.623 3.072 6.188 4.623 13.064 4.623 20.643v86.808c0 5.837 1.411 10.115 4.234 12.911 2.823 2.812 6.934 4.172 12.316 4.172h95.318c3.583 0 6.468 1.207 8.671 3.606 2.202 2.414 3.257 5.257 3.257 8.542s-1.272 6.097-3.862 8.511c-2.575 2.414-5.771 3.606-9.617 3.606H78.844v-.092ZM330.501 464.768c-3.862 0-7.042-1.207-9.617-3.606-2.575-2.414-3.863-5.256-3.863-8.511 0-3.255 1.086-6.128 3.258-8.542 2.171-2.414 5.057-3.606 8.671-3.606h95.317c5.414 0 9.509-1.36 12.316-4.171 2.823-2.781 4.235-7.075 4.235-12.912v-86.808c0-7.579 1.551-14.455 4.622-20.643 3.071-6.204 7.182-11.063 12.316-14.623 5.134-3.53 11.137-5.303 18.071-5.303v1.528c-6.934 0-12.937-1.772-18.071-5.302-5.134-3.53-9.245-8.404-12.316-14.623-3.071-6.189-4.622-13.065-4.622-20.644v-86.425c0-5.807-1.412-10.1-4.235-12.912-2.823-2.781-6.933-4.171-12.316-4.171H328.95c-3.583 0-6.469-1.208-8.671-3.622-2.172-2.384-3.258-5.241-3.258-8.542 0-3.529 1.272-6.417 3.863-8.694 2.559-2.277 5.755-3.407 9.617-3.407h102.654c12.58 0 22.181 3.3 28.867 9.855 6.685 6.556 10.005 16.167 10.005 28.804v86.028c0 10.36 2.125 17.969 6.328 22.752 4.235 4.798 11.742 7.334 22.507 7.579 3.351.244 6.034 1.497 8.066 3.774 2.063 2.277 3.071 5.195 3.071 8.725 0 3.301-1.008 6.189-3.071 8.695-2.032 2.521-4.762 3.804-8.066 3.804-10.765.245-18.257 2.781-22.507 7.579-4.234 4.798-6.328 12.5-6.328 23.135v86.028c0 8.358-1.474 15.418-4.437 21.255-2.962 5.837-7.305 10.176-13.076 13.095-5.785 2.888-12.905 4.339-21.359 4.339H330.501v.092Z\"></path><path d=\"M356.885 329.738c18.691 0 33.846-14.929 33.846-33.342 0-18.412-15.155-33.341-33.846-33.341-18.691 0-33.846 14.929-33.846 33.341 0 18.413 15.155 33.342 33.846 33.342ZM167.305 329.738c18.691 0 33.846-14.929 33.846-33.342 0-18.412-15.155-33.341-33.846-33.341-18.691 0-33.846 14.929-33.846 33.341 0 18.413 15.155 33.342 33.846 33.342ZM244.477 32.846l-2.59 68.135c0 3.82-3.661 5.73-10.983 5.73-7.321 0-10.982-1.91-10.982-5.73-.651-16.976-1.178-30.148-1.613-39.484-.217-9.55-.434-16.35-.651-20.384-.217-4.034-.326-6.479-.326-7.32v-1.268c0-4.874 4.529-7.319 13.572-7.319 9.044 0 13.573 2.552 13.573 7.64Zm54.941 0-2.59 68.135c0 3.82-3.661 5.73-10.982 5.73-7.322 0-10.982-1.91-10.982-5.73-.652-16.976-1.179-30.148-1.613-39.484-.218-9.55-.435-16.35-.652-20.384-.217-4.034-.326-6.479-.326-7.32v-1.268c0-4.874 4.53-7.319 13.573-7.319s13.572 2.552 13.572 7.64Z\"></path>"}}; /* __BRAND_ICONS__ */

// providerIcon renders the real brand mark when known, else the monogram chip.
function providerIcon(h, id, size) {
  var brand = PROVIDER_SVG[id];
  if (!brand) return providerMonoIcon(h, id, size);
  var s = size || 15;
  return h("svg", {
    xmlns: "http://www.w3.org/2000/svg",
    width: s,
    height: s,
    viewBox: brand.vb || "0 0 24 24",
    fill: "currentColor",
    "aria-hidden": "true",
    style: { flex: "0 0 auto" },
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
    p.detail ? h("div", { style: { fontSize: "12px", opacity: 0.7 } }, p.detail) : null,
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
            opacity: isActive ? 1 : 0.62,
          },
        },
        providerIcon(h, p.provider, 13),
        providerShort(p.provider),
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

// ==========================================================================
window.registerKandevPlugin("kandev-provider-usage", {
  initialize: function (registry, host) {
    registry.registerComponent("chat-top-bar", makeTopBarStatus(host));
  },
});
