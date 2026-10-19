<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, EmptyState, Pill, Toggle } from "../components/UiKit";
import { applyTheme, setLocale, t } from "../lib";
import {
  api,
  authState,
  bootState,
  cancelAuthSetup,
  confirmModal,
  data,
  deleteSource,
  disableAuth,
  fail,
  loadAuthEvents,
  loadAuthState,
  modal,
  notify,
  regenerateRecoveryCodes,
  refresh,
  schedulerRunning,
  signOut,
  startAuthSetup,
  verifyAuthSetup,
  toggleScheduler
} from "../store";
import type { AuthEvent, AuthSetupInfo } from "../types";

const route = useRoute();
const router = useRouter();
const tab = computed(() => String(route.params.tab || "general"));
const tabs = [["general", "General"], ["scheduler", "Scheduler"], ["security", "Security"], ["sources", "IP sources"], ["notify", "Notifications"], ["storage", "Storage"], ["about", "About"]];
const form = reactive<Record<string, any>>({});
const languageCodes = window.OZ.i18n.codes;
const languageLabel = (code: string) => window.OZ.i18n.label(code);
// "" follows the browser's zone. Fixed UTC is offered as "Etc/UTC" — the panel
// reserves the plain "UTC" string as the legacy "never chosen" default, which
// the server rewrites to "".
const zones = (((Intl as any).supportedValuesOf?.("timeZone") as string[] | undefined) || [
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Europe/London",
  "Europe/Paris",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Australia/Sydney"
]);
const timezones = computed(() => [{ value: "", label: t("Local (browser)") }, { value: "Etc/UTC", label: "UTC" }, ...zones.map((zone) => ({ value: zone, label: zone }))]);
const importInput = ref<HTMLInputElement | null>(null);
const tabsElement = ref<HTMLElement | null>(null);
const testingId = ref("");
const authBusy = ref(false);
const authError = ref("");
const setupStep = ref<"idle" | "code" | "codes">("idle");
const setupInfo = ref<AuthSetupInfo | null>(null);
const setupForm = reactive({ username: "admin", password: "", password2: "", code: "" });
const recoveryCodes = ref<string[]>([]);
const disableForm = reactive({ password: "", code: "" });
const regenForm = reactive({ password: "", code: "" });
const authEvents = ref<AuthEvent[]>([]);

// Where a deployment setting came from: start-up flag, environment variable,
// config.yaml, or the built-in default. Shown so neither the bind address nor
// the public path is ever a mystery.
function sourceLabel(source: string): string {
  const labels: Record<string, string> = {
    flag: t("start-up flag"),
    env: t("environment variable"),
    config: t("config.yaml"),
    default: t("built-in default")
  };
  return labels[source] || t("built-in default");
}
const listenSourceLabel = computed(() => sourceLabel(data.runtime.listenSource));
const basePathSourceLabel = computed(() => sourceLabel(data.runtime.basePathSource));

watch(tab, async (value) => {
  if (value === "security") await refreshSecurity();
  await nextTick();
  const button = tabsElement.value?.querySelector<HTMLElement>(`[data-tab="${value}"]`);
  if (!button || !tabsElement.value) return;
  tabsElement.value.scrollTo({
    left: button.offsetLeft - (tabsElement.value.clientWidth - button.offsetWidth) / 2
  });
}, { immediate: true });

// Re-seed the form only when the *stored* settings actually changed. A
// background refresh rebuilds data.settings on every poll, and re-seeding then
// would silently throw away whatever the operator is still typing.
watch(() => JSON.stringify(data.settings), () => {
  const settings = data.settings;
  Object.assign(form, {
    language: settings.general.language,
    theme: settings.general.theme,
    timezone: settings.general.timezone,
    defaultInterval: settings.general.defaultInterval,
    retention: settings.general.runRetentionDays,
    tick: settings.general.schedulerTickSeconds,
    workers: settings.general.schedulerWorkers,
    runOnStart: settings.general.runOnStart
  });
}, { immediate: true });

async function saveSettings(): Promise<void> {
  const payload: any = {
    language: form.language,
    theme: form.theme,
    timezone: form.timezone,
    defaultInterval: Number(form.defaultInterval),
    schedulerTickSeconds: Number(form.tick),
    schedulerWorkers: Number(form.workers),
    runRetentionDays: Number(form.retention),
    runOnStart: !!form.runOnStart,
    notifications: data.settings.notify.webhooks
  };
  try {
    if (bootState.value === "offline") {
      data.settings.general = { ...data.settings.general, ...payload };
    } else {
      await api.patch("/settings", payload);
      await refresh(true);
    }
    if (form.language) setLocale(form.language, true);
    if (form.theme) applyTheme(form.theme, true);
    notify("ok", t("Saved"), t("Settings persisted."));
  } catch (error) { fail(error, t("Save failed")); }
}

