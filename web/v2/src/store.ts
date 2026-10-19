import { computed, reactive, ref } from "vue";
import { useRouter } from "vue-router";
import { applyTheme, setLocale, setTimeZone, t, themePreference } from "./lib";
import type {
  AppData,
  AuthEvent,
  AuthSetupInfo,
  AuthSetupResult,
  AuthState,
  IPFamilyInfo,
  IPFamilyStatus,
  IPInfo,
  IPSource,
  Job,
  JobStatus,
  ModalState,
  Provider,
  Run,
  Toast
} from "./types";

// The panel addresses its own origin's API. BASE_URL is relative in production
// ("./"), so this resolves against the document's <base href> — the mount point
// the daemon was started with — and against "/" in the dev server.
const base = `${import.meta.env.BASE_URL}api/v1`;
let toastSequence = 0;

export const bootState = ref<"loading" | "ready" | "offline">("loading");
export const authState = ref<AuthState>({
  enabled: false,
  authenticated: false,
  setupPending: false,
  recoveryCodesRemaining: 0,
  sessions: 0,
  listen: "",
  exposed: false
});
// offlineReason carries the failure that stopped the first load, and
// staleSince marks the moment a later poll lost the daemon. Both exist so the
// panel can say "I could not reach the API" instead of quietly rendering the
// bundled sample dataset as if it were the user's instance.
export const offlineReason = ref("");
export const staleSince = ref<number | null>(null);
export const navOpen = ref(false);
export const schedulerRunning = ref(true);
export const modal = ref<ModalState | null>(null);
export const toasts = ref<Toast[]>([]);
export const providerFilter = ref("all");
export const runFilter = reactive({ status: "all", job: "all" });

const fallback = structuredClone(window.OZ.data) as AppData;
export const data = reactive<AppData>(fallback);

function toMs(value: unknown): number | null {
  if (!value) return null;
  const parsed = Date.parse(String(value));
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
}

function mapProvider(p: any): Provider {
  return {
    id: p.id,
    name: p.name,
    type: p.type,
    enabled: p.enabled,
    status: p.status,
    statusText: p.statusText,
    zones: p.zones || [],
    records: p.records || 0,
    config: p.config || {},
    lastValidated: toMs(p.lastValidated),
    createdAt: toMs(p.createdAt),
    updatedAt: toMs(p.updatedAt)
  };
}

function mapJob(j: any): Job {
  const state = j.state || {};
  const status: JobStatus = ["success", "nochange", "failed", "skipped", "running"].includes(state.lastStatus)
    ? state.lastStatus
    : "not_run";
  return {
    id: j.id,
    name: j.name,
    providerId: j.providerId,
    ipSourceId: j.ipSourceId,
    targets: (j.targets || []).map((target: any) => ({ ...target, proxied: !!target.proxied, comment: target.comment || "" })),
    intervalMinutes: j.intervalMinutes,
    runOnChangeOnly: j.runOnChangeOnly,
    jitter: j.jitter,
    enabled: j.enabled,
    lastRun: toMs(state.lastRunAt),
    lastStatus: status,
    nextRun: j.enabled ? toMs(state.nextRunAt) : null,
    current: state.current || "",
    desired: state.desired || "",
    lastChanged: toMs(state.lastChangedAt),
    error: state.error || undefined,
    note: status === "skipped" ? state.error || t("No address available from the source yet.") : undefined
  };
}

function mapRun(run: any): Run {
  return {
    id: String(run.id),
    jobId: run.jobId,
    startedAt: toMs(run.startedAt) || Date.now(),
    durationMs: run.durationMs || 0,
    status: run.status,
    oldIp: run.oldIp || null,
    newIp: run.newIp || null,
    trigger: run.trigger,
    message: run.message || "",
    records: run.records || 0,
    details: Array.isArray(run.details) ? run.details : []
  };
}

