<script setup lang="ts">
// The Issues page: two tabs, each its own URL. "Issues" lists every issue
// Visor has recorded; "supervisord log" reads supervisord's own log.
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import IssuesList from '../components/IssuesList.vue'
import MainLogTab from '../components/MainLogTab.vue'

const route = useRoute()
const onLog = computed(() => route.name === 'issues-log')
</script>

<template>
  <section class="page">
    <header>
      <h1>Issues</h1>
      <nav class="tabs" aria-label="Issues sections">
        <RouterLink to="/issues" class="tab" exact-active-class="on">Issues</RouterLink>
        <RouterLink to="/issues/log" class="tab" exact-active-class="on">supervisord log</RouterLink>
      </nav>
    </header>
    <div v-if="onLog" role="region" aria-label="supervisord log">
      <MainLogTab />
    </div>
    <IssuesList v-else />
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; }
header { display: flex; flex-direction: column; gap: 10px; }
h1 { margin: 0; font-size: 22px; font-weight: 650; }
.tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--line); }
.tab { padding: 8px 14px; margin-bottom: -1px; border-bottom: 2px solid transparent; color: var(--text-muted); text-decoration: none; }
.tab:hover { color: var(--text); }
.tab.on { border-bottom-color: var(--accent-text); color: var(--text); font-weight: 600; }
</style>
