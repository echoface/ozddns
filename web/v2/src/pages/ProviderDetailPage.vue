<script setup lang="ts">
import { computed, reactive } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import {
  Banner,
  EmptyState,
  PageHead,
  Pill,
  ProviderField,
  ProviderLogo,
  ProviderStatus,
  StatusPill,
  Toggle
} from "../components/UiKit";
import { fmtAgo, fmtDateTime, t } from "../lib";
import {
  api,
  bootState,
  confirmModal,
  data,
  deleteProvider,
  fail,
  getProvider,
  getProviderType,
  notify,
  refresh,
  toggleProvider,
  validateProvider
} from "../store";

const route = useRoute();
const router = useRouter();
const provider = computed(() => getProvider(String(route.params.id)));
const providerType = computed(() => provider.value ? getProviderType(provider.value.type) : null);
const tab = computed(() => String(route.params.tab || "overview"));
const usingJobs = computed(() => data.jobs.filter((job) => job.providerId === provider.value?.id));
const config = reactive<Record<string, any>>({});

function fieldValue(key: string): any {
  if (config[key] !== undefined) return config[key];
  const stored = provider.value?.config[key];
  if (typeof stored === "string" && stored.includes("••")) return "";
  if (stored !== undefined && stored !== null && stored !== "") return stored;
  // Nothing stored yet (or nothing meaningful): show the field's designed
  // default so the form reflects what will actually be saved.
  const field = providerType.value?.fields?.find((item: any) => item.key === key);
  return field?.default ?? "";
}

function setField(key: string, value: any): void {
  config[key] = value;
}

async function save(): Promise<void> {
  if (!provider.value || !providerType.value) return;
  const next: Record<string, unknown> = {};
  providerType.value.fields.forEach((field: any) => {
    // Submit the effective value (typed value, else stored value, else the
    // field default). Blank means "unchanged" for secrets and "clear" for
    // everything else, so blanks are skipped rather than persisted.
    const value = fieldValue(field.key);
    if (value !== undefined && value !== null && value !== "") next[field.key] = value === "true" ? true : value === "false" ? false : value;
  });
  try {
    if (bootState.value === "offline") Object.assign(provider.value.config, next);
    else {
      await api.patch(`/providers/${provider.value.id}`, { name: provider.value.name, config: next });
      await refresh(true);
    }
    notify("ok", t("Saved"), t("{name} configuration updated.", { name: provider.value.name }));
  } catch (error) { fail(error, t("Save failed")); }
}

function remove(): void {
  if (!provider.value) return;
  const value = provider.value;
  confirmModal(
    t("Delete provider?"),
    t('"{name}" will be removed. Jobs that reference it must be reassigned.', { name: value.name }),
    t("Delete provider"),
    () => deleteProvider(value)
  );
}
</script>