function mapIPFamily(item: any): IPFamilyInfo {
  const status = ["ok", "error", "no_source"].includes(item?.status)
    ? item.status as IPFamilyStatus
    : "no_source";
  return {
    address: item?.address || "",
    sourceId: item?.sourceId || "",
    source: item?.source || "",
    sourceTarget: item?.sourceTarget || "",
    status,
    error: item?.error || "",
    checkedAt: toMs(item?.checkedAt)
  };
}

function mapIPInfo(payload: any): IPInfo {
  return {
    v4: mapIPFamily(payload?.v4),
    v6: mapIPFamily(payload?.v6),
    checkedAt: toMs(payload?.checkedAt)
  };
}

export function updateIPInfo(payload: any): void {
  data.ipInfo = mapIPInfo(payload);
}

function mapSettings(settings: any): any {
  settings ||= {};
  return {
    general: {
      language: window.OZ.i18n.normalize(settings.language) || "en",
      theme: settings.theme || "light",
      defaultInterval: settings.defaultInterval || 5,
      // Empty means "follow the browser's zone".
      timezone: settings.timezone || "",
      schedulerPaused: !!settings.schedulerPaused,
      schedulerTickSeconds: settings.schedulerTickSeconds || 5,
      schedulerWorkers: settings.schedulerWorkers || 4,
      runOnStart: settings.runOnStart !== false,
      runRetentionDays: settings.runRetentionDays || 1
    },
    notify: {
      webhooks: (settings.notifications || []).map((item: any) => ({
        id: item.id,
        kind: item.kind,
        name: item.name,
        target: item.target,
        enabled: item.enabled,
        on: item.on || []
      }))
    },
    raw: settings
  };
}

function mapActivity(runs: Run[], jobs: Job[]): any[] {
  const jobsById = new Map(jobs.map((job) => [job.id, job]));
  return runs.slice(0, 12).map((run) => {
    const job = jobsById.get(run.jobId);
    const kind = run.status === "success" ? "ok" : run.status === "failed" ? "err" : "info";
    const label = t(({ success: "updated", nochange: "in sync", failed: "failed", skipped: "skipped" } as any)[run.status] || run.status);
    let text = run.message || "";
    if (run.oldIp && run.newIp && run.oldIp !== run.newIp) text = `${run.oldIp} → ${run.newIp}${text ? ` · ${text}` : ""}`;
    return { at: run.startedAt, kind, title: `${job?.name || run.jobId} ${label}`, text };
  });
}

function applyBootstrap(payload: any): void {
  data.providerTypes = payload.providerTypes || [];
  data.ipSourceTypes = payload.ipSourceTypes || [];
  data.ipSources = (payload.ipSources || []).map((item: any) => ({ ...item }));
  data.interfaces = payload.interfaces || [];
  data.providers = (payload.providers || []).map(mapProvider);
  data.jobs = (payload.jobs || []).map(mapJob);
  data.runs = (payload.runs || []).map(mapRun);
  data.ipInfo = mapIPInfo(payload.ip || {});
  const status = payload.status || {};
  data.runtime = {
    scheduler: status.scheduler,
    uptimeSeconds: status.uptimeSeconds,
    version: status.version,
    build: status.build,
    dbPath: status.dbPath,
    dbDriver: status.dbDriver,
    listen: status.listen,
    listenSource: status.listenSource,
    basePath: status.basePath,
    basePathSource: status.basePathSource,
    goVersion: status.goVersion,
    goos: status.goos,
    goarch: status.goarch
  };
  data.scheduler = {
    running: payload.scheduler?.running !== false,
    tickSeconds: payload.scheduler?.tickSeconds || 5,
    workers: payload.scheduler?.workers || 4,
    runOnStart: payload.scheduler?.runOnStart !== false,
    defaultIntervalMinutes: payload.scheduler?.defaultIntervalMinutes || 5
  };
  data.settings = mapSettings(payload.settings);
  data.activity = mapActivity(data.runs, data.jobs);
  schedulerRunning.value = status.scheduler === "running" || data.scheduler.running;
  if (!new URLSearchParams(location.search).has("lang")) setLocale(data.settings.general.language, false);
  if (!new URLSearchParams(location.search).has("theme")) applyTheme(data.settings.general.theme, false);
  setTimeZone(data.settings.general.timezone);
  bootState.value = "ready";
}

