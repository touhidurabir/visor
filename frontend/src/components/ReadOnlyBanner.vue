<script setup lang="ts">
// Shown on Programs pages when the config folder isn't writable: what is
// off, and a link to Doctor, which says how to allow editing (on a server:
// that editing comes in the next version).
import { computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import { loadFolder, readOnlyReason, useFolder } from '../lib/folder'

const folder = useFolder()
onMounted(() => void loadFolder())
const reason = computed(() => readOnlyReason(folder.value))
</script>

<template>
  <div v-if="reason" class="banner warn" role="status" data-testid="read-only-banner">
    <span>
      <strong>{{ reason }}.</strong> Visor shows and runs these configs, but adding, saving, removing and restoring them are off.
      <template v-if="folder?.Remote">Editing configs on servers comes in the next version.</template>
      <RouterLink v-else to="/doctor">How to allow editing</RouterLink>
    </span>
  </div>
</template>

<style scoped>
.banner { padding: 10px 14px; border-radius: var(--radius-md); background: var(--warning-bg); color: var(--warning-fg); font-size: 13px; line-height: 1.45; margin-bottom: 16px; }
.banner a { color: inherit; text-decoration: underline; }
</style>
