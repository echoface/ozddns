#!/usr/bin/env node
// Browser-level regression for the Overview public-IP panel.
//
// Prerequisites:
//   - An isolated OzDDNS instance is running at OZDDNS_E2E_URL.
//   - Headless Chromium exposes a CDP endpoint at OZDDNS_CDP_URL.
//   - The instance already has active IPv4/IPv6 jobs using static sources.
//
// The script also transitions the isolated instance to "no active tasks" and
// "source failure" states through the real API, then verifies the rendered UI.

import fs from "node:fs/promises";

const baseURL = (process.env.OZDDNS_E2E_URL || "http://127.0.0.1:19876").replace(/\/$/, "");
const cdpURL = (process.env.OZDDNS_CDP_URL || "http://127.0.0.1:9224").replace(/\/$/, "");
const screenshotDir = process.env.OZDDNS_E2E_SCREENSHOT_DIR || "/tmp";
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

class CDP {
  constructor(wsURL) {
    this.wsURL = wsURL;
    this.nextID = 1;
    this.pending = new Map();
  }

  async connect() {
    this.ws = new WebSocket(this.wsURL);
    await new Promise((resolve, reject) => {
      this.ws.addEventListener("open", resolve, { once: true });
      this.ws.addEventListener("error", reject, { once: true });
    });
    this.ws.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (!message.id) return;
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      if (message.error) pending.reject(new Error(`${pending.method}: ${message.error.message}`));
      else pending.resolve(message.result);
    });
  }

  call(method, params = {}) {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, method });
      this.ws.send(JSON.stringify({ id, method, params }));
    });
  }

  close() {
    this.ws?.close();
  }
}

async function evaluate(cdp, expression, awaitPromise = false) {
  const result = await cdp.call("Runtime.evaluate", {
    expression,
    awaitPromise,
    returnByValue: true,
    userGesture: true
  });
  if (result.exceptionDetails) {
    throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
  }
  return result.result.value;
}

async function waitFor(cdp, expression, description, timeout = 15_000) {
  const started = Date.now();
  while (Date.now() - started < timeout) {
    try {
      if (await evaluate(cdp, expression)) return;
    } catch {
      // Navigation can briefly invalidate the execution context.
    }
    await sleep(100);
  }
  throw new Error(`Timed out waiting for ${description}`);
}