async function request(path: string, options: RequestInit = {}): Promise<any> {
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");
  if (options.body) headers.set("Content-Type", "application/json");
  const response = await fetch(`${base}${path}`, { ...options, headers, credentials: "same-origin" });
  const text = await response.text();
  let body: any = null;
  if (text) {
    try { body = JSON.parse(text); } catch { body = text; }
  }
  if (!response.ok) {
    const error = new Error(body?.error || response.statusText || `HTTP ${response.status}`) as Error & { status?: number };
    error.status = response.status;
    throw error;
  }
  return body;
}

export const api = {
  get: (path: string) => request(path),
  download: async (path: string) => {
    const headers = new Headers({ Accept: "application/json" });
    const response = await fetch(`${base}${path}`, { headers, credentials: "same-origin" });
    if (!response.ok) {
      const body = await response.json().catch(() => null);
      const error = new Error(body?.error || response.statusText || `HTTP ${response.status}`) as Error & { status?: number };
      error.status = response.status;
      throw error;
    }
    return response.blob();
  },
  post: (path: string, body?: unknown) => request(path, { method: "POST", body: body == null ? undefined : JSON.stringify(body) }),
  patch: (path: string, body: unknown) => request(path, { method: "PATCH", body: JSON.stringify(body) }),
  del: (path: string) => request(path, { method: "DELETE" })
};

export function notify(kind: Toast["kind"], title: string, desc = "", duration = 4200): void {
  const toast = { id: ++toastSequence, kind, title, desc };
  toasts.value.push(toast);
  window.setTimeout(() => dismissToast(toast.id), duration);
}

export function dismissToast(id: number): void {
  toasts.value = toasts.value.filter((item) => item.id !== id);
}

export function fail(error: unknown, title = t("Something went wrong")): void {
  const err = error as Error & { status?: number };
  if (err?.status === 401) {
    authState.value = { ...authState.value, authenticated: false, username: "", sessions: 0, recoveryCodesRemaining: 0 };
    if (location.hash.startsWith("#/login")) {
      // Already on the sign-in page: it renders its own inline message, so a
      // second "sign-in required" toast would only add noise.
      return;
    }
    location.hash = "#/login";
    notify("err", t("Sign-in required"), t("Enter your credentials to continue."));
    return;
  }
  notify("err", title, err?.message || String(error), 5200);
}

export async function refresh(quiet = false): Promise<void> {
  try {
    applyBootstrap(await api.get("/bootstrap"));
  } catch (error) {
    if (!quiet) fail(error, t("Refresh failed"));
    throw error;
  }
}

export async function boot(): Promise<void> {
  bootState.value = "loading";
  try {
    const state = await loadAuthState();
    if (state.enabled && !state.authenticated) {
      bootState.value = "ready";
      offlineReason.value = "";
      staleSince.value = null;
      location.hash = "#/login";
      return;
    }
    await refresh(true);
    offlineReason.value = "";
    staleSince.value = null;
  } catch (error) {
    const err = error as Error & { status?: number };
    if (err?.status === 401) {
      authState.value = { ...authState.value, authenticated: false, username: "", sessions: 0, recoveryCodesRemaining: 0 };
      bootState.value = "ready";
      offlineReason.value = "";
      location.hash = "#/login";
    } else {
      // The shell loaded but the API did not answer. The panel says so plainly
      // and shows no data at all — the sample dataset bundled for first paint
      // must never be mistaken for a live instance.
      bootState.value = "offline";
      offlineReason.value = err?.message || String(error);
      staleSince.value = null;
      schedulerRunning.value = false;
      setTimeZone(data.settings.general.timezone);
    }
  }
}

// retryBoot re-runs the first load from the offline screen.
export function retryBoot(): void {
  if (bootState.value === "loading") return;
  void boot();
}

let polling = false;

