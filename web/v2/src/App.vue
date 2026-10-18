<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import AppShell from "./components/AppShell.vue";
import ModalHost from "./components/ModalHost.vue";
import Icon from "./components/Icon.vue";
import { authState, bootState, data, dismissToast, offlineReason, poll, retryBoot, staleSince, toasts } from "./store";
import { fmtAgo, localeVersion, t } from "./lib";

const route = useRoute();
const useShell = computed(() => route.meta.shell !== false);
// Scheduled runs happen in the background, so the panel re-reads state on its
// own: once per navigation and on a slow timer while the tab is visible. Every
// fetch goes through the same bootstrap payload the app boots with, so there is
// one source of truth to keep in sync.
const POLL_MS = 15000;
let timer: number | undefined;

function refreshSoon(): void {
  void poll();
}

async function translateRenderedView(): Promise<void> {
  localeVersion.value;
  await nextTick();
  const root = document.querySelector("#root");
  if (root) window.OZ.i18n.applyDOM(root);
}

watch([() => route.fullPath, localeVersion, bootState], translateRenderedView);
watch(() => route.fullPath, refreshSoon);
onMounted(() => {
  translateRenderedView();
  timer = window.setInterval(refreshSoon, POLL_MS);
  document.addEventListener("visibilitychange", onVisible);
});
onBeforeUnmount(() => {
  if (timer) window.clearInterval(timer);
  document.removeEventListener("visibilitychange", onVisible);
});

function onVisible(): void {
  if (!document.hidden) refreshSoon();
}
</script>

<template>
  <div v-if="bootState === 'loading'" class="app" aria-busy="true">
    <aside class="sidebar">
      <div class="brand">
        <span class="mark">O</span>
        <span><span class="name">Oz<span>DDNS</span></span><span class="sub">dynamic DNS</span></span>
      </div>
      <div class="nav-group">
        <div v-for="width in ['96px','128px','104px','112px','84px']" :key="width" class="nav-item sk-nav">
          <span class="sk" style="width:18px;height:18px;border-radius:5px" />
          <span class="sk" :style="{ width, height: '12px' }" />
        </div>
      </div>
      <div class="sidebar-foot">
        <div class="sched">
          <div class="row"><span class="sk" style="width:20px;height:20px" /><span class="sk" style="width:72px;height:12px" /></div>
          <div class="mt12"><span class="sk" style="width:100%;height:10px" /></div>
        </div>
      </div>
    </aside>
    <main class="main">
      <header class="topbar"><span class="sk" style="width:150px;height:14px" /><span class="spacer" /><span class="sk" style="width:224px;height:34px;border-radius:999px" /></header>
      <div class="content">
        <div class="mb24"><span class="sk" style="width:120px;height:12px" /><div class="mt12"><span class="sk" style="width:260px;height:30px" /></div></div>
        <div class="grid cols-3"><div v-for="item in 3" :key="item" class="card pad" style="min-height:118px"><span class="sk" style="width:62%;height:12px" /></div></div>
      </div>
    </main>
  </div>

  <div v-else-if="bootState === 'offline'" class="app login-mode" style="display:block">
    <div class="auth">
      <div class="aside">
        <div class="hstack" style="gap:11px;position:relative;z-index:1">
          <span class="mark" style="width:36px;height:36px;border-radius:11px;background:radial-gradient(circle at 30% 25%,#E89A7C,#D97757 62%);display:grid;place-items:center;font-family:var(--font-display);font-weight:700;color:#fff">O</span>
          <span style="font-family:var(--font-display);font-size:20px;font-weight:600">Oz<span style="color:var(--coral)">DDNS</span></span>
        </div>
        <div>
          <div class="quote">{{ t("Your hosts move. Your DNS follows, quietly.") }}</div>
          <p class="qsub">{{ t("This page is served by the daemon, so the panel exists — but the API behind it does not answer right now.") }}</p>
        </div>
        <div class="hstack" style="gap:18px;color:rgba(243,239,230,.55);font-size:12px;position:relative;z-index:1"><span>{{ t("7 providers") }}</span><span>·</span><span>{{ t("single binary") }}</span><span>·</span><span>{{ t("local-first") }}</span></div>
      </div>
      <div class="form-side">
        <div class="fbox">
          <h1 class="page-title" style="font-size:26px">{{ t("Cannot reach the daemon") }}</h1>
          <p class="page-sub mb24">{{ t("No data is shown until the API answers again — OzDDNS never fills the panel with sample records. Check that the daemon is running, then try again.") }}</p>
          <div class="field">
            <label>{{ t("Last error") }}</label>
            <input class="input mono" readonly :value="offlineReason || t('No response')">
          </div>
          <button class="btn primary block lg" type="button" @click="retryBoot()"><Icon name="rotate-cw" />{{ t("Try again") }}</button>
        </div>
      </div>
    </div>
  </div>
  <AppShell v-else-if="useShell">
    <div v-if="authState.exposed && !authState.enabled" class="banner warn mb16">
      <Icon name="shield" class="ico" />
      <div class="grow">
        <div class="b-title">{{ t("This service is reachable beyond localhost without authorization") }}</div>
        <div class="b-body">{{ t("Anyone who can reach {addr} can change every provider, task and DNS record. Enable authorization or bind the daemon to 127.0.0.1.", { addr: authState.listen || data.runtime.listen }) }}</div>
      </div>
      <RouterLink class="btn sm" to="/settings/security">{{ t("Enable authorization") }}</RouterLink>
    </div>
    <div v-if="staleSince" class="banner warn mb16">
      <Icon name="alert-triangle" class="ico" />
      <div class="grow">
        <div class="b-title">{{ t("Lost contact with the daemon") }}</div>
        <div class="b-body">{{ t("Last contact {ago}. This panel is showing the last snapshot it received.", { ago: fmtAgo(staleSince) }) }}</div>
      </div>
      <button class="btn sm" @click="refreshSoon()">{{ t("Retry now") }}</button>
    </div>
    <RouterView />
  </AppShell>
  <RouterView v-else />

  <ModalHost />
  <div v-if="toasts.length" class="toasts">
    <div v-for="toast in toasts" :key="toast.id" class="toast" :class="toast.kind">
      <Icon :name="toast.kind === 'ok' ? 'check-circle' : toast.kind === 'err' ? 'x-circle' : 'info'" />
      <div class="grow"><div class="t">{{ toast.title }}</div><div v-if="toast.desc" class="d">{{ toast.desc }}</div></div>
      <button class="icon-btn" style="color:inherit;width:24px;height:24px" :aria-label="t('Dismiss')" @click="dismissToast(toast.id)"><Icon name="x" /></button>
    </div>
  </div>
</template>
