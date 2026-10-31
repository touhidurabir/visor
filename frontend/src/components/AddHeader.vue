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
      <RouterLink class="segl" :class="{ on: mode === 'raw' }" to="/new" :aria-current="mode === 'raw' ? 'page' : undefined">Write it</RouterLink>
      <RouterLink class="segl" :class="{ on: mode === 'form' }" to="/new/form" :aria-current="mode === 'form' ? 'page' : undefined">Fill a form</RouterLink>
    </div>
  </header>
</template>

<style scoped>
.crumbs { color: var(--text-muted); }
.crumbs a { color: var(--accent-text); }
header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
.seg { display: flex; gap: 3px; padding: 3px; background: var(--seg-bg); border-radius: 8px; }
.segl { display: inline-flex; align-items: center; min-height: 30px; padding: 0 14px; border-radius: 6px; color: var(--stopped-fg); text-decoration: none; }
.segl:hover { color: var(--text); }
.segl.on { background: var(--seg-on); color: var(--text); box-shadow: 0 1px 2px rgba(0, 0, 0, 0.18); font-weight: 600; }
</style>
