// Render the real plugin bundle with React and a synthetic host/API. No account,
// CLI login, credential store, or external provider is accessed.
// BROWSER_DEPS_DIR must contain esbuild, react, react-dom, @playwright/test.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const deps = path.resolve(process.env.BROWSER_DEPS_DIR || path.join(repo, ".build/browser-tools"));
const require = createRequire(path.join(deps, "package.json"));
const { build } = require("esbuild");
const { chromium } = require("@playwright/test");
const output = path.join(repo, ".build/auth-recovery");
mkdirSync(output, { recursive: true });
const source = readFileSync(process.env.PLUGIN_UI_BUNDLE || path.join(repo, "ui/bundle.js"), "utf8");
const app = await build({
  stdin: { contents: `
    import React from "react";
    import { createRoot } from "react-dom/client";
    const host = { React, jsx: React.createElement, ui: {
      Button: ({variant, children, ...props}) => React.createElement('button', props, children),
      Card: ({children, ...props}) => React.createElement('article', props, children),
    }, api: {fetch: (url, init) => fetch('https://fixture.invalid/' + url, init)} };
    const components = new Map();
    window.fixturePlugin.initialize({registerComponent: (name, component) => components.set(name, component)}, host);
    createRoot(document.getElementById('app')).render(React.createElement(components.get('plugin-settings')));
  `, resolveDir: deps },
  bundle: true, write: false, format: "iife", platform: "browser", define: { "process.env.NODE_ENV": '"production"' },
});
const browser = await chromium.launch({ headless: true });
const results = [];
try {
  for (const phone of [false, true]) {
    const context = await browser.newContext({ viewport: phone ? {width:390,height:844} : {width:1280,height:1000}, isMobile:phone, hasTouch:phone, deviceScaleFactor:1 });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    let selected = "99", authorized = false;
    const requests = [];
    const command = "'/managed cache/long directory/O'\"'\"'Brien/CodexBarCLI' 'config' 'login' '--provider' 'copilot'";
    await context.route("https://fixture.invalid/**", async route => {
      const request = route.request(), endpoint = new URL(request.url()).pathname.slice(1).split("?")[0];
      if (request.method() === "OPTIONS") {
        await route.fulfill({status:204,headers:{"Access-Control-Allow-Origin":"*","Access-Control-Allow-Methods":"GET, POST, OPTIONS","Access-Control-Allow-Headers":"Content-Type"}});
        return;
      }
      requests.push({endpoint, method:request.method()});
      let body, status = 200;
      const detected = [{provider:"cursor",via:"app"},{provider:"copilot",via:"cli"}];
      if (endpoint === "config") body = {config:{}};
      else if (endpoint === "webhooks/discovery") body = {detected_providers:detected};
      else if (endpoint === "webhooks/cursor-teams") {
        status = 502;
        body = {selected_team_id:selected,selection_known:true,teams:[],error:"Cursor desktop Safe Storage access was denied or requires interaction. Background reads never prompt. Use Cursor · Session cookie, or choose and save Account usage (automatic)."};
      } else if (endpoint === "webhooks/cursor-team") {
        assert.equal(request.method(), "POST");
        assert.deepEqual(request.postDataJSON(), {team_id:""});
        selected = ""; body = {selected_team_id:selected};
      } else if (endpoint === "webhooks/providers") {
        const providers = [], unavailable = [];
        if (selected) unavailable.push({provider:"cursor",code:"cursor_team_lookup",account_usage_available:true,message:"Cursor desktop Safe Storage access was denied or requires interaction."});
        else providers.push({provider:"cursor",source:"web",plan:"Business",windows:[{label:"Total Usage",utilization_pct:20}],detail_warning:"Cursor desktop Safe Storage access was denied."});
        if (authorized) providers.push({provider:"copilot",source:"api",windows:[{label:"Premium interactions",utilization_pct:12}]});
        else unavailable.push({provider:"copilot",code:"copilot_authorization",message:"No available fetch strategy for copilot.",setup:{command,shell:"POSIX shell",hint:"Copilot CLI sign-in and CodexBar authorization are separate. Authorize CodexBar on the machine running Kandev, using the same operating-system user and CodexBar configuration environment. Complete GitHub's device authorization, then refresh usage."}});
        body = {providers,unavailable,detected_providers:detected,generated_at:new Date().toISOString(),codexbar:{installed:true,version:"0.83.0",source:"download",command:"/managed cache/CodexBarCLI"}};
      } else throw new Error("Unexpected fixture request: " + endpoint);
      await route.fulfill({status,contentType:"application/json",headers:{"Access-Control-Allow-Origin":"*"},body:JSON.stringify(body)});
    });
    await page.setContent(`<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><style>
      :root {--background:#fff;--foreground:#212125;--muted:#f3f4f6;--muted-foreground:#65656c;--border:#dedee3}
      *{box-sizing:border-box}body{margin:0;background:#f8f9fb;color:#212125;font:14px system-ui}main{max-width:1100px;padding:16px;margin:auto}
      button,input,select{font:inherit}button{min-height:44px;padding:8px 12px;border:1px solid #dedee3;border-radius:6px;background:white;color:inherit;cursor:pointer}
      button:disabled{opacity:.5;cursor:default}article{border:1px solid #dedee3;border-radius:10px;background:white}label{line-height:1.5}
    </style><main id="app"></main>`);
    await page.addScriptTag({content:"window.registerKandevPlugin = (_id, plugin) => window.fixturePlugin = plugin;"});
    await page.addScriptTag({content:source});
    await page.addScriptTag({content:app.outputFiles[0].text});
    const cursor = page.locator('[data-provider-settings="cursor"]');
    const copilot = page.locator('[data-provider-settings="copilot"]');
    await cursor.locator(":scope > summary").click();
    await copilot.locator(":scope > summary").click();
    await page.getByRole("combobox", {name:"Cursor team",exact:true}).waitFor();
    await page.getByText("Selected team lookup failed", {exact:true}).first().waitFor();
    await page.getByText("CodexBar returned account usage", {exact:false}).waitFor();
    const code = page.getByLabel("Copilot authorization command");
    assert.equal(await code.textContent(), command);
    await code.focus();
    assert.equal(await code.evaluate(node => node === document.activeElement), true);
    assert.equal(await page.getByRole("combobox", {name:"Cursor team",exact:true}).inputValue(), "99");
    assert.equal(requests.some(r => r.method === "POST"), false, "failed lookup must not change scope");
    await page.screenshot({path:path.join(output, phone ? "phone-auth-failure.png" : "desktop-auth-failure.png"),fullPage:true});
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, "no document horizontal overflow");
    const picker = page.getByRole("combobox", {name:"Cursor team",exact:true});
    const save = page.getByRole("button", {name:"Save team",exact:true});
    assert.ok((await picker.boundingBox()).height >= 44);
    assert.ok((await save.boundingBox()).height >= 44);
    await picker.selectOption("");
    await save.click();
    await cursor.getByText("Connected", {exact:true}).waitFor();
    authorized = true; // Simulate an external, user-initiated device authorization.
    await page.getByRole("button", {name:"Refresh usage",exact:true}).click();
    await copilot.getByText("Connected", {exact:true}).waitFor();
    await copilot.getByText("Premium interactions", {exact:false}).first().waitFor();
    assert.equal(await code.count(), 0, "login notice clears only after returned usage");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    assert.deepEqual(errors, []);
    await page.screenshot({path:path.join(output, phone ? "phone-recovered.png" : "desktop-recovered.png"),fullPage:true});
    results.push({viewport:phone?"390×844 touch emulation":"1280×1000 desktop",browser:browser.version(),status:"passed",scopeWrites:requests.filter(r=>r.method==="POST").length});
    await context.close();
  }
} finally { await browser.close(); }
writeFileSync(path.join(output,"results.json"),JSON.stringify(results,null,2)+"\n");
console.log(JSON.stringify(results,null,2));
