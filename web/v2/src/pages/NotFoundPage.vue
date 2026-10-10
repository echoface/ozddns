<script setup lang="ts">
import { EmptyState } from "../components/UiKit";
import { t } from "../lib";
</script>

<template>
  <div class="card">
    <EmptyState icon="alert-triangle" title="Page not found" body="The thing you were looking for does not exist (or was deleted).">
      <RouterLink class="btn primary" to="/overview">{{ t("Back to overview") }}</RouterLink>
    </EmptyState>
  </div>
</template>
