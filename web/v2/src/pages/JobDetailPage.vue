<script setup lang="ts">
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, DiffValue, EmptyState, Pill, StatusPill, Toggle } from "../components/UiKit";
import { fmtAgo, fmtDateTime, fmtDur, fmtIn, t } from "../lib";
import {
  api,
  bootState,
  confirmModal,
  deleteJob,
  data,
  fail,
  getJob,
  getProvider,
  getProviderType,
  getSource,
  modal,
  notify,
  runJob,
  toggleJob
} from "../store";

const route = useRoute();
const router = useRouter();
const job = computed(() => getJob(String(route.params.id)));
const provider = computed(() => job.value ? getProvider(job.value.providerId) : undefined);
const source = computed(() => job.value ? getSource(job.value.ipSourceId) : undefined);
const runs = computed(() => {
  if (!job.value) return [];
  return [...data.runs].filter((run) => run.jobId === job.value?.id).sort((a, b) => b.startedAt - a.startedAt).slice(0, 8);
});

async function execute(): Promise<void> {
  if (!job.value) return;
  try { await runJob(job.value); } catch (error) { fail(error, t("Run failed")); }
}

async function preview(): Promise<void> {
  if (!job.value) return;
  if (bootState.value === "offline") {
    notify("info", t("Preview unavailable"), t("Connect to the OzDDNS server to compute a provider diff."));
    return;
  }
  try {
    const diff = await api.post(`/jobs/${job.value.id}/dry-run`);
    modal.value = { type: "dry-run", job: job.value, diff };
  } catch (error) { fail(error, t("Preview failed")); }
}

async function changeEnabled(): Promise<void> {
  if (!job.value) return;
  try { await toggleJob(job.value); } catch (error) { fail(error); }
}

function remove(): void {
  if (!job.value) return;
  const value = job.value;
  confirmModal(
    t("Delete sync task?"),
    t('"{name}" and its history will be removed.', { name: value.name }),
    t("Delete task"),
    () => deleteJob(value)
  );
}
</script>

<template>
  <template v-if="job">
    <div class="crumb mb16"><RouterLink to="/jobs" class="muted">{{ t("Sync tasks") }}</RouterLink><span class="sep">/</span><b>{{ job.name }}</b></div>
    <div class="page-head">
      <div class="titles">
        <div class="hstack" style="gap:12px">
          <Toggle :model-value="job.enabled" :label="t(job.enabled ? 'Disable task' : 'Enable task')" @click="changeEnabled" />
          <h1 class="page-title" style="font-size:26px">{{ job.name }}</h1>
          <StatusPill :status="job.lastStatus" />
        </div>
        <p class="page-sub">{{ t("{src} → {provider} · {n} record(s)", { src: source?.name || "—", provider: getProviderType(provider?.type || "")?.label || "—", n: job.targets.length }) }}</p>
      </div>
      <div class="page-actions">
        <button class="btn" @click="preview"><Icon name="eye" />{{ t("Preview") }}</button>
        <button class="btn" @click="execute"><Icon name="play" />{{ t("Run now") }}</button>
        <button class="btn primary" @click="router.push(`/jobs/${job.id}/edit`)"><Icon name="pencil" />{{ t("Edit") }}</button>
        <button class="btn danger" @click="remove"><Icon name="trash" />{{ t("Delete") }}</button>
      </div>
    </div>

    <div v-if="job.lastStatus === 'failed'" class="mb16"><Banner kind="err" title="Last run failed"><span>{{ job.error || t("The provider rejected the update.") }} </span><RouterLink to="/executions" style="color:var(--rust);text-decoration:underline">{{ t("See execution log") }}</RouterLink></Banner></div>
    <div v-if="job.note" class="mb16"><Banner kind="info" title="Waiting for data" :body="job.note" /></div>

    <div class="grid split">
      <div class="card pad mb16">
        <div class="strong mb12">{{ t("Configuration") }}</div>
        <div v-for="[key,value] in [
          [t('Provider'), getProviderType(provider?.type || '')?.label],
          [t('IP source'), source?.name],
          [t('Interval'), t('{n} min', { n: job.intervalMinutes })],
          [t('Jitter'), t(job.jitter ? 'Enabled (±10%)' : 'Off')],
          [t('Last changed'), job.lastChanged ? fmtAgo(job.lastChanged) : t('never')]
        ]" :key="String(key)" class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">{{ key }}</span><span class="strong" style="font-size:13px">{{ value }}</span></div>
      </div>

      <div class="card mb16 table-card-scroll">
        <div class="card-head"><span class="h">{{ t("Reconciliation") }}</span><span class="spacer" /><Pill :kind="job.lastStatus === 'failed' ? 'err' : 'ok'" dot>{{ t(job.lastStatus === "failed" ? "Out of sync" : "In sync") }}</Pill></div>
        <table class="table">
          <thead><tr><th>{{ t("Record") }}</th><th>{{ t("Current") }}</th><th>{{ t("Desired") }}</th><th>{{ t("TTL") }}</th><th>{{ t("Flags") }}</th></tr></thead>
          <tbody><tr v-for="target in job.targets" :key="`${target.domain}-${target.type}`">
            <td><div class="hstack" style="gap:8px"><span class="rtype" :class="target.type">{{ target.type }}</span><span class="mono" style="font-size:13px">{{ target.domain }}</span></div><div v-if="target.comment" class="row-sub">{{ target.comment }}</div></td>
            <td class="mono tiny">{{ job.current }}</td>
            <td><DiffValue :old-value="job.current" :new-value="job.desired" /></td>
            <td class="mono tiny">{{ target.ttl }}s</td>
            <td><Pill v-if="target.proxied" kind="coral">{{ t("Proxied") }}</Pill><span v-else class="muted tiny">{{ t("DNS only") }}</span></td>
          </tr></tbody>
        </table>
        <div class="card-body" style="border-top:1px solid var(--line-2)"><span class="tiny muted">{{ t("Last reconciled {ago} · next {next}", { ago: fmtAgo(job.lastRun), next: job.enabled ? fmtIn(job.nextRun) : t("paused") }) }}</span></div>
      </div>
    </div>

    <div class="mt16 card table-card-scroll">
      <div class="card-head"><span class="h">{{ t("Run history") }}</span><span class="spacer" /><RouterLink class="btn sm ghost" to="/executions">{{ t("All executions") }}<Icon name="chevron-right" /></RouterLink></div>
      <table class="table clickable">
        <thead><tr><th>{{ t("When") }}</th><th>{{ t("Trigger") }}</th><th>{{ t("Change") }}</th><th>{{ t("Duration") }}</th><th>{{ t("Result") }}</th></tr></thead>
        <tbody><tr v-for="run in runs" :key="run.id" @click="modal = { type: 'run', run }">
          <td class="mono tiny">{{ fmtDateTime(run.startedAt) }}</td><td><Pill :kind="run.trigger === 'manual' ? 'info' : 'mute'">{{ t(run.trigger) }}</Pill></td>
          <td><DiffValue :old-value="run.oldIp || ''" :new-value="run.newIp || ''" /></td><td class="mono tiny">{{ fmtDur(run.durationMs) }}</td><td><StatusPill :status="run.status" /></td>
        </tr></tbody>
      </table>
    </div>
  </template>
  <div v-else class="card"><EmptyState icon="alert-triangle" title="Task not found" body="The thing you were looking for does not exist (or was deleted)."><RouterLink class="btn primary" to="/overview">{{ t("Back to overview") }}</RouterLink></EmptyState></div>
</template>