async function refreshSecurity(): Promise<void> {
  if (bootState.value === "offline") return;
  try {
    await loadAuthState();
    authEvents.value = await loadAuthEvents();
  } catch (error) {
    fail(error, t("Could not load security state"));
  }
}

async function beginSetup(): Promise<void> {
  authError.value = "";
  if (!setupForm.username.trim()) {
    authError.value = t("Enter an administrator username.");
    return;
  }
  if (setupForm.password.length < 10) {
    authError.value = t("Use a password with at least 10 characters.");
    return;
  }
  if (setupForm.password !== setupForm.password2) {
    authError.value = t("Passwords do not match.");
    return;
  }
  authBusy.value = true;
  try {
    setupInfo.value = await startAuthSetup(setupForm.username.trim(), setupForm.password);
    setupForm.code = "";
    setupStep.value = "code";
  } catch (error) {
    authError.value = (error as Error).message;
  } finally {
    authBusy.value = false;
  }
}

async function completeSetup(): Promise<void> {
  authError.value = "";
  if (!setupForm.code.trim()) {
    authError.value = t("Enter the current code from your authenticator.");
    return;
  }
  authBusy.value = true;
  try {
    const result = await verifyAuthSetup(setupForm.code.trim());
    recoveryCodes.value = result.recoveryCodes;
    setupInfo.value = null;
    setupStep.value = "codes";
    setupForm.password = "";
    setupForm.password2 = "";
    setupForm.code = "";
    authEvents.value = await loadAuthEvents();
    notify("ok", t("Authorization enabled"), t("Store the recovery codes before continuing."));
  } catch (error) {
    authError.value = (error as Error).message;
  } finally {
    authBusy.value = false;
  }
}

async function discardSetup(): Promise<void> {
  authBusy.value = true;
  try {
    await cancelAuthSetup();
  } catch (error) {
    authError.value = (error as Error).message;
  } finally {
    setupInfo.value = null;
    setupStep.value = "idle";
    setupForm.code = "";
    authBusy.value = false;
  }
}

function acknowledgeCodes(): void {
  recoveryCodes.value = [];
  setupInfo.value = null;
  setupStep.value = "idle";
  notify("ok", t("Authorization is ready"), t("Sign in with your password and authenticator code."));
}

async function copyRecoveryCodes(): Promise<void> {
  await copy(recoveryCodes.value.join("\n"));
}

