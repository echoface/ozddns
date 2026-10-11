<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import Icon from "./Icon.vue";
import TargetPicker from "./TargetPicker.vue";
import {
  Banner,
  Pill,
  ProviderField,
  ProviderLogo,
  StatusPill
} from "./UiKit";
import { fmtDateTime, fmtDur, t } from "../lib";
import {
  api,
  bootState,
  data,
  fail,
  getJob,
  getProviderType,
  modal,
  notify,
  refresh,
  runJob,
  upsertSource
} from "../store";
import { defaultTarget, targetPlaceholder, targetPresets } from "../ipsourceTargets";

const form = reactive<Record<string, any>>({});
const saving = ref(false);

watch(modal, (value) => {
  Object.keys(form).forEach((key) => delete form[key]);
  saving.value = false;
  if (value?.type === "source") {
    if (value.source) {
      form.name = value.source.name;
      form.sourceType = value.source.type;
      form.family = value.source.family;
      form.target = value.source.target;
    } else {
      // Seed defaults so the Type / Family / Target controls start in sync.
      form.sourceType = "http";
      form.family = "v4";
      form.target = defaultTarget("http", "v4", data.interfaces);
    }
  }
  if (value?.type === "provider-wizard") value.step ||= 0;
}, { deep: false });

const runJobInfo = computed(() => modal.value?.type === "run" ? getJob(modal.value.run.jobId) : undefined);
const sourceUsers = computed(() => modal.value?.source
  ? data.jobs.filter((job) => job.ipSourceId === modal.value?.source?.id).length
  : 0);
const targetOptions = computed(() => targetPresets(form.sourceType || "http", form.family || "v4", data.interfaces));
const sourceTargetPlaceholder = computed(() => targetPlaceholder(form.sourceType || "http", form.family || "v4"));
const targetHelp = computed(() => {
  switch (form.sourceType) {
    case "interface":
      return targetOptions.value.length
        ? t("Pick one of this host's addresses, or type a NIC name (append #2 to use the second address of that family).")
        : t("No matching local addresses were found; type a NIC name manually.");
    case "command":
      return t("Run this shell command and use the first matching address it prints.");
    case "static":
      return t("A fixed literal address, e.g. {example}.", { example: form.family === "v6" ? "2001:db8::1" : "203.0.113.42" });
    default:
      return t("Pick a public endpoint, or paste any URL that returns the address as text.");
  }
});

// Switching kind makes the old target meaningless, so seed the new default.
function onSourceTypeChange(): void {
  form.target = defaultTarget(form.sourceType || "http", form.family || "v4", data.interfaces);
}

// Interface / static targets are family-scoped; re-seed them when it changes.
function onFamilyChange(): void {
  if (form.sourceType === "interface" || form.sourceType === "static") {
    form.target = defaultTarget(form.sourceType, form.family || "v4", data.interfaces);
  }
}

function close(): void {
  modal.value = null;
}

async function confirm(): Promise<void> {
  const action = modal.value?.action;
  close();
  if (action) {
    try { await action(); } catch (error) { fail(error); }
  }
}

async function saveSource(): Promise<void> {
  if (saving.value) return;
  const state = modal.value;
  saving.value = true;
  try {
    if (bootState.value === "offline") {
      if (state?.source) Object.assign(state.source, { name: form.name, type: form.sourceType, family: form.family, target: form.target });
      else upsertSource({
        id: `src-${Date.now()}`,
        name: form.name,
        type: form.sourceType,
        family: form.family,
        target: form.target,
        enabled: true
      });
    } else if (state?.source) {
      const updated = await api.patch(`/ip-sources/${state.source.id}`, {
        name: form.name, type: form.sourceType, family: form.family, target: form.target
      });
      upsertSource(updated);
    } else {
      const created = await api.post("/ip-sources", {
        name: form.name, type: form.sourceType || "http", family: form.family || "v4", target: form.target
      });
      upsertSource(created);
    }
    notify("ok", t(state?.source ? "IP source updated" : "IP source added"), form.name || "");
    close();
  } catch (error) {
    fail(error, t(state?.source ? "Could not update source" : "Could not add source"));
  } finally {
    saving.value = false;
  }
}

