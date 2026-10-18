#!/usr/bin/env node
// Browser-level authorization regression for an isolated OzDDNS instance.
//
// Prerequisites:
//   - OzDDNS is running at OZDDNS_E2E_URL with a fresh database.
//   - Headless Chromium is running with a CDP endpoint at OZDDNS_CDP_URL.
//
// This script intentionally drives the real built panel and API. It does not
// read or modify the user's normal browser session.

import crypto from "node:crypto";
import fs from "node:fs/promises";

const baseURL = (process.env.OZDDNS_E2E_URL || "http://127.0.0.1:9876").replace(/\/$/, "");
const cdpURL = (process.env.OZDDNS_CDP_URL || "http://127.0.0.1:9222").replace(/\/$/, "");
const expectedListen = process.env.OZDDNS_E2E_EXPECTED_LISTEN || new URL(baseURL).host;
const password = process.env.OZDDNS_E2E_PASSWORD || "correct horse battery";
const username = process.env.OZDDNS_E2E_USERNAME || "admin";
const screenshotDir = process.env.OZDDNS_E2E_SCREENSHOT_DIR || "/tmp";
const results = [];
const browserErrors = [];

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

function record(name, detail = "") {
  results.push({ name, detail });
  console.log(`PASS  ${name}${detail ? ` — ${detail}` : ""}`);
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
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id);
        if (message.error) pending.reject(new Error(`${pending.method}: ${message.error.message}`));
        else pending.resolve(message.result);
        return;
      }
      if (message.method === "Runtime.exceptionThrown") {
        browserErrors.push(`exception: ${message.params.exceptionDetails.text}`);
      } else if (message.method === "Runtime.consoleAPICalled" && message.params.type === "error") {
        browserErrors.push(`console: ${message.params.args.map((arg) => arg.value || arg.description || "").join(" ")}`);
      } else if (message.method === "Log.entryAdded" && message.params.entry.level === "error") {
        browserErrors.push(`log: ${message.params.entry.text}`);
      }
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

async function waitFor(cdp, expression, description, timeout = 12_000) {
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

async function navigate(cdp, hash, readyExpression = "document.readyState === 'complete'") {
  // A hash-only navigation does not reload the document. Give every deliberate
  // navigation a unique query so the browser always evaluates the current
  // embedded build instead of whatever page happened to be open before.
  const fragment = hash.startsWith("#") ? hash : `#${hash}`;
  await cdp.call("Page.navigate", { url: `${baseURL}/?e2e=${Date.now()}${fragment}` });
  await waitFor(cdp, readyExpression, `navigation to ${hash}`);
  await sleep(250);
}

async function fill(cdp, selector, value, index = 0) {
  const ok = await evaluate(cdp, `(() => {
    const el = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
    if (!el) return false;
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
    setter.call(el, ${JSON.stringify(value)});
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  })()`);
  assert(ok, `input not found: ${selector}[${index}]`);
}

async function click(cdp, selector, index = 0) {
  const ok = await evaluate(cdp, `(() => {
    const el = document.querySelectorAll(${JSON.stringify(selector)})[${index}];
    if (!el) return false;
    el.click();
    return true;
  })()`);
  assert(ok, `click target not found: ${selector}[${index}]`);
}

async function text(cdp, selector) {
  return evaluate(cdp, `document.querySelector(${JSON.stringify(selector)})?.textContent?.trim() || ""`);
}

async function authState(cdp) {
  return evaluate(cdp, `fetch("/api/v1/auth/state", { credentials: "same-origin" }).then(async (r) => await r.json())`, true);
}

async function cookies(cdp) {
  const result = await cdp.call("Network.getAllCookies");
  return result.cookies.filter((cookie) => cookie.name === "ozddns_session");
}

async function sessionValue(cdp) {
  return (await cookies(cdp)).find((cookie) => cookie.name === "ozddns_session")?.value || "";
}

async function setOffline(cdp, offline) {
  await cdp.call("Network.emulateNetworkConditions", {
    offline,
    latency: 0,
    downloadThroughput: offline ? 0 : -1,
    uploadThroughput: offline ? 0 : -1,
    connectionType: offline ? "none" : "wifi"
  });
}

function decodeBase32(input) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const clean = input.toUpperCase().replace(/=+$/, "").replace(/\s/g, "");
  let bits = 0;
  let value = 0;
  const out = [];
  for (const character of clean) {
    const index = alphabet.indexOf(character);
    if (index < 0) throw new Error(`invalid base32 character: ${character}`);
    value = (value << 5) | index;
    bits += 5;
    if (bits >= 8) {
      bits -= 8;
      out.push((value >>> bits) & 0xff);
    }
  }
  return Buffer.from(out);
}

