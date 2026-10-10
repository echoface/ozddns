import { computed, ref } from "vue";
import type { ThemePreference } from "./types";

export const localeVersion = ref(0);
export const themePreference = ref<ThemePreference>("light");
export const timeZone = ref("UTC");

export function t(key: string, vars?: Record<string, unknown>): string {
  return window.OZ?.t ? window.OZ.t(key, vars) : interpolate(key, vars);
}

function interpolate(key: string, vars?: Record<string, unknown>): string {
  if (!vars) return key;
  return key.replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? `{${name}}`));
}

export function setLocale(code: string, persist = true): void {
  window.OZ.i18n.set(code, persist);
  document.documentElement.lang = window.OZ.i18n.locale();
  localeVersion.value++;
}

export function currentLocale(): string {
  return window.OZ?.i18n?.locale?.() || "en-US";
}

export function setTimeZone(value: unknown): void {
  timeZone.value = String(value || "UTC");
}

function normalizeTheme(value: unknown): ThemePreference | null {
  const code = String(value || "").toLowerCase();
  return code === "light" || code === "dark" || code === "auto" ? code : null;
}

export function resolvedTheme(pref = themePreference.value): "light" | "dark" {
  if (pref === "auto") {
    return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return pref;
}

export function applyTheme(pref: unknown, persist = true): void {
  themePreference.value = normalizeTheme(pref) || "light";
  const resolved = resolvedTheme();
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePref = themePreference.value;
  document.documentElement.style.colorScheme = resolved;
  if (persist) localStorage.setItem("oz.theme", themePreference.value);
}

export function toggleTheme(): ThemePreference {
  const next = resolvedTheme() === "dark" ? "light" : "dark";
  applyTheme(next, true);
  return next;
}

export function initPreferences(): void {
  const query = new URLSearchParams(location.search);
  const queryTheme = normalizeTheme(query.get("theme"));
  const storedTheme = normalizeTheme(localStorage.getItem("oz.theme"));
  applyTheme(queryTheme || storedTheme || "light", false);

  const queryLang = query.get("lang");
  const storedLang = localStorage.getItem("oz.lang");
  if (queryLang || storedLang) setLocale(queryLang || storedLang || "en", false);

  const media = window.matchMedia?.("(prefers-color-scheme: dark)");
  media?.addEventListener?.("change", () => {
    if (themePreference.value === "auto") applyTheme("auto", false);
  });
}

export const isDark = computed(() => resolvedTheme() === "dark");

export function fmtTime(ts?: number | null): string {
  if (!ts) return "—";
  return new Date(ts).toLocaleTimeString(currentLocale(), {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
    timeZone: timeZone.value
  });
}

export function fmtDateTime(ts?: number | null): string {
  if (!ts) return "—";
  const d = new Date(ts);
  return `${d.toLocaleDateString(currentLocale(), { month: "short", day: "numeric", timeZone: timeZone.value })} ${d.toLocaleTimeString(currentLocale(), { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: timeZone.value })}`;
}

export function fmtAgo(ts?: number | null): string {
  if (!ts) return t("never");
  const seconds = Math.max(0, (Date.now() - ts) / 1000);
  if (seconds < 45) return t("just now");
  if (seconds < 90) return t("1m ago");
  if (seconds < 3600) return t("{n}m ago", { n: Math.round(seconds / 60) });
  if (seconds < 5400) return t("1h ago");
  if (seconds < 86400) return t("{n}h ago", { n: Math.round(seconds / 3600) });
  return t("{n}d ago", { n: Math.round(seconds / 86400) });
}

export function fmtIn(ts?: number | null): string {
  if (!ts) return t("paused");
  const seconds = (ts - Date.now()) / 1000;
  if (seconds <= 0) return t("due now");
  if (seconds < 60) return t("in {n}s", { n: Math.round(seconds) });
  if (seconds < 3600) return t("in {n}m", { n: Math.round(seconds / 60) });
  return t("in {n}h", { n: (seconds / 3600).toFixed(1) });
}

export function fmtDur(ms?: number | null): string {
  if (ms == null) return "—";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(ms < 10000 ? 2 : 1)}s`;
}

export function fmtUptime(seconds = 0): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${days ? `${days}d ` : ""}${hours}h ${minutes}m`;
}

export function initials(label = ""): string {
  return (label.match(/[A-Za-z]/g) || []).slice(0, 2).join("").toUpperCase();
}

export function splitDomains(value: string): string[] {
  return String(value || "").split(/[\n,;]+/).map((item) => item.trim()).filter(Boolean);
}

export function looksMasked(value: unknown): boolean {
  return typeof value === "string" && value.includes("••");
}
