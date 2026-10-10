<script setup lang="ts">
import { computed } from "vue";
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { EmptyState, PageHead, Pill, ProviderLogo, ProviderStatus } from "../components/UiKit";
import { t } from "../lib";
import { data, getProviderType, modal, providerFilter } from "../store";

const router = useRouter();
const list = computed(() => data.providers.filter((provider) => {
  if (providerFilter.value === "all") return true;
  if (providerFilter.value === "ok") return provider.status === "ok" && provider.enabled;
  return provider.status === providerFilter.value;
}));

const segments = computed(() => [
  ["all", "All", data.providers.length],
  ["ok", "Connected", data.providers.filter((provider) => provider.status === "ok" && provider.enabled).length],
  ["warn", "Attention", data.providers.filter((provider) => provider.status === "warn").length],
  ["err", "Errors", data.providers.filter((provider) => provider.status === "err").length]
]);
</script>

<template>
  <PageHead
    eyebrow="Providers"
    title="DNS providers"
    sub="Accounts and credentials OzDDNS may write to. A provider is validated once and reused by any number of tasks."
  >
    <template #actions>
      <button class="btn primary" @click="modal = { type: 'provider-wizard', step: 0 }"><Icon name="plus" />{{ t("Add provider") }}</button>
    </template>
  </PageHead>

  <div class="between mb16">
    <div class="seg">
      <button
        v-for="segment in segments"
        :key="String(segment[0])"
        :class="{ on: providerFilter === segment[0] }"
        @click="providerFilter = String(segment[0])"
      >{{ t(String(segment[1])) }} · {{ segment[2] }}</button>
    </div>
    <span class="tiny muted">{{ t("Filter by state, or use the search box above to jump anywhere.") }}</span>
  </div>

  <div v-if="list.length" class="grid cols-3 reveal">
    <div
      v-for="provider in list"
      :key="provider.id"
      class="card pad"
      style="cursor:pointer;display:flex;flex-direction:column;gap:14px"
      @click="router.push(`/providers/${provider.id}`)"
    >
      <div class="hstack">
        <span class="pv" style="flex:1">
          <ProviderLogo :type="provider.type" big />
          <span><span class="pv-name" style="font-size:15px">{{ provider.name }}</span><br><span class="pv-type">{{ getProviderType(provider.type)?.label }}</span></span>
        </span>
        <ProviderStatus :provider="provider" />
      </div>
      <div class="hstack" style="gap:6px;flex-wrap:wrap"><Pill v-for="zone in provider.zones" :key="zone" kind="mute">{{ zone }}</Pill></div>
      <div class="hstack between" style="border-top:1px solid var(--line-2);padding-top:12px">
        <span class="tiny muted">{{ provider.statusText }}</span>
        <span class="hstack tiny muted" style="gap:6px"><Icon name="hash" />{{ t("{n} records", { n: provider.records }) }}</span>
      </div>
    </div>
  </div>

  <div v-else class="card">
    <EmptyState icon="server" title="No providers here" body="Nothing matches this filter. Add a provider to start writing DNS records.">
      <button class="btn primary" @click="modal = { type: 'provider-wizard', step: 0 }"><Icon name="plus" />{{ t("Add provider") }}</button>
    </EmptyState>
  </div>
</template>