function downloadRecoveryCodes(): void {
  const blob = new Blob([
    `${t("OzDDNS recovery codes")}\n`,
    `${t("Generated")}: ${new Date().toISOString()}\n`,
    `${t("Each code can be used once.")}\n\n`,
    recoveryCodes.value.join("\n"),
    "\n"
  ], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `ozddns-recovery-codes-${new Date().toISOString().slice(0, 10)}.txt`;
  link.click();
  URL.revokeObjectURL(url);
}

function disableAuthorization(): void {
  authError.value = "";
  if (!disableForm.password || !disableForm.code.trim()) {
    authError.value = t("Enter both the password and a current authenticator or recovery code.");
    return;
  }
  confirmModal(
    t("Turn off authorization?"),
    t("This deletes the administrator, every active session, the TOTP secret and all recovery codes. The panel becomes open to anyone who can reach it, and enabling it again requires a new TOTP enrollment."),
    t("Turn off authorization"),
    async () => {
      authBusy.value = true;
      try {
        await disableAuth(disableForm.password, disableForm.code.trim());
        disableForm.password = "";
        disableForm.code = "";
        authEvents.value = [];
        setupStep.value = "idle";
        recoveryCodes.value = [];
        notify("ok", t("Authorization disabled"), t("The panel is open again. Re-enabling it will generate a new TOTP secret."));
      } catch (error) {
        authError.value = (error as Error).message;
        fail(error, t("Could not disable authorization"));
      } finally {
        authBusy.value = false;
      }
    }
  );
}

async function rotateRecoveryCodes(): Promise<void> {
  authError.value = "";
  if (!regenForm.password || !regenForm.code.trim()) {
    authError.value = t("Enter both the password and a current authenticator or recovery code.");
    return;
  }
  authBusy.value = true;
  try {
    recoveryCodes.value = await regenerateRecoveryCodes(regenForm.password, regenForm.code.trim());
    regenForm.password = "";
    regenForm.code = "";
    setupStep.value = "codes";
    authEvents.value = await loadAuthEvents();
    notify("ok", t("Recovery codes replaced"), t("The previous recovery codes no longer work."));
  } catch (error) {
    authError.value = (error as Error).message;
  } finally {
    authBusy.value = false;
  }
}

// How many tasks reference a source. Rendered in its own column on wide
// screens and folded under the source name on phones.
function usedByCount(source: any): number {
  return data.jobs.filter((job) => job.ipSourceId === source.id).length;
}

async function testSource(source: any): Promise<void> {
  if (testingId.value) return;
  testingId.value = source.id;
  try {
    if (bootState.value === "offline") {
      notify("ok", t("Resolved {ip}", { ip: source.family === "v6" ? "2001:db8::23" : "203.0.113.42" }), t("This is the address the task would use."));
    } else {
      const result = await api.post(`/ip-sources/${source.id}/test`);
      notify(result.ok ? "ok" : "err", result.ok ? t("Resolved {ip}", { ip: result.ip }) : t("Could not resolve an address"), result.ok ? t("This is the address the task would use.") : result.error);
    }
  } catch (error) { fail(error, t("Resolve failed")); }
  finally { testingId.value = ""; }
}

function removeSource(source: any): void {
  confirmModal(t("Delete IP source?"), t('"{name}" will be removed. Tasks that reference it must be reassigned.', { name: source.name }), t("Delete source"), () => deleteSource(source));
}

async function testWebhook(id: string): Promise<void> {
  try {
    if (bootState.value === "offline") notify("ok", t("Test delivered"), t("Endpoint responded 200 OK."));
    else {
      const result = await api.post("/settings/test-notify", { id });
      notify(result.ok ? "ok" : "err", result.ok ? t("Test delivered") : t("Delivery failed"), result.ok ? t("The endpoint responded successfully.") : result.error);
    }
  } catch (error) { fail(error, t("Delivery failed")); }
}

async function saveNotifications(notifications: any[]): Promise<void> {
  if (bootState.value === "offline") data.settings.notify.webhooks = notifications;
  else {
    await api.patch("/settings", { notifications });
    await refresh(true);
  }
}

async function toggleNotification(webhook: any): Promise<void> {
  try {
    await saveNotifications(data.settings.notify.webhooks.map((item: any) => (
      item.id === webhook.id ? { ...item, enabled: !item.enabled } : item
    )));
  } catch (error) { fail(error, t("Save failed")); }
}

function removeNotification(webhook: any): void {
  confirmModal(
    t("Delete notification channel?"),
    t('"{name}" will stop receiving OzDDNS events.', { name: webhook.name }),
    t("Delete channel"),
    async () => {
      await saveNotifications(data.settings.notify.webhooks.filter((item: any) => item.id !== webhook.id));
      notify("ok", t("Channel deleted"), webhook.name);
    }
  );
}

async function exportConfig(): Promise<void> {
  try {
    const blob = bootState.value === "offline"
      ? new Blob([JSON.stringify({ providers: data.providers, ipSources: data.ipSources, jobs: data.jobs, settings: data.settings.raw }, null, 2)], { type: "application/json" })
      : await api.download("/config/export");
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `ozddns-config-${new Date().toISOString().slice(0, 10)}.json`;
    link.click();
    URL.revokeObjectURL(url);
    notify("ok", t("Configuration exported"), t("Store the file securely because it contains provider credentials."));
  } catch (error) { fail(error, t("Export failed")); }
}

async function importConfig(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file) return;
  try {
    const bundle = JSON.parse(await file.text());
    if (!Array.isArray(bundle.providers) || !Array.isArray(bundle.ipSources) || !Array.isArray(bundle.jobs) || !bundle.settings) {
      throw new Error(t("The selected file is not an OzDDNS configuration backup."));
    }
    confirmModal(
      t("Replace current configuration?"),
      t("Providers, IP sources, tasks and settings will be replaced. Execution history for the old topology will be deleted."),
      t("Import configuration"),
      async () => {
        if (bootState.value === "offline") {
          data.providers = bundle.providers;
          data.ipSources = bundle.ipSources;
          data.jobs = bundle.jobs;
        } else {
          const result = await api.post("/config/import", bundle);
          await refresh(true);
          notify("ok", t("Configuration imported"), t("{providers} providers, {sources} sources and {jobs} tasks restored.", {
            providers: result.providers, sources: result.ipSources, jobs: result.jobs
          }));
        }
      }
    );
  } catch (error) { fail(error, t("Import failed")); }
}

async function copy(value: string): Promise<void> {
  try {
    if (!navigator.clipboard) throw new Error(t("Clipboard access is not available."));
    await navigator.clipboard.writeText(value);
    notify("ok", t("Copied to clipboard"), value.length > 40 ? `${value.slice(0, 40)}…` : value, 2000);
  } catch (error) { fail(error, t("Copy failed")); }
}
</script>

