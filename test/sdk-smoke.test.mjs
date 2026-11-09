import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:http";
import { once } from "node:events";
import { readFileSync } from "node:fs";
import test from "node:test";

const version = readFileSync(
  new URL("../manifest.yaml", import.meta.url),
  "utf8",
).match(/^version: "([^"]+)"$/m)[1];

async function rejectInvalidHost(overrides, expectedMessage) {
  const responses = {
    "/health": { version: "v0.99.0" },
    "/api/plugins/kandev-provider-usage": { status: "active", version },
    "/api/plugins/kandev-provider-usage/webhooks/status": { installed: true },
    "/api/plugins/kandev-provider-usage/webhooks/providers": { providers: [] },
    ...overrides,
  };
  const server = createServer((req, res) => {
    const body = responses[req.url];
    res.writeHead(body ? 200 : 418, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify(
        body || { error: "Unexpected request after validation guard" },
      ),
    );
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    const child = spawn(
      process.execPath,
      [new URL("host-smoke/sdk-migration.mjs", import.meta.url).pathname],
      {
        env: {
          ...process.env,
          SMOKE_BASE_URL: `http://127.0.0.1:${server.address().port}`,
        },
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let stderr = "";
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    child.stdout.resume();
    const [code] = await once(child, "close");
    assert.notEqual(code, 0);
    assert.match(stderr, expectedMessage);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test("installed smoke rejects an older active plugin", () =>
  rejectInvalidHost(
    {
      "/api/plugins/kandev-provider-usage": {
        status: "active",
        version: "0.0.0",
      },
    },
    /Installed plugin version differs from manifest/,
  ));

test("installed smoke rejects a 200 status report with no CLI", () =>
  rejectInvalidHost(
    {
      "/api/plugins/kandev-provider-usage/webhooks/status": {
        installed: false,
      },
    },
    /CodexBar fixture is not installed/,
  ));

test("installed smoke rejects a 200 provider report without fixture usage", () =>
  rejectInvalidHost({}, /Codex fixture usage was not loaded/));
