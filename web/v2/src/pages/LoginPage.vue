<script setup lang="ts">
import { onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import Icon from "../components/Icon.vue";
import { t } from "../lib";
import {
  authState,
  bootState,
  loadAuthState,
  login,
  notify,
  verifyMfa
} from "../store";

const router = useRouter();
const step = ref<"password" | "code">("password");
const form = reactive({ username: "admin", password: "", code: "" });
const challenge = ref("");
const error = ref("");
const busy = ref(false);

async function inspectMode(): Promise<void> {
  try {
    const state = await loadAuthState();
    if (!state.enabled || state.authenticated) {
      await router.replace("/overview");
    }
  } catch (err) {
    error.value = (err as Error).message || t("Could not reach the daemon.");
  }
}

onMounted(() => {
  if (bootState.value !== "loading") void inspectMode();
});
watch(bootState, (state) => {
  if (state === "ready") void inspectMode();
});

async function submitPassword(): Promise<void> {
  error.value = "";
  if (!form.username.trim() || !form.password) {
    error.value = t("Enter your username and password.");
    return;
  }
  busy.value = true;
  try {
    challenge.value = await login(form.username.trim(), form.password);
    form.code = "";
    step.value = "code";
  } catch (err) {
    const status = (err as Error & { status?: number }).status;
    error.value = status === 429
      ? t("Too many attempts. Wait a few minutes, then try again.")
      : t("Invalid username or password.");
  } finally {
    busy.value = false;
  }
}

async function submitCode(): Promise<void> {
  error.value = "";
  if (!form.code.trim()) {
    error.value = t("Enter the 6-digit code or one recovery code.");
    return;
  }
  busy.value = true;
  try {
    await verifyMfa(challenge.value, form.code.trim());
    notify("ok", t("Welcome back"), t("Signed in as {user}", { user: authState.value.username || form.username }));
    await router.replace("/overview");
  } catch (err) {
    const status = (err as Error & { status?: number }).status;
    error.value = status === 429
      ? t("Too many attempts. Wait a few minutes, then try again.")
      : t("Invalid authentication code. Use the current 6-digit code or an unused recovery code.");
  } finally {
    busy.value = false;
  }
}

function backToPassword(): void {
  challenge.value = "";
  form.code = "";
  error.value = "";
  step.value = "password";
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
        <div>
          <div class="quote">{{ t("Your hosts move. Your DNS follows, quietly.") }}</div>
          <p class="qsub">{{ t("A provider-centric dynamic DNS engine — reconcile the address your machines actually hold with the records the world resolves, and never guess again.") }}</p>
        </div>
        <div class="hstack" style="gap:18px;color:rgba(243,239,230,.55);font-size:12px;position:relative;z-index:1"><span>{{ t("7 providers") }}</span><span>·</span><span>{{ t("single binary") }}</span><span>·</span><span>{{ t("local-first") }}</span></div>
      </div>
      <div class="form-side">
        <form v-if="step === 'password'" class="fbox" @submit.prevent="submitPassword">
          <h1 class="page-title" style="font-size:26px">{{ t("Sign in") }}</h1>
          <p class="page-sub mb24">{{ t("Authorization is enabled. Verify your password, then the code from your authenticator.") }}</p>
          <div class="field"><label>{{ t("Username") }}</label><input v-model="form.username" class="input mono" autocomplete="username" required></div>
          <div class="field"><label>{{ t("Password") }}</label><input v-model="form.password" class="input mono" type="password" autocomplete="current-password" required></div>
          <div v-if="error" class="err-msg mb12" role="alert" style="color:var(--rust);font-size:12.5px;display:flex;gap:6px;align-items:center"><Icon name="alert-triangle" />{{ error }}</div>
          <button class="btn primary block lg" type="submit" :disabled="busy"><Icon :class="busy ? 'spin' : ''" :name="busy ? 'loader' : 'log-in'" />{{ t(busy ? "Checking…" : "Continue") }}</button>
          <p class="tiny muted mt16" style="text-align:center">{{ t("Sessions stay on the server. Closing the browser ends the browser cookie, and the session expires after inactivity.") }}</p>
        </form>

        <form v-else class="fbox" @submit.prevent="submitCode">
          <div class="hstack mb16" style="gap:10px">
            <span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)"><Icon name="shield" /></span>
            <div>
              <h1 class="page-title" style="font-size:24px">{{ t("Two-step verification") }}</h1>
              <p class="tiny muted">{{ t("Signed in as {user}", { user: form.username }) }}</p>
            </div>
          </div>
          <p class="page-sub mb24">{{ t("Enter the current 6-digit code from your authenticator app. If the authenticator is unavailable, enter one unused recovery code instead.") }}</p>
          <div class="field">
            <label>{{ t("Authentication code") }}</label>
            <input v-model="form.code" class="input mono" autocomplete="one-time-code" spellcheck="false" placeholder="123456 / ABCDE-FGHIJ" autofocus required>
            <span class="help">{{ t("Recovery codes are single-use. A new code is required for every sign-in.") }}</span>
          </div>
          <div v-if="error" class="err-msg mb12" role="alert" style="color:var(--rust);font-size:12.5px;display:flex;gap:6px;align-items:center"><Icon name="alert-triangle" />{{ error }}</div>
          <button class="btn primary block lg" type="submit" :disabled="busy"><Icon :class="busy ? 'spin' : ''" :name="busy ? 'loader' : 'check-circle'" />{{ t(busy ? "Verifying…" : "Verify and sign in") }}</button>
          <button class="btn block mt12" type="button" :disabled="busy" @click="backToPassword">{{ t("Back") }}</button>
        </form>
      </div>
    </div>
  </div>
</template>