async function addNotification(): Promise<void> {
  try {
    const list = [...(data.settings.raw?.notifications || [])];
    list.push({
      id: `n-${Date.now().toString(36)}`,
      kind: form.kind || "webhook",
      name: form.name || "Webhook",
      target: form.target || "",
      enabled: true,
      on: [form.on || "failure"]
    });
    if (bootState.value === "offline") data.settings.notify.webhooks = list;
    else {
      await api.patch("/settings", { notifications: list });
      await refresh(true);
    }
    notify("ok", t("Channel added"), form.name || "Webhook");
    close();
  } catch (error) { fail(error, t("Could not add channel")); }
}

async function addProvider(): Promise<void> {
  const state = modal.value;
  const type = getProviderType(state?.typeId);
  if (!type) return;
  const config: Record<string, unknown> = {};
  type.fields.forEach((field: any) => {
    const value = form[`provider_${field.key}`];
    if (value !== "" && value != null) config[field.key] = value === "true" ? true : value === "false" ? false : value;
  });
  try {
    if (bootState.value === "offline") {
      data.providers.push({
        id: `p-${Date.now().toString(36)}`,
        name: form.providerName || type.label,
        type: type.id,
        enabled: true,
        status: "ok",
        statusText: "Validated just now",
        zones: ["example.com"],
        records: 0,
        config,
        lastValidated: Date.now(),
        createdAt: Date.now()
      });
    } else {
      await api.post("/providers", { name: form.providerName || type.label, type: type.id, config });
      await refresh(true);
    }
    notify("ok", t("Provider added"), t("{label} validated and ready.", { label: type.label }));
    close();
  } catch (error) { fail(error, t("Could not add provider")); }
}

async function rerun(): Promise<void> {
  const run = modal.value?.run;
  const job = run ? getJob(run.jobId) : undefined;
  close();
  if (!job) return;
  try { await runJob(job); } catch (error) { fail(error, t("Run failed")); }
}
</script>

