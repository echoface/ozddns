<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, DiffValue, PageHead, StatusPill } from "../components/UiKit";
import { fmtAgo, t } from "../lib";
import { api, bootState, data, fail, getProvider, getProviderType, notify, refresh } from "../store";

const router = useRouter();
const failing = computed(() => data.jobs.filter((job) => job.lastStatus === "failed"));
const enabled = computed(() => data.jobs.filter((job) => job.enabled));
const records = computed(() => data.providers.reduce((sum, provider) => sum + provider.records, 0));
const failures24h = computed(() => data.runs.filter((run) => run.status === "failed" && Date.now() - run.startedAt < 86400000).length);
const changes24h = computed(() => data.runs.filter((run) => run.status === "success" && Date.now() - run.startedAt < 86400000).length);
const ipLoading = ref(false);

// The public address is resolved lazily: bootstrap only carries whatever was
// cached, so the Overview fetches the live value once it actually needs it.
async function loadIP(): Promise<void> {
  if (ipLoading.value) return;
  ipLoading.value = true;
  try {
    if (bootState.value === "offline") {
      data.ipInfo.v4Checked = Date.now();
      data.ipInfo.v6Checked = Date.now();
    } else {
      const ip = await api.get("/ip");
      data.ipInfo.v4 = ip.v4 || "";
      data.ipInfo.v6 = ip.v6 || "";
      data.ipInfo.v4Source = ip.v4Source || "—";
      data.ipInfo.v6Source = ip.v6Source || "—";
      data.ipInfo.v4Checked = data.ipInfo.v6Checked = Date.parse(ip.checkedAt);
    }
  } finally {
    ipLoading.value = false;
  }
}

async function refreshIP(): Promise<void> {
  try {
    await loadIP();
    notify("ok", t("IP refreshed"), t("Re-checked the public address."));
  } catch (error) { fail(error, t("Refresh failed")); }
}

onMounted(() => { loadIP().catch(() => {}); });
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

  <div class="grid cols-4 mb24 reveal">
    <div class="card stat">
      <span class="lab"><Icon name="globe" />{{ t("Public IPv4") }}</span>
      <span v-if="ipLoading && !data.ipInfo.v4" class="sk" style="width:132px;height:26px" />
      <span v-else class="val">{{ data.ipInfo.v4 || "—" }}</span>
      <span class="foot">{{ t("via {src} · {ago}", { src: data.ipInfo.v4Source, ago: fmtAgo(data.ipInfo.v4Checked) }) }}</span>
    </div>
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
    <div class="ip-hero">
      <div class="col">
        <div class="hstack between">
          <span class="lab stat" style="padding:0"><span class="eyebrow">{{ t("Public IPv4") }}</span></span>
          <span class="pill mute"><span class="dot" />{{ fmtAgo(data.ipInfo.v4Checked) }}</span>
        </div>
        <div v-if="ipLoading && !data.ipInfo.v4" class="sk mt8" style="width:220px;height:30px" />
        <div v-else class="ipv4 mt8">{{ data.ipInfo.v4 || "—" }}</div>
        <div class="row-sub mt8">{{ t("Source · {s} · used by {n} tasks", { s: data.ipInfo.v4Source, n: data.jobs.length }) }}</div>
        <div class="hstack mt16">
          <button class="btn sm" @click="refreshIP"><Icon name="refresh" />{{ t("Refresh IP") }}</button>
          <span class="tiny muted">{{ t("Re-checks the public address") }}</span>
        </div>
      </div>
      <div class="col">
        <div class="hstack between">
          <span class="eyebrow">{{ t("Public IPv6") }}</span>
          <span class="pill mute"><span class="dot" />{{ fmtAgo(data.ipInfo.v6Checked) }}</span>
        </div>
        <div v-if="ipLoading && !data.ipInfo.v6" class="sk mt8" style="width:220px;height:30px" />
        <div v-else class="ipv6 mt8">{{ data.ipInfo.v6 || "—" }}</div>
        <div class="row-sub mt8">{{ t("Source · {s}", { s: data.ipInfo.v6Source }) }}</div>
        <div class="callout mt16">{{ t("Some hosts rotate their IPv6 prefix daily. OzDDNS writes only when the resolved address changes.") }}</div>
      </div>
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
