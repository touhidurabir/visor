<script setup lang="ts">
// Title and the Write it / Fill a form switch shared by both Add new pages.
import { RouterLink } from 'vue-router'
import { useOverview } from '../lib/overview'

defineProps<{ mode: 'raw' | 'form' }>()
const { overview } = useOverview()
</script>

<template>
  <nav class="crumbs" aria-label="Breadcrumb"><RouterLink to="/">{{ overview?.Server.Name ?? 'Local' }}</RouterLink> / Add new</nav>
  <header>
    <h1>Add a new config</h1>
    <div class="seg" role="group" aria-label="How to add it">
      <RouterLink :class="{ on: mode === 'raw' }" to="/new" :aria-current="mode === 'raw' ? 'page' : undefined">Write it</RouterLink>
      <RouterLink :class="{ on: mode === 'form' }" to="/new/form" :aria-current="mode === 'form' ? 'page' : undefined">Fill a form</RouterLink>
    </div>
  </header>
</template>

<style scoped>
.crumbs { color: var(--text-muted); }
.crumbs a { color: var(--accent-text); }
header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
</style>