// poll silently re-reads the current state so scheduled runs, background
// refreshes and other tabs show up without a manual reload. It is deliberately
// quiet: a failure keeps the last good data instead of flashing an error, and
// it never runs while the tab is hidden or the sign-in page is showing.
export async function poll(): Promise<void> {
  if (polling || bootState.value !== "ready" || document.hidden) return;
  if (location.hash.startsWith("#/login")) return;
  polling = true;
  try {
    applyBootstrap(await api.get("/bootstrap"));
    staleSince.value = null;
  } catch (error) {
    const err = error as Error & { status?: number };
    if (err?.status === 401) {
      // The session expired or was revoked from another browser.
      authState.value = { ...authState.value, authenticated: false, username: "", sessions: 0, recoveryCodesRemaining: 0 };
      location.hash = "#/login";
      notify("err", t("Sign-in required"), t("Enter your credentials to continue."));
    } else if (staleSince.value === null) {
      // The daemon went away after a good load: keep the last good data (it is
      // still the best information available) but say that it is stale.
      staleSince.value = Date.now();
    }
  } finally {
    polling = false;
  }
}

export function getProvider(id: string): Provider | undefined {
  return data.providers.find((provider) => provider.id === id);
}

export function getProviderType(id: string): any {
  return data.providerTypes.find((provider) => provider.id === id);
}

export function getJob(id: string): Job | undefined {
  return data.jobs.find((job) => job.id === id);
}

export function getSource(id: string): IPSource | undefined {
  return data.ipSources.find((source) => source.id === id);
}

export function getRun(id: string): Run | undefined {
  return data.runs.find((run) => String(run.id) === String(id));
}

export async function toggleScheduler(): Promise<void> {
  const next = !schedulerRunning.value;
  if (bootState.value === "offline") {
    schedulerRunning.value = next;
  } else {
    const result = await api.post("/scheduler", { running: next });
    schedulerRunning.value = !!result.running;
  }
  notify("ok", t(schedulerRunning.value ? "Scheduler resumed" : "Scheduler paused"),
    t(schedulerRunning.value ? "Jobs will run on schedule." : "No automatic runs until resumed."));
}

export async function runJob(job: Job): Promise<void> {
  notify("info", t("Running {name}…", { name: job.name }), t("Reconciling records against the provider."), 2600);
  if (bootState.value === "offline") {
    const run: Run = {
      id: `offline-${Date.now()}`,
      jobId: job.id,
      startedAt: Date.now(),
      durationMs: 620,
      status: job.lastStatus === "failed" ? "failed" : "nochange",
      oldIp: job.current,
      newIp: job.desired,
      trigger: "manual",
      message: t("Manual run completed."),
      records: 0
    };
    data.runs.unshift(run);
    job.lastRun = Date.now();
  } else {
    await api.post(`/jobs/${job.id}/run`);
    await refresh(true);
  }
  const latest = getJob(job.id) || job;
  if (latest.lastStatus === "failed") notify("err", t("{name} failed", { name: job.name }), latest.error || t("Update failed"));
  else if (latest.lastStatus === "success") notify("ok", t("{name} · record updated", { name: job.name }), t("The provider accepted the new address."));
  else if (latest.lastStatus === "skipped") notify("info", t("{name} · skipped", { name: job.name }), latest.note || t("No address available."));
  else notify("ok", t("{name} · in sync", { name: job.name }), t("Records already match the desired address."));
}

export async function runAll(): Promise<void> {
  const count = data.jobs.filter((job) => job.enabled).length;
  if (!count) {
    notify("info", t("Nothing to run"), t("No enabled tasks."));
    return;
  }
  notify("info", t("Reconciling {n} task(s)…", { n: count }), t("Reading records and diffing."), 2600);
  if (bootState.value === "offline") {
    notify("ok", t("All tasks reconciled"), t("{success} updated · {nochange} unchanged · {skipped} skipped · {failed} failed", {
      success: 1, nochange: Math.max(0, count - 2), skipped: 0, failed: 1
    }));
    return;
  }
  const summary = await api.post("/jobs/run-all");
  await refresh(true);
  notify(summary.failed ? "err" : "ok", summary.failed ? t("Some tasks failed") : t("All tasks reconciled"),
    t("{success} updated · {nochange} unchanged · {skipped} skipped · {failed} failed", {
      success: summary.success || 0,
      nochange: summary.noChange || 0,
      skipped: summary.skipped || 0,
      failed: summary.failed || 0
    }));
}