async function navigateRoute(cdp, route, language = "") {
  const languageQuery = language ? `&lang=${encodeURIComponent(language)}` : "";
  await cdp.call("Page.navigate", {
    url: `${baseURL}/?e2e=${Date.now()}${languageQuery}#${route}`
  });
  await waitFor(cdp, `location.hash === ${JSON.stringify(`#${route}`)}`, route);
  await sleep(300);
}

async function navigate(cdp, language = "") {
  await navigateRoute(cdp, "/overview", language);
  await waitFor(cdp, `!!document.querySelector(".ip-hero")`, "Overview");
  await sleep(300);
}

async function heroText(cdp) {
  return evaluate(cdp, `document.querySelector(".ip-hero")?.innerText || ""`);
}

async function api(cdp, path, options) {
  return evaluate(cdp, `fetch(${JSON.stringify(`/api/v1${path}`)}, ${JSON.stringify(options || {})})
    .then(async (response) => {
      if (!response.ok) throw new Error(await response.text());
      const text = await response.text();
      return text ? JSON.parse(text) : null;
    })`, true);
}

async function screenshot(cdp, name) {
  const result = await cdp.call("Page.captureScreenshot", { format: "png", fromSurface: true });
  const path = `${screenshotDir}/${name}`;
  await fs.writeFile(path, Buffer.from(result.data, "base64"));
  return path;
}

const targets = await fetch(`${cdpURL}/json/list`).then((response) => response.json());
const page = targets.find((target) => target.type === "page");
assert(page?.webSocketDebuggerUrl, "no Chrome page target available");

const cdp = new CDP(page.webSocketDebuggerUrl);
await cdp.connect();
try {
  await cdp.call("Page.enable");
  await cdp.call("Runtime.enable");
  await cdp.call("Emulation.setDeviceMetricsOverride", {
    width: 1280,
    height: 1100,
    deviceScaleFactor: 1,
    mobile: false
  });

  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("203.0.113.50")`,
    "active IPv4 address"
  );
  let hero = await heroText(cdp);
  assert(hero.includes("2001:db8::50"), "active IPv6 address is missing");
  const normalizedHero = hero.toLowerCase();
  assert(normalizedHero.includes("public ipv4"), "IPv4 overview panel is missing");
  assert(normalizedHero.includes("public ipv6"), "IPv6 overview panel is missing");
  assert(hero.includes("used by 1 active task"), "active task count is missing");
  assert(!hero.includes("ozddns-no-such-interface"), "unused stale source leaked into Overview");
  console.log("PASS  Overview follows active task sources");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-active.png")}`);

  // A task with no history must say "Not run" rather than claiming "No change"
  // or "In sync" before the reconciliation engine has ever touched it.
  await api(cdp, "/scheduler", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ running: false })
  });
  const providers = await api(cdp, "/providers");
  assert(providers.length > 0, "test instance has no provider");
  const notRunSource = await api(cdp, "/ip-sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: "e2e-not-run-v4",
      type: "static",
      family: "v4",
      target: "203.0.113.77"
    })
  });
  const notRunJob = await api(cdp, "/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: "not-run-yet",
      providerId: providers[0].id,
      ipSourceId: notRunSource.id,
      targets: [{ domain: "not-run.example.com", type: "A", ttl: 300 }],
      intervalMinutes: 5
    })
  });
  await navigateRoute(cdp, "/jobs");
  await waitFor(
    cdp,
    `[...document.querySelectorAll("tr")].some((row) => row.innerText.includes("not-run-yet") && row.innerText.includes("Not run"))`,
    "not-run task status"
  );
  await navigateRoute(cdp, `/jobs/${encodeURIComponent(notRunJob.id)}`);
  await waitFor(cdp, `document.body.innerText.includes("Not run yet")`, "not-run task detail");
  const detailText = await evaluate(cdp, "document.body.innerText");
  assert(!detailText.includes("In sync"), "never-run task must not claim to be in sync");
  console.log("PASS  New tasks are explicitly marked Not run until their first execution");
  console.log(`      screenshot: ${await screenshot(cdp, "job-not-run.png")}`);
  await api(cdp, `/jobs/${encodeURIComponent(notRunJob.id)}`, { method: "DELETE" });
  await api(cdp, `/ip-sources/${encodeURIComponent(notRunSource.id)}`, { method: "DELETE" });

  const jobs = await api(cdp, "/jobs");
  for (const job of jobs) {
    await api(cdp, `/jobs/${encodeURIComponent(job.id)}`, { method: "DELETE" });
  }
  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("No enabled IPv4 task references an IP source.")`,
    "no-active-task state"
  );
  hero = await heroText(cdp);
  assert(hero.includes("Not configured"), "no-active-task state should be explicit");
  assert(!hero.includes("just now"), "no-active-task state must not look like a fresh successful check");
  console.log("PASS  Overview distinguishes no active tasks from a successful check");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-no-tasks.png")}`);

  const source = await api(cdp, "/ip-sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: "broken v6 command",
      type: "command",
      family: "v6",
      target: "exit 7"
    })
  });
  await api(cdp, "/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: "failing-v6",
      providerId: providers[0].id,
      ipSourceId: source.id,
      targets: [{ domain: "broken.example.com", type: "AAAA", ttl: 300 }],
      intervalMinutes: 5
    })
  });
  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("broken v6 command")`,
    "failed-source state"
  );
  hero = await heroText(cdp);
  assert(hero.includes("Check failed"), "failed source should have an error state");
  assert(hero.includes("command failed"), "failed source should expose the concrete error");
  console.log("PASS  Overview exposes source failures instead of blank success");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-source-error.png")}`);

  await navigate(cdp, "zh-CN");
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("检查失败")`,
    "Chinese failure state"
  );
  hero = await heroText(cdp);
  assert(hero.includes("没有启用中的 IPv4 任务引用 IP 来源。"), "Chinese no-task copy is missing");
  assert(hero.includes("broken v6 command"), "Chinese state lost the source name");
  console.log("PASS  Overview states are translated in Chinese");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-source-error-zh.png")}`);
} finally {
  cdp.close();
}
