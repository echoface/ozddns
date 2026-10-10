<script setup lang="ts">
import { computed } from "vue";

const props = withDefaults(defineProps<{ name: string; class?: string }>(), { class: "" });
const path = computed(() => window.ICONS?.[props.name] || window.ICONS?.["circle-dot"] || "");
</script>

<template>
  <svg
    :class="['ico', props.class]"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    v-html="path"
  />
</template>
