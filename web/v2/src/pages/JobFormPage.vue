<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import TargetPicker from "../components/TargetPicker.vue";
import { Banner, Pill, Toggle } from "../components/UiKit";
import { defaultTarget, targetPlaceholder, targetPresets } from "../ipsourceTargets";
import { splitDomains, t } from "../lib";
import { api, bootState, data, fail, getJob, getProviderType, getSource, notify, refresh } from "../store";

const route = useRoute();
const router = useRouter();
const editId = computed(() => route.params.id ? String(route.params.id) : "");
const editing = computed(() => !!editId.value);
const original = computed(() => editId.value ? getJob(editId.value) : undefined);

const form = reactive({
  name: "",
  domains: "",
  family: "v4" as "v4" | "v6",
  providerId: "",
  sourcePickId: "",
  sourceType: "http",
  sourceTarget: "https://api.ipify.org",
  interval: Number(data.settings?.general?.defaultInterval) || 5,
  jitter: true,
  proxied: false,
  advanced: false
});

// Seed the form from the task's *editable* fields only, and only when they
// actually change: the panel refreshes itself in the background, and re-seeding
// on every poll (while lastRun/nextRun tick along) would wipe unsaved edits.
const seedSignature = computed(() => {
  const job = original.value;
  if (!job) return "";
  const source = getSource(job.ipSourceId);
  return JSON.stringify([job.name, job.targets, job.providerId, job.ipSourceId, job.intervalMinutes,
    job.jitter, source?.family, source?.type, source?.target]);
});

watch(seedSignature, () => {
  const job = original.value;
  if (!job) return;
  const source = getSource(job.ipSourceId);
  form.name = job.name;
  form.domains = job.targets.map((target) => target.domain).join("\n");
  form.family = source?.family || (job.targets[0]?.type === "AAAA" ? "v6" : "v4");
  form.providerId = job.providerId;
  form.sourcePickId = source?.id || "";
  form.sourceType = source?.type || "http";
  form.sourceTarget = source?.target || "";
  form.interval = job.intervalMinutes || 5;
  form.jitter = !!job.jitter;
  form.proxied = !!job.targets[0]?.proxied;
}, { immediate: true });

const pickedSource = computed(() => form.sourcePickId ? getSource(form.sourcePickId) : undefined);
const recordType = computed(() => form.family === "v6" ? "AAAA" : "A");
const domains = computed(() => splitDomains(form.domains));
const enabledProviders = computed(() => data.providers.filter((provider) => provider.enabled));
const selectedProvider = computed(() => data.providers.find((provider) => provider.id === form.providerId));
const selectedProviderType = computed(() => selectedProvider.value ? getProviderType(selectedProvider.value.type) : undefined);
const supportsProxied = computed(() => !!selectedProviderType.value?.supportsProxied);
const sourceUsers = computed(() => pickedSource.value ? data.jobs.filter((job) => job.ipSourceId === pickedSource.value?.id).length : 0);
const sourceTargetPresets = computed(() => targetPresets(form.sourceType || "http", form.family || "v4", data.interfaces));
const sourceTargetPlaceholder = computed(() => targetPlaceholder(form.sourceType || "http", form.family || "v4"));

watch(pickedSource, (source) => {
  if (source) form.family = source.family;
});

// Seed the orange-cloud default from the provider config when creating a task,
// but never overwrite the value an existing task already uses.
watch(() => form.providerId, (id) => {
  if (editing.value) return;
  const provider = data.providers.find((item) => item.id === id);
  const type = provider ? getProviderType(provider.type) : undefined;
  form.proxied = !!(type?.supportsProxied && provider?.config?.proxied);
});

// Switching kind makes the previous target meaningless, so seed the new default.
function onSourceTypeChange(): void {
  form.sourceTarget = defaultTarget(form.sourceType || "http", form.family || "v4", data.interfaces);
}

async function testSource(): Promise<void> {
  if (!pickedSource.value) return;
  try {
    if (bootState.value === "offline") {
      notify("ok", t("Resolved {ip}", { ip: pickedSource.value.family === "v6" ? "2001:db8::23" : "203.0.113.42" }), t("This is the address the task would use."));
    } else {
      const result = await api.post(`/ip-sources/${pickedSource.value.id}/test`);
      if (result.ok) notify("ok", t("Resolved {ip}", { ip: result.ip }), t("This is the address the task would use."));
      else notify("err", t("Could not resolve an address"), result.error || t("The source returned no usable address."));
    }
  } catch (error) { fail(error, t("Resolve failed")); }
}

