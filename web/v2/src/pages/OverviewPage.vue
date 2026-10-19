<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, PageHead, StatusPill } from "../components/UiKit";
import { fmtAgo, t } from "../lib";
import { api, bootState, data, fail, getProvider, getProviderType, notify, updateIPInfo } from "../store";
import type { IPFamilyInfo, IPInfo, IPSource } from "../types";

const V4_SOURCE_KEY = "oz.overview.ipv4SourceId";
const V6_SOURCE_KEY = "oz.overview.ipv6SourceId";

const router = useRouter();
const failing = computed(() => data.jobs.filter((job) => job.lastStatus === "failed"));
const enabled = computed(() => data.jobs.filter((job) => job.enabled));
const records = computed(() => data.providers.reduce((sum, provider) => sum + provider.records, 0));
const failures24h = computed(() => data.runs.filter((run) => run.status === "failed" && Date.now() - run.startedAt < 86400000).length);
const changes24h = computed(() => data.runs.filter((run) => run.status === "success" && Date.now() - run.startedAt < 86400000).length);
const ipLoading = ref(false);
const ipInfo = ref<IPInfo>(data.ipInfo);
const selectedV4ID = ref("");
const selectedV6ID = ref("");
const v4Sources = computed(() => data.ipSources.filter((source) => source.family === "v4"));
const v6Sources = computed(() => data.ipSources.filter((source) => source.family === "v6"));
let ipRequestSequence = 0;

function familyAddress(info: IPFamilyInfo): string {
  return info.status === "ok" ? info.address || "—" : "—";
}

function familyPill(info: IPFamilyInfo): string {
  if (info.status === "error") return "err";
  if (info.status === "no_source") return "mute";
  return "ok";
}

function familyPillText(info: IPFamilyInfo): string {
  if (info.status === "error") return t("Check failed");
  if (info.status === "no_source") return t("Not configured");
  return fmtAgo(info.checkedAt);
}

function familyError(info: IPFamilyInfo): string {
  return info.error || t("The IP source could not be read.");
}

function familyMetaTitle(info: IPFamilyInfo): string {
  return [
    info.sourceTarget ? t("Target · {s}", { s: info.sourceTarget }) : "",
    info.status === "error" ? familyError(info) : ""
  ].filter(Boolean).join(" · ");
}

function storedSourceID(key: string, sources: IPSource[]): string {
  let stored = "";
  try { stored = localStorage.getItem(key) || ""; } catch { /* storage can be disabled */ }
  return sources.some((source) => source.id === stored) ? stored : (sources[0]?.id || "");
}

function restoreSourceSelections(): void {
  const nextV4 = storedSourceID(V4_SOURCE_KEY, v4Sources.value);
  const nextV6 = storedSourceID(V6_SOURCE_KEY, v6Sources.value);
  selectedV4ID.value = nextV4;
  selectedV6ID.value = nextV6;
  persistSelection(V4_SOURCE_KEY, nextV4);
  persistSelection(V6_SOURCE_KEY, nextV6);
}

function persistSelection(key: string, value: string): void {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch { /* storage can be disabled */ }
}

function onSourceChange(family: "v4" | "v6"): void {
  if (family === "v4") persistSelection(V4_SOURCE_KEY, selectedV4ID.value);
  else persistSelection(V6_SOURCE_KEY, selectedV6ID.value);
  void loadIP().catch((error) => fail(error, t("Refresh failed")));
}

// The address cards are an inspection tool, not task state. Resolve exactly
// the sources selected in this browser so background bootstrap polling cannot
// replace the operator's choice with an unrelated default.
async function loadIP(): Promise<void> {
  const requestID = ++ipRequestSequence;
  ipLoading.value = true;
  try {
    if (bootState.value === "offline") return;
    const query = new URLSearchParams();
    if (selectedV4ID.value) query.set("v4SourceId", selectedV4ID.value);
    if (selectedV6ID.value) query.set("v6SourceId", selectedV6ID.value);
    const suffix = query.size ? `?${query.toString()}` : "";
    const payload = await api.get(`/ip${suffix}`);
    if (requestID !== ipRequestSequence) return;
    updateIPInfo(payload);
    ipInfo.value = data.ipInfo;
  } finally {
    if (requestID === ipRequestSequence) ipLoading.value = false;
  }
}

async function refreshIP(): Promise<void> {
  try {
    await loadIP();
    notify("ok", t("IPv4 + IPv6 refreshed"), t("Both selected sources were re-checked."));
  } catch (error) { fail(error, t("Refresh failed")); }
}

onMounted(() => {
  restoreSourceSelections();
  loadIP().catch(() => {});
});

watch(
  () => data.ipSources.map((source) => `${source.id}:${source.family}:${source.target}`).join("|"),
  () => {
    restoreSourceSelections();
    void loadIP().catch((error) => fail(error, t("Refresh failed")));
  }
);
</script>

