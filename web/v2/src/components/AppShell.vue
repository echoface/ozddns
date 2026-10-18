<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import Icon from "./Icon.vue";
import { Toggle } from "./UiKit";
import { fmtDateTime, fmtUptime, isDark, localeVersion, t, toggleTheme } from "../lib";
import {
  authState,
  data,
  getJob,
  getProvider,
  getProviderType,
  modal,
  navOpen,
  saveThemePreference,
  schedulerRunning,
  signOut,
  toggleScheduler
} from "../store";

const route = useRoute();
const router = useRouter();
const search = ref("");
const selected = ref(0);
const searchFocused = ref(false);

const nav = [
  { key: "overview", label: "Overview", icon: "gauge", path: "/overview" },
  { key: "providers", label: "Providers", icon: "server", path: "/providers" },
  { key: "jobs", label: "Sync tasks", icon: "list-checks", path: "/jobs" },
  { key: "executions", label: "Executions", icon: "activity", path: "/executions" },
  { key: "settings", label: "Settings", icon: "settings", path: "/settings" }
];

const activeKey = computed(() => route.path.split("/")[1] || "overview");
const failingCount = computed(() => data.jobs.filter((job) => job.lastStatus === "failed").length);
const crumbSection = computed(() => t(String(route.meta.section || "")));
const crumbTitle = computed(() => {
  if (route.path.startsWith("/providers/")) return getProvider(String(route.params.id))?.name || t("Provider");
  if (route.path.startsWith("/jobs/") && route.params.id) return getJob(String(route.params.id))?.name || t(String(route.meta.title || ""));
  return t(String(route.meta.title || ""));
});

const searchIndex = computed(() => {
  localeVersion.value;
  const items: any[] = [];
  data.jobs.forEach((job) => {
    const domains = job.targets.map((target) => target.domain).join(", ");
    const provider = getProvider(job.providerId);
    items.push({ kind: "Task", icon: "jobs", label: job.name, sub: domains || provider?.name || "", path: `/jobs/${job.id}` });
  });
  data.providers.forEach((provider) => {
    const providerType = getProviderType(provider.type);
    items.push({
      kind: "Provider",
      icon: "server",
      label: provider.name,
      sub: `${providerType?.label || provider.type}${provider.zones.length ? ` · ${provider.zones.join(", ")}` : ""}`,
      path: `/providers/${provider.id}`
    });
  });
  data.ipSources.forEach((source) => {
    items.push({ kind: "IP source", icon: "globe", label: source.name, sub: source.target, path: "/settings/sources" });
  });
  data.runs.slice(0, 30).forEach((run) => {
    const job = getJob(run.jobId);
    items.push({
      kind: "Execution",
      icon: "activity",
      label: `${job?.name || run.jobId} · ${fmtDateTime(run.startedAt)}`,
      sub: run.message || (run.oldIp && run.newIp && run.oldIp !== run.newIp ? `${run.oldIp} → ${run.newIp}` : ""),
      run
    });
  });
  nav.forEach((item) => items.push({ kind: "Page", icon: item.icon, label: t(item.label), sub: "", path: item.path }));
  return items;
});

const hits = computed(() => {
  const query = search.value.trim().toLowerCase();
  if (!query) return [];
  return searchIndex.value.filter((item) =>
    `${item.label} ${item.sub} ${item.kind}`.toLowerCase().includes(query)
  ).slice(0, 8);
});

function openHit(item: any): void {
  search.value = "";
  searchFocused.value = false;
  selected.value = 0;
  if (item.run) modal.value = { type: "run", run: item.run };
  else router.push(item.path);
}

function onSearchKey(event: KeyboardEvent): void {
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    if (hits.value.length) {
      selected.value = (selected.value + (event.key === "ArrowDown" ? 1 : hits.value.length - 1)) % hits.value.length;
    }
  } else if (event.key === "Enter" && hits.value[selected.value]) {
    event.preventDefault();
    openHit(hits.value[selected.value]);
  } else if (event.key === "Escape") {
    searchFocused.value = false;
    (event.target as HTMLInputElement).blur();
  }
}

function deferSearchBlur(): void {
  window.setTimeout(() => { searchFocused.value = false; }, 120);
}

async function switchTheme(): Promise<void> {
  toggleTheme();
  await saveThemePreference();
}

async function schedulerToggle(): Promise<void> {
  try { await toggleScheduler(); } catch { /* toast is handled by callers where needed */ }
}

function globalKey(event: KeyboardEvent): void {
  const active = document.activeElement?.tagName;
  if (event.key === "/" && active !== "INPUT" && active !== "TEXTAREA") {
    event.preventDefault();
    document.querySelector<HTMLInputElement>("#globalSearch")?.focus();
  }
  if (event.key === "Escape" && navOpen.value) navOpen.value = false;
}