<template>
  <div class="page-head"><div class="titles"><div class="eyebrow">{{ t("Configuration") }}</div><h1 class="page-title">{{ t("Settings") }}</h1><p class="page-sub">{{ t("Runtime, security, sources and delivery for this OzDDNS instance.") }}</p></div></div>
  <div ref="tabsElement" class="tabs mb16">
    <button v-for="[key,label] in tabs" :key="key" :data-tab="key" :class="{ on: tab === key }" @click="router.push(key === 'general' ? '/settings' : `/settings/${key}`)">{{ t(label) }}</button>
  </div>

  <div v-if="tab === 'general'" class="grid cols-2">
    <div class="card pad">
      <div class="strong mb16">{{ t("Preferences") }}</div>
      <div class="field" data-no-i18n><label>{{ t("Language") }}</label><select v-model="form.language" class="input"><option v-for="code in languageCodes" :key="code" :value="code">{{ languageLabel(code) }}</option></select></div>
      <div class="field"><label>{{ t("Theme") }}</label><select v-model="form.theme" class="input"><option value="light">{{ t("Light") }}</option><option value="dark">{{ t("Dark") }}</option><option value="auto">{{ t("Auto") }}</option></select><span class="help">{{ t("Auto follows your operating system setting.") }}</span></div>
      <div class="field"><label>{{ t("Timezone") }}</label><select v-model="form.timezone" class="input mono"><option v-for="timezone in timezones" :key="timezone.value" :value="timezone.value">{{ timezone.label }}</option></select></div>
      <div class="field"><label>{{ t("Default interval (minutes)") }}</label><input v-model.number="form.defaultInterval" type="number" class="input mono"></div>
      <div class="field"><label>{{ t("Execution history (days)") }}</label><input v-model.number="form.retention" type="number" class="input mono"><span class="help">{{ t("Runs older than this are deleted for real, once an hour, so the database cannot grow without bound. 1–365.") }}</span></div>
      <button class="btn primary mt16" @click="saveSettings"><Icon name="save" />{{ t("Save changes") }}</button>
    </div>
    <div class="card pad">
      <div class="strong mb16">{{ t("Server") }}</div>
      <div class="field">
        <label>{{ t("Listen address") }} <span class="tiny muted">· {{ t("Read-only") }}</span></label>
        <div class="hstack wrap" style="gap:8px">
          <input class="input mono" :value="data.runtime.listen || '—'" readonly type="text" aria-readonly="true" :aria-label="t('Listen address')">
          <Pill kind="mute">{{ listenSourceLabel }}</Pill>
        </div>
        <span class="help">{{ t("Fixed at start-up — it is deployment configuration, not a panel setting. To change it, set --listen, the OZDDNS_LISTEN environment variable, or the listen key in config.yaml, then restart OzDDNS.") }}</span>
      </div>
      <div class="field">
        <label>{{ t("Base path") }} <span class="tiny muted">· {{ t("Read-only") }}</span></label>
        <div class="hstack wrap" style="gap:8px">
          <input class="input mono" :value="data.runtime.basePath || '/'" readonly type="text" aria-readonly="true" :aria-label="t('Base path')">
          <Pill kind="mute">{{ basePathSourceLabel }}</Pill>
        </div>
        <span class="help">{{ t("Public prefix the panel and API are served under, so a reverse proxy can mount them at a sub-path without rebuilding anything. To change it, set --base-path, the OZDDNS_BASE_PATH environment variable, or the basePath key in config.yaml, then restart OzDDNS.") }}</span>
      </div>
      <div class="callout mb16">
        <Icon name="shield" />
        <div class="grow" v-html="t('Loopback by default. {c} is only reachable from this host. Bind {zero} to expose the panel — pair it with authentication.', { c: '<code>127.0.0.1:9876</code>', zero: '<code>0.0.0.0</code>' })" />
      </div>
    </div>
  </div>

  <div v-else-if="tab === 'scheduler'" class="grid cols-2">
    <div class="card pad">
      <div class="between mb12"><span class="strong">{{ t("Scheduler") }}</span><Pill :kind="schedulerRunning ? 'ok' : 'mute'" dot>{{ t(schedulerRunning ? "Running" : "Paused") }}</Pill></div>
      <p class="tiny muted mb16">{{ t("OzDDNS wakes on a timer, reconciles every task whose interval has elapsed, and stops. Each task also keeps its own interval — this is the loop that drives them.") }}</p>
      <div class="switch-row"><div class="txt"><div class="t">{{ t(schedulerRunning ? "Automatic runs are on" : "Automatic runs are paused") }}</div><div class="d">{{ t(schedulerRunning ? "Due tasks update themselves on schedule." : "Nothing runs until you resume.") }}</div></div><Toggle :model-value="schedulerRunning" label="Scheduler" @click="toggleScheduler().catch(error => fail(error))" /></div>
      <div class="mt16">
        <div class="field"><label>{{ t("Check interval (seconds)") }}</label><input v-model.number="form.tick" type="number" class="input mono"><span class="help">{{ t("How often the scheduler looks for tasks that are due.") }}</span></div>
        <div class="field"><label>{{ t("Concurrent reconciliations") }}</label><input v-model.number="form.workers" type="number" class="input mono"><span class="help">{{ t("How many tasks may write to their provider at the same time.") }}</span></div>
        <div class="switch-row"><div class="txt"><div class="t">{{ t("Reconcile once at start-up") }}</div><div class="d">{{ t("Run every enabled task the moment the daemon boots.") }}</div></div><Toggle v-model="form.runOnStart" label="Reconcile once at start-up" /></div>
      </div>
      <button class="btn primary mt16" @click="saveSettings"><Icon name="save" />{{ t("Save changes") }}</button>
    </div>
    <div class="vstack">
      <div class="card pad"><div class="strong mb12">{{ t("How the schedule works") }}</div><ol class="sched-list"><li>{{ t("The daemon wakes on the ticker, scans every task, and reconciles the ones whose own interval has elapsed.") }}</li><li>{{ t("Each task keeps its individual interval — the ticker is just the heartbeat.") }}</li><li>{{ t("A manual “Run all” reconciles everything immediately and leaves the schedule untouched.") }}</li></ol></div>
      <div class="card pad"><div class="strong mb12">{{ t("Effective now") }}</div><div v-for="[key,value] in [[t('Status'),t(schedulerRunning?'Running':'Paused')],[t('Ticker'),t('every {n}s',{n:form.tick})],[t('Workers'),t('{n} at a time',{n:form.workers})],[t('Run at start-up'),t(form.runOnStart?'Yes':'No')],[t('History kept'),t('{n} day(s)',{n:form.retention})]]" :key="String(key)" class="between" style="padding:8px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">{{ key }}</span><span class="mono" style="font-size:12.5px">{{ value }}</span></div></div>
    </div>
  </div>

  <div v-else-if="tab === 'security'" class="vstack" style="max-width:820px">
    <div v-if="authError" class="banner err">
      <Icon name="alert-triangle" class="ico" />
      <div class="grow"><div class="b-title">{{ t("Security action failed") }}</div><div class="b-body">{{ authError }}</div></div>
      <button class="icon-btn" :aria-label="t('Dismiss')" @click="authError = ''"><Icon name="x" /></button>
    </div>

    <template v-if="!authState.enabled">
      <div class="card pad">
        <div class="between mb16">
          <div><div class="strong">{{ t("Authorization") }}</div><div class="tiny muted">{{ t("Default local-first mode") }}</div></div>
          <Pill kind="warn" dot>{{ t("Open — no sign-in") }}</Pill>
        </div>
        <p class="tiny muted mb16">{{ t("OzDDNS is currently open. Every API request is accepted without credentials. That is intentional while the service listens only on 127.0.0.1; it is unsafe as soon as the port is reachable from another machine.") }}</p>
        <div class="callout mb16" :class="{ 'callout-danger': authState.exposed }">
          <Icon name="shield" />
          <span v-if="authState.exposed">{{ t("This daemon is listening on {addr}, which is reachable beyond this host. Enable authorization before allowing traffic from other machines.", { addr: authState.listen }) }}</span>
          <span v-else>{{ t("Listening on {addr}. Local-only use does not need a login.", { addr: authState.listen }) }}</span>
        </div>

        <template v-if="setupStep === 'idle'">
          <div class="strong mb12">{{ t("Enable authorization") }}</div>
          <div v-if="authState.setupPending" class="callout mb16">{{ t("An unfinished enrollment already exists on the server. Starting again replaces its pending password and TOTP secret.") }} <button class="btn sm" type="button" @click="discardSetup">{{ t("Discard pending setup") }}</button></div>
          <div class="grid cols-2">
            <div>
              <div class="field"><label>{{ t("Administrator username") }}</label><input v-model="setupForm.username" class="input" autocomplete="username" maxlength="64"></div>
              <div class="field"><label>{{ t("Password") }}</label><input v-model="setupForm.password" class="input mono" type="password" autocomplete="new-password"><span class="help">{{ t("At least 10 characters. Stored as a salted PBKDF2-SHA256 hash.") }}</span></div>
              <div class="field"><label>{{ t("Confirm password") }}</label><input v-model="setupForm.password2" class="input mono" type="password" autocomplete="new-password"></div>
              <button class="btn primary mt8" type="button" :disabled="authBusy" @click="beginSetup"><Icon :class="authBusy ? 'spin' : ''" :name="authBusy ? 'loader' : 'shield-check'" />{{ t("Continue to TOTP setup") }}</button>
            </div>
            <div class="card pad" style="box-shadow:none">
              <div class="strong mb12">{{ t("What changes") }}</div>
              <ol class="sched-list">
                <li>{{ t("Every API route except sign-in and health checks requires a server-side session.") }}</li>
                <li>{{ t("Sign-in needs the password plus a 6-digit TOTP code.") }}</li>
                <li>{{ t("Ten one-time recovery codes are issued once, at enrollment.") }}</li>
                <li>{{ t("Turning authorization off deletes the account, sessions, TOTP secret and recovery codes.") }}</li>
              </ol>
            </div>
          </div>
        </template>

        <template v-else-if="setupStep === 'code' && setupInfo">
          <div class="strong mb12">{{ t("Connect your authenticator") }}</div>
          <p class="tiny muted mb16">{{ t("Scan the QR code with any RFC 6238 authenticator, then enter its current 6-digit code. The enrollment expires at {time}.", { time: new Date(setupInfo.expiresAt).toLocaleString() }) }}</p>
          <div class="totp-enroll mb16">
            <div class="totp-qr">
              <img v-if="setupInfo.qrCodeDataUrl" :src="setupInfo.qrCodeDataUrl" width="220" height="220" :alt="t('TOTP enrollment QR code')">
            </div>
            <div class="grow">
              <div class="strong mb8">{{ t("Can't scan the code?") }}</div>
              <p class="tiny muted mb12">{{ t("Enter this setup key manually in your authenticator. Keep it private until enrollment is complete.") }}</p>
              <div class="hstack wrap">
                <code class="manual-secret">{{ setupInfo.secret }}</code>
                <button class="icon-btn" type="button" :aria-label="t('Copy secret')" @click="copy(setupInfo.secret)"><Icon name="copy" /></button>
              </div>
              <a class="tiny mono mt12" :href="setupInfo.otpauthUrl" style="word-break:break-all">{{ setupInfo.otpauthUrl }}</a>
            </div>
          </div>
          <div class="field" style="max-width:320px"><label>{{ t("Current 6-digit code") }}</label><input v-model="setupForm.code" class="input mono" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="123456"></div>
          <div class="hstack wrap mt16">
            <button class="btn primary" type="button" :disabled="authBusy" @click="completeSetup"><Icon :class="authBusy ? 'spin' : ''" :name="authBusy ? 'loader' : 'check-circle'" />{{ t("Verify and enable") }}</button>
            <button class="btn" type="button" :disabled="authBusy" @click="discardSetup">{{ t("Cancel setup") }}</button>
          </div>
        </template>
      </div>
    </template>

    <template v-else>
      <div class="card pad">
        <div class="between mb16">
          <div><div class="strong">{{ t("Authorization enabled") }}</div><div class="tiny muted">{{ t("Server-side session + TOTP") }}</div></div>
          <Pill kind="ok" dot>{{ t("Protected") }}</Pill>
        </div>
        <div class="grid cols-2">
          <div>
            <div class="between security-row"><span class="tiny muted">{{ t("Administrator") }}</span><span class="mono">{{ authState.username }}</span></div>
            <div class="between security-row"><span class="tiny muted">{{ t("Listening on") }}</span><span class="mono">{{ authState.listen }}</span></div>
            <div class="between security-row"><span class="tiny muted">{{ t("Active sessions") }}</span><span class="mono">{{ authState.sessions }}</span></div>
            <div class="between security-row"><span class="tiny muted">{{ t("Recovery codes remaining") }}</span><span class="mono">{{ authState.recoveryCodesRemaining }} / 10</span></div>
          </div>
          <div class="card pad" style="box-shadow:none">
            <div class="strong mb8">{{ t("Session model") }}</div>
            <p class="tiny muted mb12">{{ t("The browser holds an opaque HttpOnly cookie. The database stores only its SHA-256 hash. Sessions expire after 30 minutes of inactivity or 12 hours in total.") }}</p>
            <button class="btn" type="button" @click="signOut"><Icon name="log-out" />{{ t("Sign out this browser") }}</button>
          </div>
        </div>
      </div>

      <div class="grid cols-2">
        <div class="card pad">
          <div class="strong mb8">{{ t("Replace recovery codes") }}</div>
          <p class="tiny muted mb16">{{ t("Enter your password and a current authenticator or recovery code. All previous recovery codes stop working immediately.") }}</p>
          <div class="field"><label>{{ t("Password") }}</label><input v-model="regenForm.password" class="input mono" type="password" autocomplete="current-password"></div>
          <div class="field"><label>{{ t("Authenticator or recovery code") }}</label><input v-model="regenForm.code" class="input mono" autocomplete="one-time-code"></div>
          <button class="btn" type="button" :disabled="authBusy" @click="rotateRecoveryCodes"><Icon name="refresh" />{{ t("Generate a new set") }}</button>
        </div>

        <div class="card pad">
          <div class="strong mb8">{{ t("Turn off authorization") }}</div>
          <p class="tiny muted mb16">{{ t("This returns the panel to open mode. The account, TOTP secret, recovery codes and every session are deleted. Enabling it again starts a completely new enrollment.") }}</p>
          <div class="field"><label>{{ t("Password") }}</label><input v-model="disableForm.password" class="input mono" type="password" autocomplete="current-password"></div>
          <div class="field"><label>{{ t("Authenticator or recovery code") }}</label><input v-model="disableForm.code" class="input mono" autocomplete="one-time-code"></div>
          <button class="btn danger" type="button" :disabled="authBusy" @click="disableAuthorization"><Icon name="power" />{{ t("Turn off authorization") }}</button>
        </div>
      </div>

      <div class="card pad">
        <div class="between mb12"><span class="strong">{{ t("Recent security activity") }}</span><button class="btn sm" type="button" @click="refreshSecurity"><Icon name="refresh" />{{ t("Refresh") }}</button></div>
        <div v-if="authEvents.length" class="table-wrap"><table class="table"><thead><tr><th>{{ t("When") }}</th><th>{{ t("Event") }}</th><th>{{ t("Detail") }}</th><th>{{ t("Source") }}</th></tr></thead><tbody><tr v-for="event in authEvents" :key="event.id"><td class="mono tiny">{{ new Date(event.at).toLocaleString() }}</td><td>{{ event.event }}</td><td class="tiny muted">{{ event.detail || "—" }}</td><td class="mono tiny">{{ event.ip || "—" }}</td></tr></tbody></table></div>
        <p v-else class="tiny muted">{{ t("No security activity recorded yet.") }}</p>
      </div>
    </template>

    <div v-if="setupStep === 'codes' && recoveryCodes.length" class="card pad">
      <div class="between mb12"><div><div class="strong">{{ t("Recovery codes") }}</div><div class="tiny muted">{{ t("Shown once. Each code works for exactly one sign-in.") }}</div></div><Pill kind="warn" dot>{{ t("Save them now") }}</Pill></div>
      <div class="recovery-grid"><code v-for="code in recoveryCodes" :key="code">{{ code }}</code></div>
      <div class="callout mb16">{{ t("Store these somewhere separate from the authenticator. If you lose both the authenticator and all recovery codes, use the local CLI break-glass command on the machine running OzDDNS: ozddns auth disable --yes") }}</div>
      <div class="hstack wrap">
        <button class="btn primary" type="button" @click="acknowledgeCodes"><Icon name="check-circle" />{{ t("I saved these codes") }}</button>
        <button class="btn" type="button" @click="copyRecoveryCodes"><Icon name="copy" />{{ t("Copy all") }}</button>
        <button class="btn" type="button" @click="downloadRecoveryCodes"><Icon name="download" />{{ t("Download .txt") }}</button>
      </div>
    </div>
  </div>

  <template v-else-if="tab === 'sources'">
    <Banner kind="info" title="What an IP source is" body="An IP source answers one question for a task — what address should this record hold right now? OzDDNS resolves it over HTTP, from a network interface, a shell command, or a fixed value, then writes it to your provider. Sources are reusable: pick one from the dropdown on the task form, or create a new one right there." />
    <div class="card mt16">
      <div class="card-head"><span class="h">{{ t("IP sources") }}</span><span class="spacer" /><button class="btn sm primary" @click="modal = { type: 'source' }"><Icon name="plus" />{{ t("Add source") }}</button></div>
      <div v-if="data.ipSources.length" class="table-wrap"><table class="table src-table"><thead><tr><th>{{ t("Name") }}</th><th class="col-type">{{ t("Type") }}</th><th>{{ t("Family") }}</th><th class="col-target">{{ t("Target") }}</th><th class="col-usedby">{{ t("Used by") }}</th><th class="r">{{ t("Actions") }}</th></tr></thead>
        <tbody><tr v-for="source in data.ipSources" :key="source.id">
          <td class="row-title">{{ source.name }}<span class="row-sub mono sub-target">{{ source.target }}</span><span class="row-sub sub-usedby">{{ t("Used by {n} task(s)", { n: usedByCount(source) }) }}</span></td>
          <td class="col-type">{{ data.ipSourceTypes.find(item => item.id === source.type)?.label || source.type }}</td><td><Pill :kind="source.family === 'v6' ? 'violet' : 'info'">{{ source.family.toUpperCase() }}</Pill></td>
          <td class="mono tiny col-target">{{ source.target }}</td><td class="num col-usedby">{{ usedByCount(source) }}</td>
          <td class="r"><div class="hstack" style="justify-content:flex-end;gap:6px"><button class="btn sm" :disabled="testingId === source.id" @click="testSource(source)"><Icon :class="testingId === source.id ? 'spin' : ''" :name="testingId === source.id ? 'loader' : 'refresh'" /><span class="lbl">{{ t(testingId === source.id ? "Testing…" : "Test") }}</span></button><button class="icon-btn" :disabled="testingId !== ''" :aria-label="t('Edit')" @click="modal = { type: 'source', source }"><Icon name="pencil" /></button><button class="icon-btn act-copy" :aria-label="t('Copy target')" @click="copy(source.target)"><Icon name="copy" /></button><button class="icon-btn" :disabled="testingId !== ''" :aria-label="t('Delete')" @click="removeSource(source)"><Icon name="trash" /></button></div></td>
        </tr></tbody>
      </table></div>
      <EmptyState v-else icon="globe" title="No IP sources yet" body="Add one to tell OzDDNS where this host's address comes from, then point a task at it."><button class="btn primary" @click="modal = { type: 'source' }"><Icon name="plus" />{{ t("Add source") }}</button></EmptyState>
    </div>
  </template>

  <div v-else-if="tab === 'notify'" class="card">
    <div class="card-head"><span class="h">{{ t("Notification channels") }}</span><span class="spacer" /><button class="btn sm primary" @click="modal = { type: 'notify' }"><Icon name="plus" />{{ t("Add channel") }}</button></div>
    <div class="card-body vstack"><div v-for="webhook in data.settings.notify.webhooks" :key="webhook.id" class="card pad" style="box-shadow:none"><div class="between"><div class="hstack"><span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)"><Icon :name="webhook.kind === 'slack' ? 'slack' : 'plug'" /></span><div><div class="strong">{{ webhook.name }}</div><div class="mono tiny muted">{{ webhook.target }}</div></div></div><div class="hstack"><Pill v-for="event in webhook.on" :key="event" kind="mute">{{ event }}</Pill><button class="btn sm" @click="testWebhook(webhook.id)">{{ t("Test") }}</button><Toggle :model-value="webhook.enabled" :label="webhook.name" @click="toggleNotification(webhook)" /><button class="icon-btn" :aria-label="t('Delete')" @click="removeNotification(webhook)"><Icon name="trash" /></button></div></div></div></div>
    <div class="card-body" style="border-top:1px solid var(--line-2)"><Banner kind="info" title="Shoutrrr-style URLs" body="Paste a service URL (Slack, Discord, ntfy, Gotify, Telegram…) and OzDDNS routes events to it." /></div>
  </div>

  <div v-else-if="tab === 'storage'" class="grid cols-2">
    <div class="card pad"><div class="strong mb16">{{ t("Database") }}</div><div v-for="[key,value] in [[t('Driver'),data.runtime.dbDriver],[t('SQLite path'),data.runtime.dbPath]]" :key="String(key)" class="between" style="padding:8px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">{{ key }}</span><span class="mono" style="font-size:12.5px">{{ value }}</span></div></div>
    <div class="card pad"><div class="strong mb12">{{ t("Backup & restore") }}</div><p class="tiny muted mb16">{{ t("Configuration backups include provider credentials. Store exported files securely.") }}</p><div class="hstack wrap"><button class="btn" @click="exportConfig"><Icon name="download" />{{ t("Export config") }}</button><button class="btn" @click="importInput?.click()"><Icon name="upload" />{{ t("Import config") }}</button><input ref="importInput" class="file-picker" type="file" accept="application/json,.json" tabindex="-1" aria-hidden="true" @change="importConfig"></div></div>
  </div>

  <div v-else class="grid cols-2">
    <div class="card pad"><div class="hstack mb12"><span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)"><Icon name="compass" /></span><div><div class="strong" style="font-size:16px">OzDDNS</div><div class="tiny muted">{{ t("Provider-centric dynamic DNS") }}</div></div></div><div v-for="[key,value] in [[t('Version'),data.runtime.version],[t('Build'),data.runtime.build],[t('Go runtime'),`${data.runtime.goVersion} · ${data.runtime.goos}/${data.runtime.goarch}`],[t('Database'),`${data.runtime.dbDriver} · ${data.runtime.dbPath}`]]" :key="String(key)" class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">{{ key }}</span><span class="mono" style="font-size:12.5px">{{ value }}</span></div><div class="hstack mt16"><a class="btn" href="https://github.com/echoface/ozddns#readme" target="_blank" rel="noreferrer"><Icon name="external-link" />{{ t("Documentation") }}</a></div></div>
    <div class="card pad"><div class="strong mb12">{{ t("Installed providers") }}</div><div class="hstack wrap" style="gap:8px"><Pill v-for="type in data.providerTypes" :key="type.id" kind="mute">{{ type.label }}</Pill></div><div class="divider" /><div class="tiny muted">{{ t("Providers are compiled into the binary and register themselves at start-up. Adding a new one means adding a package and registering a factory — no schema change required.") }}</div></div>
  </div>
</template>
