<script setup lang="ts">
import { computed, nextTick, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import AppShell from "./components/AppShell.vue";
import ModalHost from "./components/ModalHost.vue";
import Icon from "./components/Icon.vue";
import { bootState, dismissToast, toasts } from "./store";
import { localeVersion, t } from "./lib";

const route = useRoute();
const useShell = computed(() => route.meta.shell !== false);

async function translateRenderedView(): Promise<void> {
  localeVersion.value;
  await nextTick();
  const root = document.querySelector("#root");
  if (root) window.OZ.i18n.applyDOM(root);
}

watch([() => route.fullPath, localeVersion, bootState], translateRenderedView);
onMounted(translateRenderedView);
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

  <AppShell v-else-if="useShell">
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