<template>
  <template v-if="modal">
    <div class="scrim" @click="close" />
    <div class="modal" role="dialog" aria-modal="true">
      <div v-if="modal.type === 'confirm'" class="sheet" style="width:min(440px,100%)">
        <div class="sheet-head">
          <span class="logo lg" style="color:var(--rust);background:var(--rust-050)"><Icon name="alert-triangle" /></span>
          <div>
            <div class="h">{{ modal.title }}</div>
            <div class="s">{{ modal.body }}</div>
          </div>
        </div>
        <div class="sheet-foot">
          <button class="btn" @click="close">{{ t("Cancel") }}</button>
          <button class="btn" :class="modal.danger ? 'danger' : 'primary'" @click="confirm">{{ modal.confirm }}</button>
        </div>
      </div>

      <div v-else-if="modal.type === 'run'" class="sheet wide">
        <div class="sheet-head">
          <span class="logo lg" style="color:var(--slate);background:var(--slate-050)"><Icon name="activity" /></span>
          <div class="grow">
            <div class="h">{{ t("Execution {id}", { id: modal.run.id }) }}</div>
            <div class="s">{{ runJobInfo?.name || modal.run.jobId }} · {{ fmtDateTime(modal.run.startedAt) }} · {{ t(modal.run.trigger) }}</div>
          </div>
          <StatusPill :status="modal.run.status" />
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <div class="grid cols-3 mb16">
            <div class="card stat">
              <span class="lab"><Icon name="arrow-left" />{{ t("Old value") }}</span>
              <span class="val"><span class="mono" style="font-size:15px">{{ modal.run.oldIp || "—" }}</span></span>
            </div>
            <div class="card stat">
              <span class="lab"><Icon name="arrow-right" />{{ t("New value") }}</span>
              <span class="val"><span class="mono" style="font-size:15px">{{ modal.run.newIp || "—" }}</span></span>
            </div>
            <div class="card stat">
              <span class="lab"><Icon name="clock" />{{ t("Duration") }}</span>
              <span class="val">{{ fmtDur(modal.run.durationMs) }}</span>
            </div>
          </div>
          <div class="strong mb8">{{ t("Steps") }}</div>
          <div class="timeline mb16">
            <div
              v-for="(step, index) in [
                [t('Read IP source'), modal.run.newIp ? `resolved ${modal.run.newIp}` : t('no address available')],
                [t('Fetch current record'), modal.run.oldIp ? `found ${modal.run.oldIp}` : t('no existing record')],
                [t('Compute diff'), modal.run.oldIp === modal.run.newIp ? t('in sync — no write') : t('drift detected')],
                [t(modal.run.status === 'failed' ? 'Write rejected' : modal.run.status === 'success' ? 'Write applied' : 'Reconcile'), modal.run.message || t('done')]
              ]"
              :key="index"
              class="tl-item"
            >
              <div class="tl-rail">
                <span class="tl-node" :class="index === 3 ? (modal.run.status === 'failed' ? 'err' : modal.run.status === 'success' ? 'ok' : 'info') : 'info'" />
                <span v-if="index < 3" class="tl-line" />
              </div>
              <div class="tl-body">
                <div class="tl-text"><b>{{ step[0] }}</b></div>
                <div class="tl-text muted">{{ step[1] }}</div>
              </div>
            </div>
          </div>
          <Banner v-if="modal.run.status === 'failed'" kind="err" title="Provider rejected the request" :body="modal.run.message" />
        </div>
        <div class="sheet-foot">
          <button class="btn" @click="rerun"><Icon name="refresh" />{{ t("Re-run") }}</button>
          <button class="btn primary" @click="close">{{ t("Close") }}</button>
        </div>
      </div>

      <div v-else-if="modal.type === 'dry-run'" class="sheet wide">
        <div class="sheet-head">
          <span class="logo lg" style="color:var(--slate);background:var(--slate-050)"><Icon name="eye" /></span>
          <div class="grow">
            <div class="h">{{ t("Reconciliation preview") }}</div>
            <div class="s">{{ modal.job.name }} · {{ t("No provider changes were made.") }}</div>
          </div>
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>{{ t("Record") }}</th><th>{{ t("Current") }}</th><th>{{ t("Desired") }}</th><th>{{ t("Action") }}</th></tr></thead>
              <tbody><tr v-for="target in modal.diff.targets" :key="`${target.domain}-${target.type}`">
                <td><div class="hstack" style="gap:8px"><span class="rtype" :class="target.type">{{ target.type }}</span><span class="mono tiny">{{ target.domain }}</span></div></td>
                <td class="mono tiny">{{ target.current || "—" }}</td>
                <td class="mono tiny">{{ target.desired || "—" }}</td>
                <td><Pill :kind="target.action === 'none' ? 'ok' : target.action === 'skip' ? 'warn' : 'info'">{{ t(target.action) }}</Pill></td>
              </tr></tbody>
            </table>
          </div>
        </div>
        <div class="sheet-foot"><button class="btn primary" @click="close">{{ t("Close") }}</button></div>
      </div>

      <div v-else-if="modal.type === 'provider-wizard' && !modal.step" class="sheet wide">
        <div class="sheet-head">
          <div class="grow">
            <div class="h">{{ t("Add a provider") }}</div>
            <div class="s">{{ t("Choose the DNS provider you want OzDDNS to write to.") }}</div>
          </div>
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <div class="pick-grid">
            <button
              v-for="type in data.providerTypes"
              :key="type.id"
              class="pick"
              @click="modal.typeId = type.id; modal.step = 1"
            >
              <ProviderLogo :type="type.id" big />
              <span><span class="t">{{ t(type.label) }}</span><span class="d">{{ t(type.desc) }}</span></span>
            </button>
          </div>
        </div>
      </div>

      <div v-else-if="modal.type === 'provider-wizard'" class="sheet">
        <div class="sheet-head">
          <ProviderLogo :type="modal.typeId" big />
          <div class="grow">
            <div class="h">{{ getProviderType(modal.typeId)?.label }}</div>
            <div class="s">{{ t(getProviderType(modal.typeId)?.desc || "") }}</div>
          </div>
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <div class="field">
            <label>{{ t("Display name") }} <span class="req">*</span></label>
            <input v-model="form.providerName" class="input" :placeholder="t('{label} (production)', { label: getProviderType(modal.typeId)?.label })">
          </div>
          <ProviderField
            v-for="field in getProviderType(modal.typeId)?.fields || []"
            :key="field.key"
            v-model="form[`provider_${field.key}`]"
            :field="field"
          />
          <Banner kind="info" title="Credentials are sensitive" body="Values are masked in the UI and API responses. Protect the database and exported configuration files because provider credentials are stored in them." />
        </div>
        <div class="sheet-foot">
          <button class="btn" @click="modal.step = 0"><Icon name="arrow-left" />{{ t("Back") }}</button>
          <button class="btn primary" @click="addProvider"><Icon name="check" />{{ t("Add & validate") }}</button>
        </div>
      </div>

      <div v-else-if="modal.type === 'source'" class="sheet" style="width:min(520px,100%)">
        <div class="sheet-head">
          <span class="logo lg" style="color:var(--slate);background:var(--slate-050)"><Icon name="globe" /></span>
          <div class="grow">
            <div class="h">{{ t(modal.source ? "Edit IP source" : "Add an IP source") }}</div>
            <div class="s">{{ t(modal.source ? "Changes apply to every task that uses this source." : "Tell OzDDNS where this host's address comes from.") }}</div>
          </div>
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <Banner
            v-if="sourceUsers"
            kind="warn"
            :title="t('Shared with {n} task(s)', { n: sourceUsers })"
            body="They read this source on their next run, so the change lands immediately. The address family is locked while tasks reference it."
          />
          <div class="field"><label>{{ t("Name") }} <span class="req">*</span></label><input v-model="form.name" class="input" placeholder="ipify (IPv4)"></div>
          <div class="field"><label>{{ t("Type") }}</label><select v-model="form.sourceType" class="input" @change="onSourceTypeChange"><option v-for="type in data.ipSourceTypes" :key="type.id" :value="type.id">{{ t(type.label) }}</option></select></div>
          <div class="field"><label>{{ t("Family") }}</label><select v-model="form.family" class="input" :disabled="sourceUsers > 0" @change="onFamilyChange"><option value="v4">IPv4</option><option value="v6">IPv6</option></select></div>
          <div class="field">
            <label>{{ t("Target") }} <span class="req">*</span></label>
            <TargetPicker v-model="form.target" :options="targetOptions" :placeholder="sourceTargetPlaceholder" />
            <span class="help">{{ targetHelp }}</span>
          </div>
        </div>
        <div class="sheet-foot">
          <button class="btn" :disabled="saving" @click="close">{{ t("Cancel") }}</button>
          <button class="btn primary" :disabled="saving" @click="saveSource"><Icon :class="saving ? 'spin' : ''" :name="saving ? 'loader' : 'check'" />{{ t(saving ? "Saving…" : (modal.source ? "Save source" : "Add source")) }}</button>
        </div>
      </div>

      <div v-else-if="modal.type === 'notify'" class="sheet" style="width:min(520px,100%)">
        <div class="sheet-head">
          <span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)"><Icon name="bell" /></span>
          <div class="grow"><div class="h">{{ t("Add a notification channel") }}</div><div class="s">{{ t("OzDDNS POSTs a JSON payload on the events you choose.") }}</div></div>
          <button class="icon-btn" :aria-label="t('Close')" @click="close"><Icon name="x" /></button>
        </div>
        <div class="sheet-body">
          <div class="field"><label>{{ t("Name") }}</label><input v-model="form.name" class="input" placeholder="#homelab alerts"></div>
          <div class="field"><label>{{ t("Kind") }}</label><select v-model="form.kind" class="input"><option v-for="kind in ['slack','discord','ntfy','gotify','webhook']" :key="kind">{{ kind }}</option></select></div>
          <div class="field"><label>{{ t("Webhook URL") }}</label><input v-model="form.target" class="input mono" placeholder="https://hooks.slack.com/services/…"></div>
          <div class="field"><label>{{ t("Notify on") }}</label><select v-model="form.on" class="input"><option v-for="kind in ['change','failure','success','all']" :key="kind">{{ kind }}</option></select></div>
        </div>
        <div class="sheet-foot">
          <button class="btn" @click="close">{{ t("Cancel") }}</button>
          <button class="btn primary" @click="addNotification"><Icon name="check" />{{ t("Add channel") }}</button>
        </div>
      </div>

    </div>
  </template>
</template>
