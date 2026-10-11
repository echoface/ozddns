<script setup lang="ts">
import { computed } from "vue";
import Icon from "../components/Icon.vue";
import RunRecords from "../components/RunRecords.vue";
import { DiffValue, EmptyState, StatusPill } from "../components/UiKit";
import { fmtDateTime, fmtDur, t } from "../lib";
import { data, getJob, modal, runFilter } from "../store";

const statuses = [
  ["all", "All"],
  ["success", "Success"],
  ["nochange", "No change"],
  ["failed", "Failed"],
  ["skipped", "Skipped"]
];
const counts = computed(() => Object.fromEntries(statuses.map(([status]) => [
  status,
  status === "all" ? data.runs.length : data.runs.filter((run) => run.status === status).length
])));
const list = computed(() => [...data.runs].filter((run) => {
  if (runFilter.status !== "all" && run.status !== runFilter.status) return false;
  if (runFilter.job !== "all" && run.jobId !== runFilter.job) return false;
  return true;
}).sort((a, b) => b.startedAt - a.startedAt));
const days = computed(() => data.settings?.general?.runRetentionDays || 1);
const windowLabel = computed(() => days.value === 1 ? t("last 24 hours") : t("last {n} days", { n: days.value }));
</script>

<template>
  <div class="page-head">
    <div class="titles">
      <div class="eyebrow">{{ t("Observability") }}</div>
      <h1 class="page-title">{{ t("Executions") }}</h1>
      <p class="page-sub">{{ t("Every reconciliation the engine attempted, with the exact diff it applied. Click a row to inspect the API exchange.") }} {{ t("History is kept for {win} and purged hourly.", { win: windowLabel }) }}</p>
    </div>
  </div>

  <div class="grid cols-4 mb16">
    <div v-for="stat in [
      ['Total runs', data.runs.length, windowLabel, 'activity'],
      ['Writes', counts.success, 'records updated', 'check-circle'],
      ['No-ops', counts.nochange, 'already in sync', 'circle-dot'],
      ['Failures', counts.failed, 'need review', 'x-circle']
    ]" :key="String(stat[0])" class="card stat">
      <span class="lab"><Icon :name="String(stat[3])" />{{ t(String(stat[0])) }}</span>
      <span class="val">{{ stat[1] }}</span><span class="foot">{{ t(String(stat[2])) }}</span>
    </div>
  </div>

  <div class="card" style="border-top:0">
    <div class="card-head" style="flex-wrap:wrap;gap:10px;row-gap:8px">
      <div class="seg" style="flex-wrap:wrap">
        <button v-for="[status,label] in statuses" :key="status" :class="{ on: runFilter.status === status }" @click="runFilter.status = status">{{ t(label) }} · {{ counts[status] }}</button>
      </div>
      <span class="spacer" />
      <select v-model="runFilter.job" class="input" style="width:auto;max-width:100%">
        <option value="all">{{ t("All tasks") }}</option><option v-for="job in data.jobs" :key="job.id" :value="job.id">{{ job.name }}</option>
      </select>
    </div>
    <div class="table-wrap">
      <table class="table clickable exec-table">
        <thead><tr><th>{{ t("Time") }}</th><th>{{ t("Task") }}</th><th class="col-trigger">{{ t("Trigger") }}</th><th>{{ t("Change") }}</th><th>{{ t("Records") }}</th><th class="col-duration">{{ t("Duration") }}</th><th>{{ t("Detail") }}</th></tr></thead>
        <tbody>
          <tr v-for="run in list" :key="run.id" tabindex="0" @click="modal = { type: 'run', run }" @keydown.enter="modal = { type: 'run', run }">
            <td class="mono tiny nowrap">{{ fmtDateTime(run.startedAt) }}</td>
            <td><span class="row-title">{{ getJob(run.jobId)?.name || run.jobId }}</span></td>
            <td class="col-trigger"><span class="pill" :class="run.trigger === 'manual' ? 'info' : 'mute'">{{ t(run.trigger) }}</span></td>
            <td><DiffValue v-if="run.oldIp || run.newIp" :old-value="run.oldIp || ''" :new-value="run.newIp || ''" /><span v-else class="muted tiny">—</span></td>
            <td><RunRecords v-if="run.details?.length" :details="run.details" summary /><StatusPill v-else :status="run.status" /></td>
            <td class="mono tiny col-duration">{{ fmtDur(run.durationMs) }}</td>
            <td class="muted"><span class="cell-detail tiny" :title="run.message">{{ run.message }}</span></td>
          </tr>
          <tr v-if="!list.length"><td colspan="7"><EmptyState icon="filter" title="No runs match" body="Adjust the filters above to see executions." /></td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
