<script setup lang="ts">
import { reactive, ref } from "vue";
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { t } from "../lib";
import { bootState, fail, notify, signIn } from "../store";

const router = useRouter();
const form = reactive({ username: "admin", password: "" });
const passwordError = ref(false);

async function submit(): Promise<void> {
  if (!form.password && bootState.value !== "offline") {
    passwordError.value = true;
    return;
  }
  try {
    if (bootState.value !== "offline") await signIn(form.username || "admin", form.password);
    passwordError.value = false;
    notify("ok", t("Welcome back"), t("Signed in as {user}", { user: form.username || "admin" }));
    router.push("/overview");
  } catch (error) {
    passwordError.value = true;
    fail(error, t("Sign-in failed"));
  }
}
</script>

<template>
  <div class="app login-mode" style="display:block">
    <div class="auth">
      <div class="aside">
        <div class="hstack" style="gap:11px;position:relative;z-index:1">
          <span class="mark" style="width:36px;height:36px;border-radius:11px;background:radial-gradient(circle at 30% 25%,#E89A7C,#D97757 62%);display:grid;place-items:center;font-family:var(--font-display);font-weight:700;color:#fff">O</span>
          <span style="font-family:var(--font-display);font-size:20px;font-weight:600">Oz<span style="color:var(--coral)">DDNS</span></span>
        </div>
        <div><div class="quote">{{ t("Your hosts move. Your DNS follows, quietly.") }}</div><p class="qsub">{{ t("A provider-centric dynamic DNS engine — reconcile the address your machines actually hold with the records the world resolves, and never guess again.") }}</p></div>
        <div class="hstack" style="gap:18px;color:rgba(243,239,230,.55);font-size:12px;position:relative;z-index:1"><span>{{ t("7 providers") }}</span><span>·</span><span>{{ t("single binary") }}</span><span>·</span><span>{{ t("local-first") }}</span></div>
      </div>
      <div class="form-side">
        <form class="fbox" @submit.prevent="submit">
          <h1 class="page-title" style="font-size:26px">{{ t("Sign in") }}</h1>
          <p class="page-sub mb24">{{ t("Authenticate to manage providers, tasks and records.") }}</p>
          <div class="field"><label>{{ t("Username") }}</label><input v-model="form.username" class="input mono" autocomplete="username"></div>
          <div class="field"><label>{{ t("Password") }}</label><input v-model="form.password" class="input mono" type="password" autocomplete="current-password" placeholder="••••••••••"></div>
          <div v-if="passwordError" class="err-msg mb12" style="color:var(--rust);font-size:12.5px;display:flex;gap:6px;align-items:center"><Icon name="alert-triangle" />{{ t("Check your credentials.") }}</div>
          <button class="btn primary block lg" type="submit"><Icon name="log-in" />{{ t("Sign in") }}</button>
          <div class="divider" />
          <p class="tiny muted" style="text-align:center">{{ t("No credentials yet? A fresh install opens without auth on") }} <span class="mono">127.0.0.1</span>.</p>
        </form>
      </div>
    </div>
  </div>
</template>