function totp(secret, at = Date.now()) {
  const counter = Math.floor(at / 1000 / 30);
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(BigInt(counter));
  const digest = crypto.createHmac("sha1", decodeBase32(secret)).update(message).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const value = ((digest[offset] & 0x7f) << 24)
    | (digest[offset + 1] << 16)
    | (digest[offset + 2] << 8)
    | digest[offset + 3];
  return String(value % 1_000_000).padStart(6, "0");
}

async function screenshot(cdp, name) {
  const result = await cdp.call("Page.captureScreenshot", { format: "png", fromSurface: true });
  const path = `${screenshotDir}/${name}`;
  await fs.writeFile(path, Buffer.from(result.data, "base64"));
  return path;
}

async function loginPassword(cdp) {
  await waitFor(cdp, `location.hash === "#/login" && !!document.querySelector('input[autocomplete="current-password"]')`, "password sign-in form");
  await fill(cdp, 'input[autocomplete="username"]', username);
  await fill(cdp, 'input[autocomplete="current-password"]', password);
  await click(cdp, "form button.btn.primary.block.lg");
  await waitFor(cdp, `!!document.querySelector('input[autocomplete="one-time-code"]')`, "authentication-code form");
}

async function submitSecondFactor(cdp, code) {
  await fill(cdp, 'input[autocomplete="one-time-code"]', code);
  await click(cdp, "form button.btn.primary.block.lg");
}

async function signOut(cdp) {
  await click(cdp, "aside.sidebar button.btn.sm.block.mt8");
  await waitFor(cdp, `location.hash === "#/login"`, "sign-out navigation", 15_000);
}