<template>
  <template v-if="provider">
    <div class="crumb mb16"><RouterLink to="/providers" class="muted">{{ t("Providers") }}</RouterLink><span class="sep">/</span><b>{{ provider.name }}</b></div>
    <div class="page-head">
      <div class="hstack" style="gap:16px;flex:1">
        <ProviderLogo :type="provider.type" big />
        <div>
          <h1 class="page-title">{{ provider.name }}</h1>
          <div class="hstack mt8" style="gap:8px"><ProviderStatus :provider="provider" /><span class="tiny muted">{{ providerType?.label }} · {{ provider.statusText }}</span></div>
        </div>
      </div>
      <div class="page-actions">
        <button class="btn" @click="validateProvider(provider).catch(error => fail(error, t('Validation failed')))"><Icon name="shield-check" />{{ t("Validate") }}</button>
        <button class="btn primary" @click="router.push(`/providers/${provider.id}/configuration`)"><Icon name="pencil" />{{ t("Edit") }}</button>
      </div>
    </div>

    <div v-if="provider.status === 'err'" class="mb16"><Banner kind="err" title="This provider is failing" :body="`${provider.statusText} ${t('Re-enter credentials or re-issue a token, then validate again.')}`" /></div>
    <div v-if="provider.status === 'warn'" class="mb16"><Banner kind="warn" title="Heads up" :body="`${provider.statusText} ${t('Rotate the credential before it expires to avoid interrupted updates.')}`" /></div>

    <div class="tabs mb16">
      <button :class="{ on: tab === 'overview' }" @click="router.push(`/providers/${provider.id}`)">{{ t("Overview") }}</button>
      <button :class="{ on: tab === 'configuration' }" @click="router.push(`/providers/${provider.id}/configuration`)">{{ t("Configuration") }}</button>
      <button :class="{ on: tab === 'usage' }" @click="router.push(`/providers/${provider.id}/usage`)">{{ t("Used by ({n})", { n: usingJobs.length }) }}</button>
    </div>

    <div v-if="tab === 'configuration'" class="grid cols-2">
      <div class="card pad">
        <ProviderField
          v-for="field in providerType?.fields || []"
          :key="field.key"
          :field="field"
          :model-value="fieldValue(field.key)"
          :saved-value="provider.config[field.key]"
          @update:model-value="setField(field.key, $event)"
        />
        <div class="hstack mt8">
          <button class="btn primary" @click="save"><Icon name="save" />{{ t("Save changes") }}</button>
          <button class="btn" @click="validateProvider(provider).catch(error => fail(error, t('Validation failed')))"><Icon name="shield-check" />{{ t("Test connection") }}</button>
        </div>
      </div>
      <div class="vstack">
        <div class="card pad"><Banner kind="info" title="Credentials are sensitive" body="Values are masked in the UI and API responses. Protect the database and exported configuration files because provider credentials are stored in them." /></div>
        <div class="card pad">
          <div class="strong mb8">{{ t("Provider capability") }}</div>
          <div class="switch-row"><div class="txt"><div class="t">{{ t("Proxied records") }}</div><div class="d">{{ t("Toggle orange-cloud proxying per record") }}</div></div><Pill :kind="providerType?.supportsProxied ? 'ok' : 'mute'">{{ t(providerType?.supportsProxied ? "Supported" : "N/A") }}</Pill></div>
          <div class="switch-row"><div class="txt"><div class="t">{{ t("IPv6 (AAAA)") }}</div><div class="d">{{ t("Write AAAA records") }}</div></div><Pill :kind="providerType?.tag === 'godaddy' ? 'warn' : 'ok'">{{ t(providerType?.tag === "godaddy" ? "Limited" : "Supported") }}</Pill></div>
        </div>
      </div>
    </div>

    <div v-else-if="tab === 'usage'">
      <div v-if="usingJobs.length" class="card">
        <table class="table clickable">
          <thead><tr><th>{{ t("Task") }}</th><th>{{ t("Record") }}</th><th>{{ t("Interval") }}</th><th>{{ t("Status") }}</th></tr></thead>
          <tbody><tr v-for="job in usingJobs" :key="job.id" @click="router.push(`/jobs/${job.id}`)">
            <td class="row-title">{{ job.name }}</td>
            <td><div class="hstack" style="gap:8px"><span class="rtype" :class="job.targets[0]?.type">{{ job.targets[0]?.type }}</span><div><div class="row-title mono" style="font-size:13px">{{ job.targets[0]?.domain }}</div><div class="row-sub">{{ job.current || "—" }}</div></div></div></td>
            <td class="mono tiny">{{ t("{n} min", { n: job.intervalMinutes }) }}</td>
            <td><StatusPill :status="job.lastStatus" /></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="card"><EmptyState icon="jobs" title="Not used yet" body="No task writes through this provider. Create a task to start syncing." /></div>
    </div>

    <div v-else class="grid cols-2">
      <div class="card">
        <div class="card-head"><span class="h">{{ t("Zones") }}</span></div>
        <table class="table"><thead><tr><th>{{ t("Zone") }}</th><th>{{ t("Records") }}</th><th>{{ t("Visibility") }}</th></tr></thead>
          <tbody>
            <tr v-for="zone in provider.zones" :key="zone"><td class="mono">{{ zone }}</td><td class="num">{{ Math.ceil(provider.records / provider.zones.length) }}</td><td><Pill kind="ok">{{ t("Read / Write") }}</Pill></td></tr>
            <tr v-if="!provider.zones.length"><td colspan="3" class="muted">{{ t(providerType?.writeOnly ? "Zones cannot be read back from this provider." : "No zones discovered.") }}</td></tr>
          </tbody>
        </table>
      </div>
      <div class="card pad">
        <div class="strong mb12">{{ t("Details") }}</div>
        <div v-for="[key,value] in [
          [t('Provider type'), providerType?.label],
          [t('Created'), fmtDateTime(provider.createdAt)],
          [t('Last validated'), fmtAgo(provider.lastValidated)],
          [t('Records tracked'), providerType?.writeOnly ? '—' : String(provider.records)]
        ]" :key="String(key)" class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">{{ key }}</span><span class="strong" style="font-size:13px">{{ value }}</span></div>
        <div class="divider" />
        <div class="switch-row">
          <div class="txt">
            <div class="t">{{ t("Provider enabled") }}</div>
            <div class="d">{{ t("Every task that writes through this provider is skipped while it is disabled.") }}</div>
          </div>
          <Toggle :model-value="provider.enabled" :label="t('Provider enabled')" @click="toggleProvider(provider).catch(error => fail(error, t('Save failed')))" />
        </div>
        <div class="divider" />
        <button class="btn danger block" @click="remove"><Icon name="trash" />{{ t("Delete provider") }}</button>
      </div>
    </div>
  </template>
  <div v-else class="card"><EmptyState icon="alert-triangle" title="Provider not found" body="The thing you were looking for does not exist (or was deleted)."><RouterLink class="btn primary" to="/overview">{{ t("Back to overview") }}</RouterLink></EmptyState></div>
</template>