export async function toggleJob(job: Job): Promise<void> {
  if (bootState.value === "offline") job.enabled = !job.enabled;
  else {
    await api.patch(`/jobs/${job.id}`, { enabled: !job.enabled });
    await refresh(true);
  }
  notify(job.enabled ? "ok" : "info", t(job.enabled ? "Enabled {name}" : "Paused {name}", { name: job.name }));
}

export async function validateProvider(provider: Provider): Promise<void> {
  const writeOnly = !!getProviderType(provider.type)?.writeOnly;
  notify("info", t("Validating {name}…", { name: provider.name }),
    t(writeOnly ? "This provider has no read API — there is nothing to verify." : "Checking credentials & zones."), 2000);
  if (bootState.value === "offline") {
    provider.status = provider.type === "godaddy" ? "err" : "ok";
    notify(provider.status === "ok" ? "ok" : "err",
      t(provider.status === "ok" ? (writeOnly ? "{name} — nothing to verify" : "{name} is valid") : "{name} failed validation", { name: provider.name }),
      provider.status === "ok"
        ? t(writeOnly ? "Nothing to verify — credentials are used on the next run." : "Credentials and zones look good.")
        : t("Invalid credentials (401)."));
    return;
  }
  const result = await api.post(`/providers/${provider.id}/validate`);
  notify(result.ok ? "ok" : "err",
    t(result.ok ? (writeOnly ? "{name} — nothing to verify" : "{name} is valid") : "{name} failed validation", { name: provider.name }),
    result.ok
      ? t(writeOnly ? "Nothing to verify — credentials are used on the next run." : "Credentials and zones look good.")
      : result.error || t("The provider rejected the request."));
  await refresh(true);
}

// toggleProvider enables or disables a provider. While disabled, every task
// that writes through it is skipped instead of failing.
export async function toggleProvider(provider: Provider): Promise<void> {
  const next = !provider.enabled;
  if (bootState.value === "offline") provider.enabled = next;
  else {
    const updated = await api.patch(`/providers/${provider.id}`, { enabled: next });
    const index = data.providers.findIndex((item) => item.id === provider.id);
    if (index >= 0) data.providers.splice(index, 1, mapProvider(updated));
    await refresh(true);
  }
  notify(next ? "ok" : "info", t(next ? "Enabled {name}" : "Disabled {name}", { name: provider.name }),
    t(next ? "Tasks that use it will run again." : "Tasks that use it are skipped until it is enabled."));
}

export function confirmModal(title: string, body: string, confirm: string, action: () => void | Promise<void>, danger = true): void {
  modal.value = { type: "confirm", title, body, confirm, danger, action };
}

export async function deleteProvider(provider: Provider): Promise<void> {
  if (bootState.value === "offline") data.providers = data.providers.filter((item) => item.id !== provider.id);
  else {
    await api.del(`/providers/${provider.id}`);
    await refresh(true);
  }
  notify("ok", t("Provider deleted"), provider.name);
  location.hash = "#/providers";
}

export async function deleteJob(job: Job): Promise<void> {
  if (bootState.value === "offline") data.jobs = data.jobs.filter((item) => item.id !== job.id);
  else {
    await api.del(`/jobs/${job.id}`);
    await refresh(true);
  }
  notify("ok", t("Task deleted"), job.name);
  location.hash = "#/jobs";
}

// upsertSource folds a create/update response back into the shared list so the
// UI updates immediately instead of waiting on a full (and slow) bootstrap.
export function upsertSource(source: IPSource): void {
  const index = data.ipSources.findIndex((item) => item.id === source.id);
  if (index >= 0) data.ipSources.splice(index, 1, { ...data.ipSources[index], ...source });
  else data.ipSources.push(source);
}

