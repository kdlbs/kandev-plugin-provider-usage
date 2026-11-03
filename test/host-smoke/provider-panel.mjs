// Render the real bundle with React and synthetic provider data. No provider
// account, credential store, or local CLI is accessed.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const deps = path.resolve(process.env.BROWSER_DEPS_DIR || path.join(repo, ".build/browser-tools"));
const require = createRequire(path.join(deps, "package.json"));
const { build } = require("esbuild");
const { chromium, expect } = require("@playwright/test");
const output = path.join(repo, ".build/provider-panel");
mkdirSync(output, { recursive: true });
const source = readFileSync(process.env.PLUGIN_UI_BUNDLE || path.join(repo, "ui/bundle.js"), "utf8");
const app = await build({ stdin: { contents: `
  import React from "react";
  import { createRoot } from "react-dom/client";
  const host = { React, jsx: React.createElement, ui: {
    Button: ({variant, size, children, ...props}) => React.createElement('button', props, children),
    Card: ({children, ...props}) => React.createElement('article', props, children),
    Action: ({label, icon, text, tooltip, ...props}) => React.createElement('button', {...props, 'aria-label':label}, icon, text),
  }, api: {fetch: (url, init) => fetch('https://fixture.invalid/' + url, init)} };
  const components = new Map();
  window.fixturePlugin.initialize({registerComponent: (name, component) => components.set(name, component)}, host);
  const root = createRoot(document.getElementById('app'));
  window.renderView = (slot, slotProps = {}) => root.render(React.createElement(components.get(slot), {key:slot, slotProps}));
`, resolveDir: deps }, bundle: true, write: false, format: "iife", platform: "browser",
  define: { "process.env.NODE_ENV": '"production"' } });