async function save(): Promise<void> {
  if (!form.providerId) {
    notify("err", t("Pick a DNS provider"), t("Choose where these records should be written."));
    return;
  }
  if (!domains.value.length) {
    notify("err", t("Add at least one domain"), t("Enter one domain per line."));
    return;
  }
  const family = pickedSource.value?.family || form.family;
  const existingTargets = new Map((original.value?.targets || []).map((target) => [target.domain, target]));
  const targets = domains.value.map((domain) => {
    const existing = existingTargets.get(domain);
    return {
      domain,
      type: (family === "v6" ? "AAAA" : "A") as "AAAA" | "A",
      ttl: existing?.ttl || 300,
      proxied: supportsProxied.value ? form.proxied : false,
      comment: existing?.comment || ""
    };
  });
  const name = form.name || domains.value[0];
  try {
    let sourceId = pickedSource.value?.id;
    if (!sourceId) {
      if (bootState.value === "offline") {
        sourceId = `src-${Date.now()}`;
        data.ipSources.push({ id: sourceId, name: `${name} source`, type: form.sourceType, family, target: form.sourceTarget });
      } else {
        const source = await api.post("/ip-sources", { name: `${name} source`, type: form.sourceType, family, target: form.sourceTarget });
        sourceId = source.id;
      }
    }
    const payload = {
      name,
      providerId: form.providerId,
      ipSourceId: sourceId!,
      targets,
      intervalMinutes: Number(form.interval) || 5,
      runOnChangeOnly: original.value?.runOnChangeOnly ?? true,
      jitter: form.jitter,
      enabled: original.value?.enabled ?? true
    };
    if (bootState.value === "offline") {
      if (original.value) Object.assign(original.value, payload);
      else data.jobs.push({
        id: `j-${Date.now()}`,
        ...payload,
        lastRun: null,
        lastStatus: "not_run",
        nextRun: Date.now() + payload.intervalMinutes * 60000,
        current: "",
        desired: "",
        lastChanged: null
      });
    } else {
      if (editing.value) await api.patch(`/jobs/${editId.value}`, payload);
      else await api.post("/jobs", payload);
      await refresh(true);
    }
    notify("ok", t(editing.value ? "Task saved" : "Task created"), t("It will run on the next scheduler tick."));
    router.push(editing.value ? `/jobs/${editId.value}` : "/jobs");
  } catch (error) { fail(error, t(editing.value ? "Could not save task" : "Could not create task")); }
}
</script>