<template>
  <PageHead
    eyebrow="Control room"
    title="Overview"
    sub="Everything OzDDNS is keeping in sync right now — providers, tasks, and the reconciliation between your hosts and DNS."
  >
    <template #actions>
      <button class="btn primary" @click="router.push('/jobs/new')"><Icon name="plus" />{{ t("New task") }}</button>
    </template>
  </PageHead>

  <div class="grid cols-3 mb24 reveal">
    <div class="card stat">
      <span class="lab"><Icon name="jobs" />{{ t("Active tasks") }}</span>
      <span class="val">{{ enabled.length }}<small> / {{ data.jobs.length }}</small></span>
      <span class="foot">{{ t("{n} writes in 24h", { n: changes24h }) }}</span>
    </div>
    <div class="card stat">
      <span class="lab"><Icon name="database" />{{ t("Records managed") }}</span>
      <span class="val">{{ records }}</span>
      <span class="foot">{{ t("across {n} providers", { n: data.providers.length }) }}</span>
    </div>
    <div class="card stat">
      <span class="lab"><Icon name="alert-triangle" />{{ t("Failures · 24h") }}</span>
      <span class="val">{{ failures24h }}</span>
      <span class="foot">{{ t(failures24h ? "needs attention" : "all clear") }}</span>
    </div>
  </div>

  <div class="card mb24">
    <div class="ip-hero" :aria-busy="ipLoading">
      <div class="col">
        <div class="hstack between">
          <span class="eyebrow">{{ t("Public IPv4") }}</span>
          <span class="pill" :class="familyPill(ipInfo.v4)"><span class="dot" />{{ familyPillText(ipInfo.v4) }}</span>
        </div>
        <div class="ip-source-picker mt12">
          <label for="overview-ipv4-source">{{ t("Displayed source") }}</label>
          <div class="select-shell">
            <select
              id="overview-ipv4-source"
              v-model="selectedV4ID"
              class="input"
              :disabled="!v4Sources.length"
              :aria-label="t('Displayed IPv4 source')"
              @change="onSourceChange('v4')"
            >
              <option v-if="!v4Sources.length" value="">{{ t("No IPv4 source configured") }}</option>
              <option v-for="source in v4Sources" :key="source.id" :value="source.id">{{ source.name }}</option>
            </select>
            <Icon name="chevron-down" class="select-arrow" />
          </div>
        </div>
        <div class="ip-address-slot">
          <div v-if="ipLoading && !ipInfo.v4.address" class="sk" style="width:220px;height:30px" />
          <div v-else class="ipv4">{{ familyAddress(ipInfo.v4) }}</div>
        </div>
        <div class="ip-meta-slot" aria-live="polite">
          <template v-if="ipInfo.v4.status === 'error'">
            <span v-if="ipInfo.v4.sourceTarget" class="ip-meta-target" :title="t('Target · {s}', { s: ipInfo.v4.sourceTarget })">{{ t("Target · {s}", { s: ipInfo.v4.sourceTarget }) }}</span>
            <span class="ip-meta-error" :title="familyMetaTitle(ipInfo.v4)">{{ familyError(ipInfo.v4) }}</span>
          </template>
          <div v-else-if="ipInfo.v4.status === 'no_source'" class="ip-meta-empty">
            <span>{{ t("No IPv4 source is configured.") }}</span>
            <RouterLink to="/settings/sources" style="color:var(--coral-700);text-decoration:underline">{{ t("Add a source") }}</RouterLink>
          </div>
          <span v-else-if="ipInfo.v4.sourceTarget" class="ip-meta-target" :title="t('Target · {s}', { s: ipInfo.v4.sourceTarget })">{{ t("Target · {s}", { s: ipInfo.v4.sourceTarget }) }}</span>
        </div>
      </div>
      <div class="col">
        <div class="hstack between">
          <span class="eyebrow">{{ t("Public IPv6") }}</span>
          <span class="pill" :class="familyPill(ipInfo.v6)"><span class="dot" />{{ familyPillText(ipInfo.v6) }}</span>
        </div>
        <div class="ip-source-picker mt12">
          <label for="overview-ipv6-source">{{ t("Displayed source") }}</label>
          <div class="select-shell">
            <select
              id="overview-ipv6-source"
              v-model="selectedV6ID"
              class="input"
              :disabled="!v6Sources.length"
              :aria-label="t('Displayed IPv6 source')"
              @change="onSourceChange('v6')"
            >
              <option v-if="!v6Sources.length" value="">{{ t("No IPv6 source configured") }}</option>
              <option v-for="source in v6Sources" :key="source.id" :value="source.id">{{ source.name }}</option>
            </select>
            <Icon name="chevron-down" class="select-arrow" />
          </div>
        </div>
        <div class="ip-address-slot">
          <div v-if="ipLoading && !ipInfo.v6.address" class="sk" style="width:220px;height:30px" />
          <div v-else class="ipv6">{{ familyAddress(ipInfo.v6) }}</div>
        </div>
        <div class="ip-meta-slot" aria-live="polite">
          <template v-if="ipInfo.v6.status === 'error'">
            <span v-if="ipInfo.v6.sourceTarget" class="ip-meta-target" :title="t('Target · {s}', { s: ipInfo.v6.sourceTarget })">{{ t("Target · {s}", { s: ipInfo.v6.sourceTarget }) }}</span>
            <span class="ip-meta-error" :title="familyMetaTitle(ipInfo.v6)">{{ familyError(ipInfo.v6) }}</span>
          </template>
          <div v-else-if="ipInfo.v6.status === 'no_source'" class="ip-meta-empty">
            <span>{{ t("No IPv6 source is configured.") }}</span>
            <RouterLink to="/settings/sources" style="color:var(--coral-700);text-decoration:underline">{{ t("Add a source") }}</RouterLink>
          </div>
          <span v-else-if="ipInfo.v6.sourceTarget" class="ip-meta-target" :title="t('Target · {s}', { s: ipInfo.v6.sourceTarget })">{{ t("Target · {s}", { s: ipInfo.v6.sourceTarget }) }}</span>
        </div>
      </div>
    </div>
    <div class="ip-actions">
      <button class="btn sm" :disabled="ipLoading || (!v4Sources.length && !v6Sources.length)" @click="refreshIP"><Icon :name="ipLoading ? 'loader' : 'refresh'" :class="ipLoading ? 'spin' : ''" />{{ t("Refresh IPv4 + IPv6") }}</button>
      <span v-if="v6Sources.length" class="ip-action-hint tiny muted" :title="t('Some hosts rotate their IPv6 prefix daily. OzDDNS writes only when the resolved address changes.')">{{ t("Some hosts rotate their IPv6 prefix daily. OzDDNS writes only when the resolved address changes.") }}</span>
    </div>
  </div>

  <div v-if="failing.length" class="mb24">
    <Banner kind="err" :title="t(failing.length > 1 ? '{n} tasks need attention' : '{n} task needs attention', { n: failing.length })">
      <b>{{ failing[0].name }}</b> — {{ failing[0].error || t("update failed") }}.
      <RouterLink :to="`/jobs/${failing[0].id}`" style="color:var(--rust);text-decoration:underline">{{ t("Open task") }}</RouterLink>
    </Banner>
  </div>

  <div class="grid overview-lower" style="grid-template-columns:1.7fr 1fr">
    <div class="card table-card-scroll">
      <div class="card-head"><span class="h">{{ t("Sync tasks") }}</span><span class="spacer" /><RouterLink class="btn sm ghost" to="/jobs">{{ t("View all") }} <Icon name="chevron-right" /></RouterLink></div>
      <table class="table clickable">
        <thead><tr><th>{{ t("Task") }}</th><th>{{ t("Record") }}</th><th>{{ t("Last result") }}</th></tr></thead>
        <tbody>
          <tr v-for="job in data.jobs.slice(0,6)" :key="job.id" @click="router.push(`/jobs/${job.id}`)">
            <td><div class="row-title">{{ job.name }}</div><div class="row-sub">{{ getProviderType(getProvider(job.providerId)?.type || "")?.label }}</div></td>
            <td>
              <div class="hstack" style="gap:8px">
                <span class="rtype" :class="job.targets[0]?.type">{{ job.targets[0]?.type }}</span>
                <div><div class="row-title mono" style="font-size:13px">{{ job.targets[0]?.domain }}</div><div class="row-sub">{{ job.targets.length > 1 ? t("{n} more", { n: job.targets.length - 1 }) + " · " : "" }}{{ job.current || "—" }}</div></div>
              </div>
            </td>
            <td><StatusPill :status="job.lastStatus" /></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="card">
      <div class="card-head"><span class="h">{{ t("Activity") }}</span><span class="spacer" /><button class="icon-btn" :aria-label="t('Executions')" @click="router.push('/executions')"><Icon name="activity" /></button></div>
      <div class="card-body"><div class="timeline">
        <div v-for="(activity, index) in data.activity.slice(0,6)" :key="`${activity.at}-${index}`" class="tl-item">
          <div class="tl-rail"><span class="tl-node" :class="activity.kind" /><span v-if="index < Math.min(data.activity.length,6)-1" class="tl-line" /></div>
          <div class="tl-body"><div class="tl-text"><b>{{ activity.title }}</b></div><div class="tl-text muted">{{ activity.text }}</div><div class="tl-time">{{ fmtAgo(activity.at) }}</div></div>
        </div>
      </div></div>
    </div>
  </div>
</template>
