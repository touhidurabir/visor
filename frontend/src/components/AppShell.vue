<script setup lang="ts">
// The main window frame: sidebar plus a scrolling content area. The sidebar
// health comes from the same visor:overview feed as every other view.
import { computed } from 'vue'
import { useOverview } from '../lib/overview'
import Sidebar from './Sidebar.vue'

const { overview } = useOverview()
const name = computed(() => overview.value?.Server.Name ?? 'Local')
const health = computed(() => overview.value?.Server.Health ?? 'unknown')
const failing = computed(() => overview.value?.Stats.Failing ?? 0)
const openIssues = computed(() => overview.value?.Stats.OpenIssues ?? 0)
</script>

<template>
  <div class="shell">
    <Sidebar :name="name" :health="health" :failing="failing" :open-issues="openIssues" />
    <main class="content"><slot /></main>
  </div>
</template>

<style scoped>
.shell { display: flex; height: 100vh; background: var(--bg); color: var(--text); }
.content { flex: 1; min-width: 0; overflow: auto; box-sizing: border-box; padding: 40px 28px 40px; }
</style>