<template>
  <div class="crumb mb16"><RouterLink to="/jobs" class="muted">{{ t("Sync tasks") }}</RouterLink><span class="sep">/</span><b>{{ editing ? form.name : t("New task") }}</b></div>
  <div class="page-head">
    <div class="titles">
      <h1 class="page-title">{{ t(editing ? "Edit sync task" : "New sync task") }}</h1>
      <p class="page-sub">{{ t(editing
        ? "Change the provider, the records or the schedule. Every field opens with the value this task uses today."
        : "Pick an address family, a provider and a set of domains — OzDDNS keeps every record pointed at this host.") }}</p>
    </div>
  </div>

  <div class="card pad" style="max-width:760px">
    <div class="field"><label>{{ t("Task name") }}</label><input v-model="form.name" class="input" :placeholder="t('e.g. Home IPv4')"><span class="help">{{ t("Optional. Defaults to the first domain.") }}</span></div>
    <div class="row2">
      <div class="field">
        <label>{{ t("IP version") }}</label>
        <div class="seg" style="width:100%">
          <button type="button" :class="{ on: form.family === 'v4' }" style="flex:1" :disabled="!!pickedSource" @click="form.family = 'v4'">IPv4 · A</button>
          <button type="button" :class="{ on: form.family === 'v6' }" style="flex:1" :disabled="!!pickedSource" @click="form.family = 'v6'">IPv6 · AAAA</button>
        </div>
        <span class="help">{{ t(pickedSource ? "Set by the IP source family." : "Sets the record type for every domain below.") }}</span>
      </div>
      <div class="field">
        <label>{{ t("DNS provider") }} <span class="req">*</span></label>
        <select v-if="enabledProviders.length" v-model="form.providerId" class="input">
          <option value="" disabled>{{ t("Choose a provider…") }}</option>
          <option v-for="provider in enabledProviders" :key="provider.id" :value="provider.id">{{ provider.name }}</option>
        </select>
        <template v-else>
          <Banner kind="warn" title="No providers yet" body="Add a DNS provider first — right now OzDDNS has nowhere to write this record." />
          <div class="mt8"><RouterLink class="btn sm" to="/providers"><Icon name="plus" />{{ t("Add a provider") }}</RouterLink></div>
        </template>
      </div>
    </div>

    <div class="field">
      <label>{{ t("IP source") }} <span class="req">*</span></label>
      <select v-model="form.sourcePickId" class="input">
        <option value="">{{ t("Create a new source") }}</option>
        <option v-for="source in data.ipSources" :key="source.id" :value="source.id">{{ source.name }} · {{ source.family === "v6" ? "IPv6" : "IPv4" }} · {{ source.target }}</option>
      </select>
      <div v-if="pickedSource" class="callout">
        <div class="hstack wrap" style="gap:10px">
          <Icon name="globe" /><span class="strong">{{ pickedSource.name }}</span>
          <Pill :kind="pickedSource.family === 'v6' ? 'violet' : 'info'">{{ pickedSource.family.toUpperCase() }}</Pill>
          <span class="mono tiny muted">{{ pickedSource.target }}</span>
          <button type="button" class="btn sm" @click="testSource"><Icon name="refresh" />{{ t("Test") }}</button>
          <Pill v-if="sourceUsers" kind="mute">{{ t("{n} task(s) already use it.", { n: sourceUsers }) }}</Pill>
        </div>
      </div>
      <div v-else class="row2">
        <select v-model="form.sourceType" class="input" @change="onSourceTypeChange">
          <option v-for="type in data.ipSourceTypes" :key="type.id" :value="type.id">{{ t(type.label) }}</option>
        </select>
        <TargetPicker v-model="form.sourceTarget" :options="sourceTargetPresets" :placeholder="sourceTargetPlaceholder" />
      </div>
      <span class="help">{{ t(pickedSource ? "This task shares the saved source — nothing new is stored, and OzDDNS resolves it on every run." : "Returns the caller IP as plain text.") }}</span>
    </div>

    <div class="field"><label>{{ t("Domains") }} <span class="req">*</span></label><textarea v-model="form.domains" class="input mono" rows="4" :placeholder="`${t('One domain per line, e.g.')}\nhome.example.com\nnas.example.com`" /><span class="help">{{ t("One domain per line. Each becomes a {type} record.", { type: recordType }) }}<template v-if="domains.length"> · {{ t("{n} record(s)", { n: domains.length }) }}</template></span></div>

    <div v-if="supportsProxied" class="switch-row">
      <div class="txt">
        <div class="t">{{ t("Proxy through Cloudflare (orange cloud)") }}</div>
        <div class="d">{{ t("Proxied") }} · {{ t("Toggle orange-cloud proxying per record") }}</div>
      </div>
      <Toggle v-model="form.proxied" :label="t('Proxy through Cloudflare (orange cloud)')" />
    </div>

    <button type="button" class="btn sm mt16" @click="form.advanced = !form.advanced"><Icon :name="form.advanced ? 'chevron-down' : 'chevron-right'" />{{ t("Advanced options") }}</button>
    <template v-if="form.advanced">
      <div class="divider" />
      <div class="field"><label>{{ t("Interval (minutes)") }}</label><input v-model.number="form.interval" class="input mono" type="number"></div>
      <div class="switch-row"><div class="txt"><div class="t">{{ t("Add jitter") }}</div><div class="d">{{ t("Sprinkle runs ±10% to avoid thundering herds.") }}</div></div><Toggle v-model="form.jitter" :label="t('Add jitter')" /></div>
    </template>

    <div class="hstack between mt24">
      <button class="btn" @click="router.push(editing ? `/jobs/${editId}` : '/jobs')"><Icon name="arrow-left" />{{ t("Cancel") }}</button>
      <button class="btn primary" @click="save"><Icon name="check" />{{ t(editing ? "Save changes" : "Create task") }}</button>
    </div>
  </div>
</template>