export async function deleteSource(source: IPSource): Promise<void> {
  if (bootState.value !== "offline") await api.del(`/ip-sources/${source.id}`);
  data.ipSources = data.ipSources.filter((item) => item.id !== source.id);
  notify("ok", t("IP source deleted"), source.name);
}

export async function saveThemePreference(): Promise<void> {
  if (bootState.value !== "offline") {
    await api.patch("/settings", { theme: themePreference.value });
    data.settings.general.theme = themePreference.value;
  }
}

export async function loadAuthState(): Promise<AuthState> {
  const state = await api.get("/auth/state");
  authState.value = {
    enabled: !!state.enabled,
    authenticated: !!state.authenticated,
    setupPending: !!state.setupPending,
    username: state.username || "",
    recoveryCodesRemaining: state.recoveryCodesRemaining || 0,
    sessions: state.sessions || 0,
    listen: state.listen || "",
    exposed: !!state.exposed
  };
  return authState.value;
}

export async function login(username: string, password: string): Promise<string> {
  const result = await api.post("/auth/login", { username, password });
  if (!result?.challenge) throw new Error(t("The server did not start a sign-in challenge."));
  return String(result.challenge);
}

export async function verifyMfa(challenge: string, code: string): Promise<void> {
  await api.post("/auth/mfa", { challenge, code });
  await loadAuthState();
  await refresh(true);
  bootState.value = "ready";
  staleSince.value = null;
}

export async function logout(): Promise<void> {
  await api.post("/auth/logout");
  authState.value = { ...authState.value, authenticated: false, username: "", sessions: 0, recoveryCodesRemaining: 0 };
  bootState.value = "ready";
  location.hash = "#/login";
}

export async function signOut(): Promise<void> {
  try {
    await logout();
  } catch (error) {
    fail(error, t("Could not sign out"));
  }
}

export async function startAuthSetup(username: string, password: string): Promise<AuthSetupInfo> {
  const info = await api.post("/auth/setup/start", { username, password });
  authState.value = { ...authState.value, enabled: false, authenticated: false, setupPending: true };
  return {
    secret: String(info.secret || ""),
    otpauthUrl: String(info.otpauthUrl || ""),
    qrCodeDataUrl: String(info.qrCodeDataUrl || ""),
    expiresAt: String(info.expiresAt || "")
  };
}

export async function verifyAuthSetup(code: string): Promise<AuthSetupResult> {
  const result = await api.post("/auth/setup/verify", { code });
  await loadAuthState();
  await refresh(true);
  bootState.value = "ready";
  return {
    recoveryCodes: (result.recoveryCodes || []).map(String),
    user: result.user || { username: authState.value.username || "" }
  };
}

export async function cancelAuthSetup(): Promise<void> {
  await api.post("/auth/setup/cancel");
  authState.value = { ...authState.value, setupPending: false };
}

export async function disableAuth(password: string, code: string): Promise<void> {
  await api.post("/auth/disable", { password, code });
  authState.value = {
    enabled: false,
    authenticated: false,
    setupPending: false,
    recoveryCodesRemaining: 0,
    sessions: 0,
    listen: authState.value.listen,
    exposed: authState.value.exposed
  };
  await refresh(true);
  bootState.value = "ready";
}

export async function regenerateRecoveryCodes(password: string, code: string): Promise<string[]> {
  const result = await api.post("/auth/recovery-codes", { password, code });
  await loadAuthState();
  return (result.recoveryCodes || []).map(String);
}

export async function loadAuthEvents(): Promise<AuthEvent[]> {
  if (!authState.value.enabled || !authState.value.authenticated) return [];
  const events = await api.get("/auth/events");
  return Array.isArray(events) ? events : [];
}

export const failingJobs = computed(() => data.jobs.filter((job) => job.lastStatus === "failed"));
export const enabledJobs = computed(() => data.jobs.filter((job) => job.enabled));

export function useNavigation() {
  const router = useRouter();
  return {
    go: (path: string) => router.push(path),
    closeNav: () => { navOpen.value = false; }
  };
}
