<script setup lang="ts">
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { Banner, PageHead, StatusPill, Toggle } from "../components/UiKit";
import { fmtIn, t } from "../lib";
import { data, fail, getProvider, getProviderType, runAll, runJob, toggleJob } from "../store";
import type { Job } from "../types";

const router = useRouter();

async function execute(job: Job, event: MouseEvent): Promise<void> {
  event.stopPropagation();
  try { await runJob(job); } catch (error) { fail(error, t("Run failed")); }
}

async function changeEnabled(job: Job, event?: Event): Promise<void> {
  event?.stopPropagation();
  try { await toggleJob(job); } catch (error) { fail(error); }
}
</script>

<template>
  <PageHead
    eyebrow="Sync"
    title="Sync tasks"
    sub="A task pairs an IP source with a provider and a set of records. On each tick OzDDNS diffs desired vs. actual and writes only what drifted."
  >
    <template #actions>
      <button class="btn" title="Reconcile every enabled task now — the schedule is unchanged" @click="runAll().catch(error => fail(error))"><Icon name="play" />{{ t("Run all") }}</button>
      <button class="btn primary" @click="router.push('/jobs/new')"><Icon name="plus" />{{ t("New task") }}</button>
    </template>
  </PageHead>

  <div v-if="data.jobs.some(job => job.lastStatus === 'failed')" class="mb16">
    <Banner kind="err" title="A task is failing" body="One or more tasks could not write. Open the failing task to see the provider response." />
  </div>

  <div class="card table-card-scroll">
    <table class="table clickable">
      <thead><tr><th style="width:26%">{{ t("Task") }}</th><th>{{ t("Record") }}</th><th>{{ t("Interval") }}</th><th>{{ t("Last result") }}</th><th>{{ t("Next run") }}</th><th /></tr></thead>
      <tbody>
        <tr v-for="job in data.jobs" :key="job.id" @click="router.push(`/jobs/${job.id}`)">
          <td>
            <div class="hstack" style="gap:10px">
              <Toggle :model-value="job.enabled" :label="t(job.enabled ? 'Disable task' : 'Enable task')" @click="changeEnabled(job, $event)" />
              <div style="min-width:0"><div class="row-title truncate">{{ job.name }}</div><div class="row-sub truncate" :title="job.name">{{ getProviderType(getProvider(job.providerId)?.type || "")?.label }}</div></div>
            </div>
          </td>
          <td>
            <div class="hstack" style="gap:8px">
              <span class="rtype" :class="job.targets[0]?.type">{{ job.targets[0]?.type }}</span>
              <div style="min-width:0"><div class="row-title mono truncate" style="font-size:13px">{{ job.targets[0]?.domain }}</div><div class="row-sub truncate" :title="job.current">{{ job.targets.length > 1 ? t("{n} more", { n: job.targets.length - 1 }) + " · " : "" }}{{ job.current || "—" }}</div></div>
            </div>
          </td>
          <td class="mono tiny">{{ t("{n} min", { n: job.intervalMinutes }) }}</td>
          <td><StatusPill :status="job.lastStatus" /></td>
          <td class="mono tiny">{{ job.enabled ? fmtIn(job.nextRun) : "—" }}</td>
          <td class="r"><div class="hstack" style="justify-content:flex-end;gap:6px">
            <button class="btn sm" @click="execute(job, $event)"><Icon name="play" />{{ t("Run") }}</button>
            <button class="icon-btn" :aria-label="t('Details')" @click.stop="router.push(`/jobs/${job.id}`)"><Icon name="chevron-right" /></button>
          </div></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
