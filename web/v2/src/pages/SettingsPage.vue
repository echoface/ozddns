<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, EmptyState, Pill, Toggle } from "../components/UiKit";
import { applyTheme, setLocale, t } from "../lib";
import {
  api,
  bootState,
  confirmModal,
  data,
  deleteSource,
  fail,
  modal,
  notify,
  refresh,
  schedulerRunning,
  toggleScheduler
} from "../store";

const route = useRoute();
const router = useRouter();
const tab = computed(() => String(route.params.tab || "general"));
const tabs = [["general", "General"], ["scheduler", "Scheduler"], ["security", "Security"], ["sources", "IP sources"], ["notify", "Notifications"], ["storage", "Storage"], ["about", "About"]];
const form = reactive<Record<string, any>>({});
const languageCodes = window.OZ.i18n.codes;
const languageLabel = (code: string) => window.OZ.i18n.label(code);
const timezones = ["UTC", ...(((Intl as any).supportedValuesOf?.("timeZone") as string[] | undefined) || [
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Europe/London",
  "Europe/Paris",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Australia/Sydney"
])];
const importInput = ref<HTMLInputElement | null>(null);
const tabsElement = ref<HTMLElement | null>(null);
const testingId = ref("");

// Where the bind address came from: start-up flag, environment variable,
// config.yaml, or the built-in default. Shown so the address is never a mystery.
const listenSourceLabel = computed(() => {
  const labels: Record<string, string> = {
    flag: t("start-up flag"),
    env: t("environment variable"),
    config: t("config.yaml"),
    default: t("built-in default")
  };
  return labels[data.runtime.listenSource] || t("built-in default");
});

watch(tab, async (value) => {
  await nextTick();
  const button = tabsElement.value?.querySelector<HTMLElement>(`[data-tab="${value}"]`);
  if (!button || !tabsElement.value) return;
  tabsElement.value.scrollTo({
    left: button.offsetLeft - (tabsElement.value.clientWidth - button.offsetWidth) / 2
  });
}, { immediate: true });

watch(() => data.settings, (settings) => {
  Object.assign(form, {
    language: settings.general.language,
    theme: settings.general.theme,
    timezone: settings.general.timezone,
    defaultInterval: settings.general.defaultInterval,
    retention: settings.general.runRetentionDays,
    tick: settings.general.schedulerTickSeconds,
    workers: settings.general.schedulerWorkers,
    runOnStart: settings.general.runOnStart,
    username: settings.security.username,
    password: "",
    password2: ""
  });
}, { immediate: true, deep: true });

async function saveSettings(): Promise<void> {
  if (form.password && form.password !== form.password2) {
    notify("err", t("Save failed"), t("Passwords do not match."));
    return;
  }
  const payload: any = {
    language: form.language,
    theme: form.theme,
    timezone: form.timezone,
    defaultInterval: Number(form.defaultInterval),
    schedulerTickSeconds: Number(form.tick),
    schedulerWorkers: Number(form.workers),
    runRetentionDays: Number(form.retention),
    runOnStart: !!form.runOnStart,
    username: form.username,
    notifications: data.settings.notify.webhooks
  };
  if (form.password) payload.password = form.password;
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
      t("Providers, IP sources, tasks and settings will be replaced. Execution history is kept."),
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
      <div class="field"><label>{{ t("Timezone") }}</label><select v-model="form.timezone" class="input mono"><option v-for="timezone in timezones" :key="timezone" :value="timezone">{{ timezone }}</option></select></div>
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
      <div class="callout mb16" v-html="t('Loopback by default. {c} is only reachable from this host. Bind {zero} to expose the panel — pair it with authentication.', { c: '<code>127.0.0.1:9876</code>', zero: '<code>0.0.0.0</code>' })" />
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

  <div v-else-if="tab === 'security'" style="max-width:760px">
    <div class="card pad"><div class="strong mb16">{{ t("Administrator") }}</div>
      <div class="field"><label>{{ t("Username") }}</label><input v-model="form.username" class="input"></div>
      <div class="field"><label>{{ t("New password") }}</label><input v-model="form.password" type="password" class="input mono"><span class="help">{{ t("Leave blank to keep the current password.") }}</span></div>
      <div class="field"><label>{{ t("Confirm password") }}</label><input v-model="form.password2" type="password" class="input mono"></div>
      <button class="btn primary mt16" @click="saveSettings"><Icon name="save" />{{ t("Save changes") }}</button>
    </div>
  </div>

  <template v-else-if="tab === 'sources'">
    <Banner kind="info" title="What an IP source is" body="An IP source answers one question for a task — what address should this record hold right now? OzDDNS resolves it over HTTP, from a network interface, a shell command, or a fixed value, then writes it to your provider. Sources are reusable: pick one from the dropdown on the task form, or create a new one right there." />
    <div class="card mt16">
      <div class="card-head"><span class="h">{{ t("IP sources") }}</span><span class="spacer" /><button class="btn sm primary" @click="modal = { type: 'source' }"><Icon name="plus" />{{ t("Add source") }}</button></div>
      <div v-if="data.ipSources.length" class="table-wrap"><table class="table src-table"><thead><tr><th>{{ t("Name") }}</th><th class="col-type">{{ t("Type") }}</th><th>{{ t("Family") }}</th><th>{{ t("Target") }}</th><th>{{ t("Used by") }}</th><th class="r">{{ t("Actions") }}</th></tr></thead>
        <tbody><tr v-for="source in data.ipSources" :key="source.id">
          <td class="row-title">{{ source.name }}<span class="row-sub mono sub-target">{{ source.target }}</span></td>
          <td class="col-type">{{ data.ipSourceTypes.find(item => item.id === source.type)?.label || source.type }}</td><td><Pill :kind="source.family === 'v6' ? 'violet' : 'info'">{{ source.family.toUpperCase() }}</Pill></td>
          <td class="mono tiny col-target">{{ source.target }}</td><td class="num">{{ data.jobs.filter(job => job.ipSourceId === source.id).length }}</td>
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