async function main() {
  const pageList = await fetch(`${cdpURL}/json/list`).then((response) => response.json());
  const page = pageList.find((item) => item.type === "page");
  assert(page?.webSocketDebuggerUrl, "no Chromium page available on the CDP endpoint");

  const cdp = new CDP(page.webSocketDebuggerUrl);
  await cdp.connect();
  await Promise.all([
    cdp.call("Page.enable"),
    cdp.call("Runtime.enable"),
    cdp.call("Network.enable"),
    cdp.call("Log.enable")
  ]);

  try {
    await setOffline(cdp, false);
    await cdp.call("Network.clearBrowserCookies");
    await navigate(cdp, "#/overview", `location.hash === "#/overview" && document.body.innerText.length > 0`);
    await evaluate(cdp, "localStorage.clear()");
    await navigate(cdp, "#/overview", `location.hash === "#/overview" && !!document.querySelector("main")`);

    let state = await authState(cdp);
    assert(state.enabled === false && state.authenticated === false, "fresh instance did not start in open mode");
    assert(state.listen === expectedListen && state.exposed === false, `unexpected listen state: ${JSON.stringify(state)}`);
    record("default loopback open mode", `${state.listen}, no credentials accepted`);

    const health = await evaluate(cdp, `fetch("/healthz", { credentials: "omit" }).then((r) => r.status)`, true);
    assert(health === 200, `health endpoint returned ${health}`);
    record("unauthenticated health probe");

    await navigate(cdp, "#/settings/security", `document.body.innerText.includes("Authorization")`);
    await fill(cdp, 'input[autocomplete="username"]', username);
    await fill(cdp, 'input[autocomplete="new-password"]', password, 0);
    await fill(cdp, 'input[autocomplete="new-password"]', password, 1);
    await click(cdp, "button.btn.primary.mt8");
    await waitFor(cdp, `!!document.querySelector(".manual-secret")`, "TOTP enrollment details");
    const firstSecret = await text(cdp, ".manual-secret");
    assert(/^[A-Z2-7]{32}$/.test(firstSecret), `unexpected TOTP secret: ${firstSecret}`);
    const qrReady = await evaluate(cdp, `(() => {
      const image = document.querySelector(".totp-qr img");
      return !!image && image.src.startsWith("data:image/png;base64,") && image.complete && image.naturalWidth > 0;
    })()`);
    assert(qrReady, "locally generated TOTP QR image did not render");
    record("TOTP enrollment material", "manual secret + locally generated PNG QR");

    await cdp.call("Emulation.setDeviceMetricsOverride", {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true
    });
    await sleep(300);
    const mobileOverflow = await evaluate(cdp, "document.documentElement.scrollWidth - window.innerWidth");
    assert(mobileOverflow <= 1, `mobile enrollment overflowed by ${mobileOverflow}px`);
    const mobileShot = await screenshot(cdp, "ozddns-auth-e2e-mobile-qr.png");
    record("mobile enrollment layout", mobileShot);
    await cdp.call("Emulation.clearDeviceMetricsOverride");

    await fill(cdp, 'input[placeholder="123456"]', totp(firstSecret));
    await click(cdp, "button.btn.primary");
    await waitFor(cdp, "document.querySelectorAll('.recovery-grid code').length === 10", "recovery-code list", 20_000);
    const firstCodes = await evaluate(cdp, "[...document.querySelectorAll('.recovery-grid code')].map((el) => el.textContent.trim())");
    assert(new Set(firstCodes).size === 10, "recovery codes are not unique");
    const firstSession = await sessionValue(cdp);
    assert(firstSession, "enrollment did not issue a server-side session cookie");
    state = await authState(cdp);
    assert(state.enabled && state.authenticated && state.sessions === 1, `unexpected post-enrollment state: ${JSON.stringify(state)}`);
    record("TOTP enrollment and session", `${firstCodes.length} one-time recovery codes issued`);

    await click(cdp, ".recovery-grid");
    await click(cdp, "div.card.pad button.btn.primary");
    await waitFor(cdp, `document.body.innerText.includes("Authorization enabled")`, "security dashboard");

    const sessionReloadURL = `${baseURL}/?e2e=${Date.now()}#/settings/security`;
    await cdp.call("Page.navigate", { url: sessionReloadURL });
    await waitFor(cdp, `document.body.innerText.includes("Active sessions")`, "authenticated reload");
    state = await authState(cdp);
    assert(state.authenticated, "session did not survive a full page reload");
    record("session survives reload");

    await setOffline(cdp, true);
    await click(cdp, "aside.sidebar button.btn.sm.block.mt8");
    await sleep(100);
    const modalDuringOfflineSignOut = await evaluate(cdp, "!!document.querySelector('.modal')");
    assert(!modalDuringOfflineSignOut, "sign-out opened a confirmation modal");
    await waitFor(cdp, `document.body.innerText.includes("Could not sign out")`, "offline sign-out error", 10_000);
    assert((await sessionValue(cdp)) === firstSession, "offline sign-out discarded the session cookie");
    assert((await evaluate(cdp, "location.hash")) === "#/settings/security", "offline sign-out navigated away");
    record("offline sign-out fails safely", "error shown, session retained");

    await setOffline(cdp, false);
    await signOut(cdp);
    assert(!(await sessionValue(cdp)), "online sign-out did not clear the session cookie");
    state = await authState(cdp);
    assert(state.enabled && !state.authenticated, `unexpected post-logout state: ${JSON.stringify(state)}`);
    const unauthenticated = await evaluate(cdp, `fetch("/api/v1/bootstrap", { credentials: "omit" }).then((r) => ({ status: r.status, challenge: r.headers.get("www-authenticate") }))`, true);
    assert(unauthenticated.status === 401 && !unauthenticated.challenge, `unexpected unauthenticated response: ${JSON.stringify(unauthenticated)}`);
    record("online sign-out and API gate", "cookie cleared, 401 without Basic challenge");

    await navigate(cdp, "#/overview", `location.hash === "#/login"`);
    record("protected route redirects to sign-in");

    await loginPassword(cdp);
    await submitSecondFactor(cdp, firstCodes[0]);
    await waitFor(cdp, `location.hash === "#/overview"`, "recovery-code sign-in");
    record("recovery-code sign-in");

    await signOut(cdp);
    await loginPassword(cdp);
    await submitSecondFactor(cdp, firstCodes[0]);
    await waitFor(cdp, `document.body.innerText.includes("Invalid authentication code")`, "recovery-code reuse rejection");
    assert((await evaluate(cdp, "location.hash")) === "#/login", "rejected recovery code navigated away");
    record("recovery-code replay rejected");

    await submitSecondFactor(cdp, firstCodes[1]);
    await waitFor(cdp, `location.hash === "#/overview"`, "second recovery-code sign-in");

    await signOut(cdp);
    await loginPassword(cdp);
    // Enrollment consumed the current TOTP step. Wait for the next 30-second
    // step so this exercises a fresh sign-in, then reuse it immediately to
    // prove replay protection.
    await sleep(30_000 - (Date.now() % 30_000) + 150);
    const currentCode = totp(firstSecret);
    await submitSecondFactor(cdp, currentCode);
    await waitFor(cdp, `location.hash === "#/overview"`, "TOTP sign-in");
    await signOut(cdp);
    await loginPassword(cdp);
    await submitSecondFactor(cdp, currentCode);
    await waitFor(cdp, `document.body.innerText.includes("Invalid authentication code")`, "TOTP replay rejection");
    record("TOTP replay rejected");

    await submitSecondFactor(cdp, firstCodes[2]);
    await waitFor(cdp, `location.hash === "#/overview"`, "recovery sign-in after replay test");
    await navigate(cdp, "#/settings/security", `document.body.innerText.includes("Active sessions")`);

    // The enabled security page has two credential cards. Index 1 is the
    // "Turn off authorization" card; index 0 rotates recovery codes.
    await fill(cdp, 'input[autocomplete="current-password"]', password, 1);
    await fill(cdp, 'input[autocomplete="one-time-code"]', firstCodes[3], 1);
    await click(cdp, "button.btn.danger");
    await waitFor(cdp, "!!document.querySelector('.sheet-foot button.danger')", "disable confirmation");
    await click(cdp, ".sheet-foot button.danger");
    await waitFor(cdp, `document.body.innerText.includes("Open — no sign-in")`, "open mode after disable", 20_000);
    state = await authState(cdp);
    assert(!state.enabled && !state.authenticated && !(await sessionValue(cdp)), `unexpected disabled state: ${JSON.stringify(state)}`);
    record("authorization disable", "account, sessions, TOTP and recovery codes removed");

    await navigate(cdp, "#/settings/security", `document.body.innerText.includes("Default local-first mode")`);
    await fill(cdp, 'input[autocomplete="username"]', username);
    await fill(cdp, 'input[autocomplete="new-password"]', password, 0);
    await fill(cdp, 'input[autocomplete="new-password"]', password, 1);
    await click(cdp, "button.btn.primary.mt8");
    await waitFor(cdp, `!!document.querySelector(".manual-secret")`, "second TOTP enrollment");
    const secondSecret = await text(cdp, ".manual-secret");
    assert(secondSecret !== firstSecret, "re-enabling authorization reused the old TOTP secret");
    await fill(cdp, 'input[placeholder="123456"]', totp(secondSecret));
    await click(cdp, "button.btn.primary");
    await waitFor(cdp, "document.querySelectorAll('.recovery-grid code').length === 10", "new recovery codes");
    const secondCodes = await evaluate(cdp, "[...document.querySelectorAll('.recovery-grid code')].map((el) => el.textContent.trim())");
    assert(secondCodes.every((code) => !firstCodes.includes(code)), "recovery codes were reused after re-enrollment");
    record("re-enable creates a fresh enrollment", "new TOTP secret and new recovery codes");

    const oldSessionResponse = await fetch(`${baseURL}/api/v1/bootstrap`, {
      headers: { Cookie: `ozddns_session=${firstSession}` }
    });
    assert(oldSessionResponse.status === 401, `old session survived re-enrollment with status ${oldSessionResponse.status}`);
    record("old session invalidated after re-enrollment");

    await click(cdp, "div.card.pad button.btn.primary");
    await waitFor(cdp, `document.body.innerText.includes("Authorization enabled")`, "re-enabled security dashboard");

    for (const route of ["#/overview", "#/providers", "#/jobs", "#/executions", "#/settings/general"]) {
      await navigate(cdp, route, `location.hash === "${route}" && !!document.querySelector("main")`);
      const overflow = await evaluate(cdp, "document.documentElement.scrollWidth - window.innerWidth");
      assert(overflow <= 1, `${route} overflowed horizontally by ${overflow}px`);
    }
    await navigate(cdp, "#/does-not-exist", `location.hash === "#/does-not-exist" && !!document.querySelector("main")`);
    record("primary routes and not-found route render");

    await cdp.call("Emulation.setDeviceMetricsOverride", {
      width: 390,
      height: 844,
      deviceScaleFactor: 2,
      mobile: true
    });
    for (const route of ["#/overview", "#/settings/security"]) {
      await navigate(cdp, route, `location.hash === "${route}" && !!document.querySelector("main")`);
      const overflow = await evaluate(cdp, "document.documentElement.scrollWidth - window.innerWidth");
      assert(overflow <= 1, `${route} mobile layout overflowed by ${overflow}px`);
    }
    const mobileSecurityShot = await screenshot(cdp, "ozddns-auth-e2e-mobile-security.png");
    record("mobile protected routes", mobileSecurityShot);
    await cdp.call("Emulation.clearDeviceMetricsOverride");

    await signOut(cdp);
    await loginPassword(cdp);
    await submitSecondFactor(cdp, secondCodes[0]);
    await waitFor(cdp, `location.hash === "#/overview"`, "final sign-in");
    record("final sign-in after full lifecycle");

    const unexpectedBrowserErrors = browserErrors.filter((error) =>
      !error.includes("ERR_INTERNET_DISCONNECTED")
      && !error.includes("ERR_CONNECTION_REFUSED")
      && !error.includes("TypeError: Failed to fetch")
      && !error.includes("status of 401 (Unauthorized)")
    );
    assert(unexpectedBrowserErrors.length === 0, `browser errors: ${unexpectedBrowserErrors.join(" | ")}`);
    record("browser console clean", "no uncaught exceptions or console errors");

    console.log(JSON.stringify({
      ok: true,
      firstSecretLength: firstSecret.length,
      secondSecretLength: secondSecret.length,
      recoveryCodesPerSet: firstCodes.length,
      checks: results.length,
      screenshots: [mobileShot, mobileSecurityShot]
    }, null, 2));
  } finally {
    cdp.close();
  }
}

main().catch((error) => {
  console.error(`FAIL  ${error.stack || error.message}`);
  if (browserErrors.length) console.error(`Browser errors: ${browserErrors.join(" | ")}`);
  process.exitCode = 1;
});