const browser = await chromium.launch({ headless: true });
const results = [];
try {
  for (const phone of [false, true]) {
    const context = await browser.newContext({ viewport: phone ? {width:390,height:844} : {width:1280,height:900},
      isMobile:phone, hasTouch:phone, deviceScaleFactor:1 });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    const now = new Date().toISOString();
    const reset = new Date(Date.now() + 28 * 86400000).toISOString();
    const providers = [
      {provider:"codex",plan:"Pro",windows:[{label:"5-hour",utilization_pct:17}]},
      {provider:"copilot",plan:"Business",windows:[],copilot_credits:{used:3313,allowance:25000,allowance_source:"settings",reset_at:reset}},
      {provider:"cursor",plan:"Cursor Team",windows:[{label:"Total Usage",utilization_pct:4.3}]},
      {provider:"antigravity",plan:"Google AI Pro",windows:[{label:"Gemini models",utilization_pct:25}]},
      {provider:"claude",windows:[{label:"5-hour",utilization_pct:30}]},
      {provider:"augment",windows:[],detail:"1,234 credits this month"},
    ].map(p => ({...p,fetched_at:now}));
    const data = {providers,detected_providers:providers.map(p=>({provider:p.provider,via:"cli"})),
      generated_at:now,codexbar:{installed:true,version:"0.83.0"},pill_providers:["copilot"],
      status_bar_mode:"both",warn_threshold:75,high_threshold:90};
    await context.route("https://fixture.invalid/**", async route => {
      const endpoint = new URL(route.request().url()).pathname.slice(1).split("?")[0];
      const body = endpoint === "config" ? {config:{copilot_monthly_credits:25000}} : data;
      await route.fulfill({contentType:"application/json",headers:{"Access-Control-Allow-Origin":"*"},body:JSON.stringify(body)});
    });
    await page.setContent(`<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><style>
      :root{--background:#1c1c1e;--foreground:#e4e4e7;--muted:#29292d;--muted-foreground:#9a9aa2;--border:#34343a;--ring:#8186ec}
      *{box-sizing:border-box}body{margin:0;background:#151517;color:var(--foreground);font:14px system-ui}
      main{padding:24px;max-width:1100px;margin:auto}button,input,select{font:inherit}
      button{border:1px solid var(--border);border-radius:8px;background:var(--background);color:inherit;padding:8px 12px;cursor:pointer}
      #provider-usage-topbar{display:inline-flex;gap:8px;align-items:center;min-height:44px}
      article{border:1px solid var(--border);border-radius:10px;background:var(--background)}
    </style><main id="app"></main>`);
    await page.addScriptTag({content:"window.registerKandevPlugin = (_id, plugin) => window.fixturePlugin = plugin;"});
    await page.addScriptTag({content:source});
    await page.addScriptTag({content:app.outputFiles[0].text});
    await page.evaluate(phone => window.renderView("main-top-bar", phone ? {presentation:"mobile"} : {}), phone);
    const trigger = page.getByRole("button", {name:"Provider usage",exact:true});
    await trigger.waitFor();
    if (phone) await trigger.tap(); else await trigger.focus();
    const tabs = page.getByRole("group", {name:"Usage providers",exact:true});
    await tabs.waitFor();
    await expect(tabs).toHaveAttribute("data-overflow-start", "false");
    await expect(tabs).toHaveAttribute("data-overflow-end", "true");
    const before = await tabs.evaluate(node => ({height:node.clientHeight,color:getComputedStyle(node).scrollbarColor,mask:getComputedStyle(node).maskImage}));
    assert.match(before.color, /rgba\(0, 0, 0, 0\)/, "idle scrollbar thumb is transparent");
    assert.match(before.mask, /linear-gradient/, "overflow uses a gradient mask");
    if (!phone) {
      await tabs.hover();
      assert.notEqual(await tabs.evaluate(node => getComputedStyle(node).scrollbarColor), before.color, "hover reveals the scrollbar");
      assert.equal(await tabs.evaluate(node => node.clientHeight), before.height, "hover does not change row height");
      const first = tabs.getByRole("button").first();
      await first.focus();
      await page.mouse.move(5,5);
      assert.notEqual(await tabs.evaluate(node => getComputedStyle(node).scrollbarColor), before.color, "keyboard focus reveals the scrollbar");
    }
    await tabs.evaluate(node => {node.scrollLeft = (node.scrollWidth - node.clientWidth) / 2;});
    await expect(tabs).toHaveAttribute("data-overflow-start", "true");
    await expect(tabs).toHaveAttribute("data-overflow-end", "true");
    await tabs.evaluate(node => {node.scrollLeft = node.scrollWidth;});
    await expect(tabs).toHaveAttribute("data-overflow-end", "false");
    const antigravityTab = tabs.getByRole("button", {name:"Antigravity",exact:true});
    if (phone) await antigravityTab.tap(); else await antigravityTab.click();
    await expect(tabs.getByRole("button", {name:"Antigravity",exact:true})).toHaveAttribute("aria-pressed", "true");
    assert.ok(await tabs.getByRole("button", {name:"Antigravity",exact:true}).locator("svg").count());
    assert.equal(await tabs.getByRole("button", {name:"Gemini",exact:true}).count(), 0);
    const copilotTab = tabs.getByRole("button", {name:"GitHub Copilot",exact:true});
    if (phone) await copilotTab.tap(); else await copilotTab.click();
    await page.getByText("3,313 / 25,000 AI credits", {exact:true}).waitFor();
    await expect(page.getByRole("meter", {name:"AI credit allowance used",exact:true})).toHaveAttribute("aria-valuenow", "3313");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    if (phone) assert.ok((await tabs.getByRole("button").first().boundingBox()).height >= 44);
    await page.screenshot({path:path.join(output,phone?"phone-overlay.png":"desktop-overlay.png"),fullPage:true});
    await page.evaluate(() => window.renderView("plugin-settings"));
    const copilot = page.locator('[data-provider-settings="copilot"]');
    await copilot.locator(":scope > summary").click();
    await copilot.getByText("3,313 / 25,000 AI credits",{exact:true}).waitFor();
    await expect(page.getByLabel("Included AI credits",{exact:true})).toHaveValue("25000");
    await page.screenshot({path:path.join(output,phone?"phone-settings.png":"desktop-settings.png"),fullPage:true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.evaluate(() => window.renderView("app-status-bar-right", {presentation:"mobile-drawer"}));
    await page.getByText("3,313 / 25,000 AI credits", {exact:true}).waitFor();
    for (const used of [3313, 0]) {
      data.providers.find(p => p.provider === "copilot").copilot_credits = {used};
      await page.evaluate(() => window.renderView("main-top-bar"));
      await page.getByRole("button", {name:"Provider usage",exact:true}).waitFor();
      await page.evaluate(() => window.renderView("app-status-bar-right", {presentation:"mobile-drawer"}));
      await expect(page.getByText("AI credit usage", {exact:true})).toBeVisible();
      await expect(page.getByText(used.toLocaleString("en-US") + " AI credits", {exact:true})).toBeVisible();
      await expect(page.getByText("Usage unavailable", {exact:true})).toHaveCount(0);
      await page.screenshot({path:path.join(output,(phone?"phone":"desktop") + "-credits-" + used + ".png"),fullPage:true});
    }
    assert.deepEqual(errors, []);
    results.push({viewport:phone?"390×844 touch emulation":"1280×900 desktop",browser:browser.version(),status:"passed",
      checks:["edge fades",...(phone?["touch activation","44px tab targets"]:["scrollbar hover/focus","no layout shift"]),
        "tab selection","Antigravity branding","exact Copilot credits","allowance setting","Status drawer",
        "unknown allowance and observed zero","no horizontal page overflow"]});
    await context.close();
  }
} finally { await browser.close(); }
writeFileSync(path.join(output,"results.json"),JSON.stringify(results,null,2)+"\n");
console.log(JSON.stringify(results,null,2));
