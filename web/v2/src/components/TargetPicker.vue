<script setup lang="ts">
import { computed, ref, watch } from "vue";
import Icon from "./Icon.vue";
import { t } from "../lib";

interface Option {
  value: string;
  label: string;
}

// A dropdown of sensible presets that can also take a hand-written value:
// "Custom…" flips the control into a free-text field, and any stored value
// that is not one of the presets opens straight in that mode.
const props = withDefaults(defineProps<{
  modelValue: string;
  options: Option[];
  placeholder?: string;
  disabled?: boolean;
}>(), { placeholder: "", disabled: false });

const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const CUSTOM = "__custom__";
const custom = ref(false);

const known = computed(() => props.options.some((option) => option.value === props.modelValue));
const selected = computed(() => (props.modelValue && known.value ? props.modelValue : ""));

watch([() => props.modelValue, () => props.options], () => {
  custom.value = props.modelValue !== "" && !known.value;
}, { immediate: true, deep: true });

function onSelect(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  if (value === CUSTOM) {
    custom.value = true;
    return;
  }
  custom.value = false;
  emit("update:modelValue", value);
}

function onInput(event: Event): void {
  emit("update:modelValue", (event.target as HTMLInputElement).value);
}

function backToList(): void {
  custom.value = false;
  if (!known.value) emit("update:modelValue", "");
}
</script>

<template>
  <select v-if="!custom" class="input mono" :value="selected" :disabled="disabled" @change="onSelect">
    <option value="" disabled>{{ t("Choose a preset…") }}</option>
    <option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option>
    <option :value="CUSTOM">{{ t("Custom…") }}</option>
  </select>
  <div v-else class="hstack" style="gap:8px">
    <input class="input mono" style="flex:1" :value="modelValue" :placeholder="placeholder" :disabled="disabled" @input="onInput">
    <button type="button" class="icon-btn" :aria-label="t('Back to presets')" :disabled="disabled" @click="backToList"><Icon name="list" /></button>
  </div>
</template>
