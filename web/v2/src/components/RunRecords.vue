<script setup lang="ts">
import { computed } from "vue";
import Icon from "./Icon.vue";
import { t } from "../lib";
import type { RunDetail } from "../types";

const props = defineProps<{ details?: RunDetail[]; summary?: boolean }>();

// Outcome ordering and colour, shared by the per-record pills and the summary.
const SHAPES: { action: string; kind: string }[] = [
  { action: "created", kind: "ok" },
  { action: "updated", kind: "ok" },
  { action: "applied", kind: "ok" },
  { action: "failed", kind: "err" },
  { action: "skipped", kind: "warn" },
  { action: "unchanged", kind: "mute" }
];

// Map a per-record action to a pill kind so the colour carries the meaning.
function kind(action: string): string {
  switch (action) {
    case "created":
    case "updated":
    case "applied":
      return "ok";
    case "failed":
      return "err";
    case "skipped":
      return "warn";
    default:
      return "mute";
  }
}

function tip(detail: RunDetail): string {
  const label = t(detail.action);
  const proxy = detail.proxied ? ` · ${t("Proxied")}` : "";
  if (detail.note) return `${detail.domain} · ${label}${proxy} · ${detail.note}`;
  const from = detail.current || "—";
  const to = detail.desired || "—";
  return `${detail.domain} · ${label}${proxy} · ${from} → ${to}`;
}

// Counts per action, in a stable order, for the list views that only need a
// tally ("3 created · 1 failed") rather than every record.
const counts = computed(() => {
  const tally = new Map<string, number>();
  for (const detail of props.details || []) tally.set(detail.action, (tally.get(detail.action) || 0) + 1);
  const rows = SHAPES.filter((shape) => tally.get(shape.action)).map((shape) => ({ action: shape.action, n: tally.get(shape.action) as number }));
  for (const [action, n] of tally) if (!SHAPES.some((shape) => shape.action === action)) rows.push({ action, n });
  return rows;
});
</script>

<template>
  <div v-if="details?.length" class="hstack wrap" style="gap:6px">
    <template v-if="summary">
      <span v-for="row in counts" :key="row.action" class="pill" :class="kind(row.action)">{{ row.n }} {{ t(row.action) }}</span>
    </template>
    <template v-else>
      <span
        v-for="detail in details"
        :key="`${detail.domain}-${detail.type}`"
        class="pill"
        :class="kind(detail.action)"
        :title="tip(detail)"
      >
        <Icon v-if="detail.proxied" name="cloud" />
        <span class="mono rr-domain">{{ detail.domain }}</span>
        <span>{{ t(detail.action) }}</span>
      </span>
    </template>
  </div>
  <span v-else class="muted tiny">—</span>
</template>

<style scoped>
.rr-domain {
  font-size: 11px;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
