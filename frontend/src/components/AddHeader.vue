<script setup lang="ts">
// Title and the Write it / Fill a form / Import files switch, shared by
// the Add new pages. Write it has "Copy an existing config…" here; the form has it next
// to its templates.
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { placeName, useOverview } from '../lib/overview'
import CopyPicker from './CopyPicker.vue'

defineProps<{ mode: 'raw' | 'form' | 'import' }>()
const { overview } = useOverview()
const picking = ref(false)
</script>

<template>
  <nav class="crumbs" aria-label="Breadcrumb"><RouterLink to="/">{{ placeName(overview) }}</RouterLink> <span aria-hidden="true">/</span> Add new</nav>
  <header>
    <h1>Add a new config</h1>
    <button v-if="mode === 'raw'" type="button" class="btn" @click="picking = true">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15V6a2 2 0 0 1 2-2h8" /></svg>
      Copy an existing config…
    </button>
    <div class="seg" role="group" aria-label="How to add it">
      <RouterLink :class="{ on: mode === 'raw' }" to="/new" :aria-current="mode === 'raw' ? 'page' : undefined">Write it</RouterLink>
      <RouterLink :class="{ on: mode === 'form' }" to="/new/form" :aria-current="mode === 'form' ? 'page' : undefined">Fill a form</RouterLink>
      <RouterLink :class="{ on: mode === 'import' }" to="/new/import" :aria-current="mode === 'import' ? 'page' : undefined">Import files</RouterLink>
    </div>
  </header>
  <CopyPicker v-if="picking" @close="picking = false" />
</template>

<style scoped>
.crumbs { color: var(--ink3); font-size: 12.5px; }
.crumbs a { color: var(--accent-text); text-decoration: none; }
.crumbs a:hover { text-decoration: underline; }
header { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; }
h1 { margin: 0 auto 0 0; font-family: var(--display); font-optical-sizing: auto; font-size: 30px; font-weight: 650; line-height: 1.1; letter-spacing: -0.015em; }
</style>
