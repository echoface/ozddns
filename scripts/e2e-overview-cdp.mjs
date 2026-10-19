#!/usr/bin/env node
// Browser-level regression for the Overview public-IP inspectors.
//
// Prerequisites:
//   - An isolated OzDDNS instance is running at OZDDNS_E2E_URL.
//   - Headless Chromium exposes a CDP endpoint at OZDDNS_CDP_URL.
//
// The script creates its own static sources, verifies source switching and
// browser persistence, then exercises source failure, Chinese copy and mobile
// overflow for the real production build.

import fs from "node:fs/promises";

const baseURL = (process.env.OZDDNS_E2E_URL || "http://127.0.0.1:19876").replace(/\/$/, "");
const cdpURL = (process.env.OZDDNS_CDP_URL || "http://127.0.0.1:9224").replace(/\/$/, "");
const screenshotDir = process.env.OZDDNS_E2E_SCREENSHOT_DIR || "/tmp";
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const runID = Date.now().toString(36);
const created = { providerID: "", sourceIDs: [], jobIDs: [] };

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

class CDP {
  constructor(wsURL, onEvent) {
    this.wsURL = wsURL;
    this.onEvent = onEvent;
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
      if (!message.id) {
        this.onEvent?.(message);
        return;
      }
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

async function createSource(cdp, name, family, target, type = "static") {
  const source = await api(cdp, "/ip-sources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, type, family, target })
  });
  created.sourceIDs.push(source.id);
  return source;
}

async function selectSources(cdp, v4ID, v6ID) {
  await evaluate(cdp, `(() => {
    localStorage.setItem("oz.overview.ipv4SourceId", ${JSON.stringify(v4ID)});
    localStorage.setItem("oz.overview.ipv6SourceId", ${JSON.stringify(v6ID)});
  })()`);
}

async function screenshot(cdp, name) {
  const result = await cdp.call("Page.captureScreenshot", { format: "png", fromSurface: true });
  const path = `${screenshotDir}/${name}`;
  await fs.writeFile(path, Buffer.from(result.data, "base64"));
  return path;
}

const consoleErrors = [];
const targets = await fetch(`${cdpURL}/json/list`).then((response) => response.json());
const page = targets.find((target) => target.type === "page");
assert(page?.webSocketDebuggerUrl, "no Chrome page target available");

const cdp = new CDP(page.webSocketDebuggerUrl, (message) => {
  if (message.method === "Runtime.exceptionThrown") {
    consoleErrors.push(message.params?.exceptionDetails?.exception?.description || "uncaught exception");
  }
  if (message.method === "Runtime.consoleAPICalled" && message.params?.type === "error") {
    consoleErrors.push("console.error");
  }
});

let initialSchedulerRunning = true;
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
  const status = await api(cdp, "/status");
  initialSchedulerRunning = status.scheduler === "running";
  await api(cdp, "/scheduler", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ running: false })
  });

  // Start from a clean job list so the public-IP inspectors cannot be mistaken
  // for task-derived state.
  const existingJobs = await api(cdp, "/jobs");
  for (const job of existingJobs) {
    await api(cdp, `/jobs/${encodeURIComponent(job.id)}`, { method: "DELETE" });
  }

  let providers = await api(cdp, "/providers");
  if (!providers.length) {
    const provider = await api(cdp, "/providers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: `e2e-overview-${runID}`,
        type: "custom",
        config: { url: "http://127.0.0.1:9/hook", method: "POST" }
      })
    });
    created.providerID = provider.id;
    providers = [provider];
  }

  const v4A = await createSource(cdp, `e2e-v4-a-${runID}`, "v4", "203.0.113.50");
  const v4B = await createSource(cdp, `e2e-v4-b-${runID}`, "v4", "203.0.113.51");
  const v6A = await createSource(cdp, `e2e-v6-a-${runID}`, "v6", "2001:db8::50");

  await selectSources(cdp, v4A.id, v6A.id);
  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("203.0.113.50")`,
    "selected IPv4 address"
  );
  let hero = await heroText(cdp);
  assert(hero.includes("2001:db8::50"), "selected IPv6 address is missing");
  assert(hero.includes(v4A.name) && hero.includes(v6A.name), "selected source names are missing");
  assert((await api(cdp, "/jobs")).length === 0, "source inspection unexpectedly depends on a task");
  assert(
    await evaluate(cdp, `document.querySelectorAll(".grid.cols-3 .stat").length === 3`),
    "Overview should show three non-IP summary cards"
  );
  assert(
    await evaluate(cdp, `![...document.querySelectorAll(".stat .lab")].some((node) => node.innerText.includes("Public IPv4"))`),
    "Public IPv4 summary tile still duplicates the IP inspector"
  );
  assert(
    await evaluate(cdp, `(() => {
      const trigger = document.querySelector("#overview-ipv4-source");
      const arrow = trigger?.parentElement?.querySelector(".select-arrow");
      if (!trigger || !arrow || trigger.tagName !== "SELECT") return false;
      const t = trigger.getBoundingClientRect();
      const a = arrow.getBoundingClientRect();
      const style = getComputedStyle(trigger);
      return style.appearance === "none" &&
        parseFloat(style.paddingRight) >= 40 &&
        a.width > 0 && a.right <= t.right - 10 && a.left >= t.left &&
        [...trigger.options].some((option) => option.value === ${JSON.stringify(v4A.id)}) &&
        [...trigger.options].some((option) => option.value === ${JSON.stringify(v4B.id)});
    })()`),
    "the native source select is not consistently skinned or its chevron overlaps the edge"
  );
  console.log("PASS  Overview shows one selected source per family without requiring a task");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-selected-sources.png")}`);

  await waitFor(
    cdp,
    `(() => {
      const button = document.querySelector(".ip-actions > button");
      const hint = document.querySelector(".ip-action-hint");
      if (!button || !hint || button.disabled || !button.textContent.includes("Refresh IPv4 + IPv6")) return false;
      const b = button.getBoundingClientRect();
      const h = hint.getBoundingClientRect();
      const style = getComputedStyle(hint);
      return style.whiteSpace === "nowrap" && style.overflow === "hidden" &&
        Math.abs((b.top + b.bottom) / 2 - (h.top + h.bottom) / 2) <= 1;
    })()`,
    "shared refresh action with an inline IPv6 hint"
  );
  const goodLayout = await evaluate(cdp, `(() => {
    const card = document.querySelector(".ip-hero").closest(".card");
    return {
      cardHeight: card.getBoundingClientRect().height,
      addressHeights: [...document.querySelectorAll(".ip-address-slot")].map((node) => node.getBoundingClientRect().height),
      metaHeights: [...document.querySelectorAll(".ip-meta-slot")].map((node) => node.getBoundingClientRect().height)
    };
  })()`);
  assert(goodLayout.addressHeights.every((height) => Math.abs(height - 40) <= 0.5), "address rows are not fixed at 40px");
  assert(goodLayout.metaHeights.every((height) => Math.abs(height - 20) <= 0.5), "metadata rows are not fixed at 20px");
  assert(
    !(await evaluate(cdp, `document.querySelector(".ip-actions").innerText.includes("Checks both selected sources together.")`)),
    "redundant refresh helper copy is still visible"
  );
  assert(
    await evaluate(cdp, `!document.querySelector(".ip-status-slot .ip-action-hint")`),
    "the IPv6 hint still occupies the status region"
  );
  assert(await evaluate(cdp, `!document.querySelector(".ip-status-slot")`), "the standalone error region is still rendered");
  await evaluate(cdp, `(() => {
    performance.clearResourceTimings();
    document.querySelector(".ip-actions > button").click();
  })()`);
  await waitFor(
    cdp,
    `performance.getEntriesByType("resource").some((entry) => {
      const url = new URL(entry.name);
      return url.pathname === "/api/v1/ip" &&
        url.searchParams.get("v4SourceId") === ${JSON.stringify(v4A.id)} &&
        url.searchParams.get("v6SourceId") === ${JSON.stringify(v6A.id)};
    })`,
    "one refresh request carrying both source families"
  );
  assert(
    await evaluate(cdp, `performance.getEntriesByType("resource")
      .filter((entry) => new URL(entry.name).pathname === "/api/v1/ip").length === 1`),
    "Refresh IPv4 + IPv6 should issue exactly one combined request"
  );
  console.log("PASS  One clearly labelled refresh action re-checks both selected families in one request");

  await evaluate(cdp, `(() => {
    const select = document.querySelector("#overview-ipv4-source");
    select.value = ${JSON.stringify(v4B.id)};
    select.dispatchEvent(new Event("change", { bubbles: true }));
  })()`);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("203.0.113.51")`,
    "switched IPv4 source"
  );
  assert(
    await evaluate(cdp, `localStorage.getItem("oz.overview.ipv4SourceId") === ${JSON.stringify(v4B.id)}`),
    "IPv4 source choice was not persisted"
  );

  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector("#overview-ipv4-source")?.value === ${JSON.stringify(v4B.id)} &&
      document.querySelector(".ip-hero")?.innerText.includes("203.0.113.51")`,
    "persisted IPv4 source"
  );
  console.log("PASS  Native source select remains keyboard-accessible and survives a reload through localStorage");

  // A task with no history must say "Not run" rather than claiming "No change"
  // or "In sync" before the reconciliation engine has ever touched it.
  const notRunJob = await api(cdp, "/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: `not-run-yet-${runID}`,
      providerId: providers[0].id,
      ipSourceId: v4B.id,
      targets: [{ domain: "not-run.example.com", type: "A", ttl: 300 }],
      intervalMinutes: 5
    })
  });
  created.jobIDs.push(notRunJob.id);
  await navigateRoute(cdp, "/jobs");
  await waitFor(
    cdp,
    `[...document.querySelectorAll("tr")].some((row) =>
      row.innerText.includes(${JSON.stringify(notRunJob.name)}) && row.innerText.includes("Not run"))`,
    "not-run task status"
  );
  await navigateRoute(cdp, `/jobs/${encodeURIComponent(notRunJob.id)}`);
  await waitFor(cdp, `document.body.innerText.includes("Not run yet")`, "not-run task detail");
  const detailText = await evaluate(cdp, "document.body.innerText");
  assert(!detailText.includes("In sync"), "never-run task must not claim to be in sync");
  console.log("PASS  New tasks are explicitly marked Not run until their first execution");
  console.log(`      screenshot: ${await screenshot(cdp, "job-not-run.png")}`);

  await api(cdp, `/jobs/${encodeURIComponent(notRunJob.id)}`, { method: "DELETE" });
  created.jobIDs = created.jobIDs.filter((id) => id !== notRunJob.id);
  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("203.0.113.51")`,
    "source inspection after task deletion"
  );
  console.log("PASS  Overview keeps inspecting the selected source after all tasks disappear");

  const broken = await createSource(cdp, `broken-v6-${runID}`, "v6", "exit 7", "command");
  await selectSources(cdp, v4B.id, broken.id);
  await navigate(cdp);
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes(${JSON.stringify(broken.name)})`,
    "failed-source state"
  );
  hero = await heroText(cdp);
  assert(hero.includes("Check failed"), "failed source should have an error state");
  assert(hero.includes("command failed"), "failed source should expose the concrete error");
  const failedLayout = await evaluate(cdp, `(() => {
    const card = document.querySelector(".ip-hero").closest(".card");
    return {
      cardHeight: card.getBoundingClientRect().height,
      addressHeights: [...document.querySelectorAll(".ip-address-slot")].map((node) => node.getBoundingClientRect().height),
      metaHeights: [...document.querySelectorAll(".ip-meta-slot")].map((node) => node.getBoundingClientRect().height)
    };
  })()`);
  assert(
    Math.abs(failedLayout.cardHeight - goodLayout.cardHeight) <= 1,
    `source failure changed the public-IP card height from ${goodLayout.cardHeight} to ${failedLayout.cardHeight}`
  );
  assert(
    failedLayout.metaHeights.every((height, index) => Math.abs(height - goodLayout.metaHeights[index]) <= 1),
    "source failure changed the fixed metadata row height"
  );
  assert(
    failedLayout.addressHeights.every((height, index) => Math.abs(height - goodLayout.addressHeights[index]) <= 1),
    "source failure changed the fixed address row height"
  );
  const errorLine = await evaluate(cdp, `(() => {
    const error = document.querySelector(".ip-meta-error");
    const target = error?.parentElement?.querySelector(".ip-meta-target");
    if (!error) return null;
    const style = getComputedStyle(error);
    const e = error.getBoundingClientRect();
    const t = target?.getBoundingClientRect();
    return {
      title: error.getAttribute("title") || "",
      overflow: style.overflow,
      whiteSpace: style.whiteSpace,
      textOverflow: style.textOverflow,
      sameLine: !target || Math.abs((e.top + e.bottom) / 2 - (t.top + t.bottom) / 2) <= 1
    };
  })()`);
  assert(errorLine?.overflow === "hidden", "error summary is not clipped inside the metadata row");
  assert(errorLine?.whiteSpace === "nowrap", "error summary can wrap onto another line");
  assert(errorLine?.textOverflow === "ellipsis", "error summary does not use an ellipsis");
  assert(errorLine?.sameLine, "target and error summary are not on the same line");
  assert(errorLine?.title.includes("command failed"), "hover title does not expose the full source error");
  console.log("PASS  Overview exposes source failures instead of blank success");
  console.log("PASS  Errors share the target row, truncate safely and expose the full message on hover");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-source-error.png")}`);

  await navigate(cdp, "zh-CN");
  await waitFor(
    cdp,
    `document.querySelector(".ip-hero")?.innerText.includes("检查失败")`,
    "Chinese failure state"
  );
  hero = await heroText(cdp);
  assert(hero.includes("显示来源"), "Chinese source-picker copy is missing");
  assert(hero.includes(broken.name), "Chinese state lost the source name");
  const actionText = await evaluate(cdp, `document.querySelector(".ip-actions")?.innerText || ""`);
  assert(actionText.includes("刷新 IPv4 + IPv6"), "Chinese refresh action is missing");
  assert(actionText.includes("有些主机每天更换 IPv6 前缀。OzDDNS 仅在解析地址变化时写入。"), "Chinese IPv6 hint is missing");
  assert(!actionText.includes("同时检查选中的 IPv4 和 IPv6 来源。"), "redundant Chinese refresh helper is still visible");
  console.log("PASS  Overview source controls and states are translated in Chinese");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-source-error-zh.png")}`);

  await cdp.call("Emulation.setDeviceMetricsOverride", {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true
  });
  await navigate(cdp);
  await waitFor(cdp, `document.querySelector(".ip-hero")`, "mobile Overview");
  assert(
    await evaluate(cdp, `Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) <= window.innerWidth + 1`),
    "Overview overflows horizontally at 390px"
  );
  assert(
    await evaluate(cdp, `(() => {
      const select = document.querySelector("#overview-ipv6-source");
      const arrow = select?.parentElement?.querySelector(".select-arrow");
      if (!select || !arrow) return false;
      const s = select.getBoundingClientRect();
      const a = arrow.getBoundingClientRect();
      return s.left >= 0 && s.right <= innerWidth + 1 && a.right <= s.right - 10;
    })()`),
    "mobile source select or chevron leaves its available width"
  );
  assert(
    await evaluate(cdp, `(() => {
      const actions = document.querySelector(".ip-actions").getBoundingClientRect();
      const button = document.querySelector(".ip-actions > button").getBoundingClientRect();
      const hint = document.querySelector(".ip-action-hint");
      return actions.left >= 0 && actions.right <= innerWidth + 1 &&
        button.left >= actions.left && button.right <= actions.right + 1 &&
        getComputedStyle(hint).display === "none";
    })()`),
    "mobile refresh action leaves the card or keeps the desktop IPv6 hint"
  );
  assert(consoleErrors.length === 0, `browser errors: ${consoleErrors.join("; ")}`);
  console.log("PASS  Overview and native source selects fit a 390px viewport without browser errors");
  console.log(`      screenshot: ${await screenshot(cdp, "overview-mobile.png")}`);

  await evaluate(cdp, `(() => {
    localStorage.removeItem("oz.overview.ipv4SourceId");
    localStorage.removeItem("oz.overview.ipv6SourceId");
  })()`);
} finally {
  try {
    for (const id of created.jobIDs) {
      await api(cdp, `/jobs/${encodeURIComponent(id)}`, { method: "DELETE" });
    }
    for (const id of created.sourceIDs) {
      await api(cdp, `/ip-sources/${encodeURIComponent(id)}`, { method: "DELETE" });
    }
    if (created.providerID) {
      await api(cdp, `/providers/${encodeURIComponent(created.providerID)}`, { method: "DELETE" });
    }
    await api(cdp, "/scheduler", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ running: initialSchedulerRunning })
    });
  } catch {
    // The isolated test harness owns final cleanup if the daemon disappeared.
  }
  cdp.close();
}