onMounted(() => window.addEventListener("keydown", globalKey));
onBeforeUnmount(() => window.removeEventListener("keydown", globalKey));

watch(() => route.fullPath, async () => {
  await nextTick();
  window.OZ.i18n.applyDOM(document.querySelector("#root") || document);
});
</script>

<template>
  <div class="app" :class="{ 'nav-open': navOpen }">
    <aside class="sidebar">
      <div class="brand">
        <span class="mark">O</span>
        <span>
          <span class="name">Oz<span>DDNS</span></span>
          <span class="sub">dynamic DNS</span>
        </span>
      </div>

      <div class="nav-group">
        <RouterLink
          v-for="item in nav"
          :key="item.key"
          class="nav-item"
          :class="{ active: activeKey === item.key }"
          :to="item.path"
        >
          <Icon :name="item.icon" />
          <span>{{ t(item.label) }}</span>
          <span
            v-if="item.key === 'providers'"
            class="count"
          >{{ data.providers.length }}</span>
          <span
            v-else-if="item.key === 'jobs'"
            class="count"
            :class="{ warn: failingCount }"
          >{{ failingCount || data.jobs.length }}</span>
        </RouterLink>
      </div>

      <div class="sidebar-foot">
        <div class="sched">
          <div class="row">
            <Icon :name="schedulerRunning ? 'zap' : 'pause'" />
            <span class="big">{{ t("Scheduler") }}</span>
            <span class="spacer grow" />
            <Toggle
              :model-value="schedulerRunning"
              :label="t('Scheduler')"
              @click="schedulerToggle"
            />
          </div>
          <div class="meta">
            {{ schedulerRunning
              ? t("Running · uptime {u}", { u: fmtUptime(data.runtime.uptimeSeconds) })
              : t("Paused — no automatic runs") }}
          </div>
        </div>
        <RouterLink class="who" to="/settings/about">
          <span class="avatar">{{ authState.enabled ? (authState.username || "AD").slice(0, 2).toUpperCase() : "LO" }}</span>
          <span class="grow">
            <span class="strong" style="font-size:13px">{{ t(authState.enabled ? "Administrator" : "Local open mode") }}</span><br>
            <span class="tiny muted">{{ authState.enabled ? authState.username : data.runtime.version }}</span>
          </span>
          <Icon name="chevron-right" class="muted" />
        </RouterLink>
        <button v-if="authState.enabled" class="btn sm block mt8" type="button" @click="signOut"><Icon name="log-out" />{{ t("Sign out") }}</button>
      </div>
    </aside>

    <div
      class="nav-scrim"
      aria-hidden="true"
      @click="navOpen = false"
    />

    <main class="main">
      <header class="topbar">
        <button
          class="icon-btn mobile-toggle"
          :aria-label="t('Open navigation')"
          @click="navOpen = !navOpen"
        >
          <Icon name="list" />
        </button>
        <div class="crumb">
          <span>{{ crumbSection }}</span>
          <span class="sep">/</span>
          <b>{{ crumbTitle }}</b>
        </div>
        <span class="spacer" />
        <div class="searchbox">
          <span class="ico" style="display:grid;width:16px;height:16px"><Icon name="search" /></span>
          <input
            id="globalSearch"
            v-model="search"
            autocomplete="off"
            spellcheck="false"
            :placeholder="t('Search tasks, domains, providers, sources…')"
            @focus="searchFocused = true"
            @blur="deferSearchBlur"
            @keydown="onSearchKey"
          >
          <kbd>/</kbd>
          <div
            v-if="searchFocused && search.trim()"
            class="search-panel"
          >
            <button
              v-for="(item, index) in hits"
              :key="`${item.kind}-${item.label}-${index}`"
              type="button"
              class="search-hit"
              :class="{ on: selected === index }"
              @mousedown.prevent="openHit(item)"
            >
              <span class="ico"><Icon :name="item.icon" /></span>
              <span class="grow">
                <span class="t">{{ item.label }}</span>
                <span v-if="item.sub" class="d">{{ item.sub }}</span>
              </span>
              <span class="pill mute">{{ t(item.kind) }}</span>
            </button>
            <div v-if="!hits.length" class="search-none">
              {{ t("Nothing matches “{q}”.", { q: search }) }}
            </div>
          </div>
        </div>
        <button
          class="icon-btn theme-toggle"
          :title="t('Theme')"
          :aria-label="t('Theme')"
          @click="switchTheme"
        >
          <Icon :name="isDark ? 'sun' : 'moon'" />
        </button>
      </header>
      <div class="content">
        <slot />
      </div>
    </main>
  </div>
</template>
