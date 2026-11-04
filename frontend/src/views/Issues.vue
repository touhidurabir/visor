<script lang="ts">
import { reactive } from 'vue'
import type { LocationQuery } from 'vue-router'

// Each tab's filters, from its URL, so switching tabs and back keeps them.
const lastQuery = reactive<Record<'issues' | 'issues-log', LocationQuery>>({ issues: {}, 'issues-log': {} })
</script>

<script setup lang="ts">
// The Issues page: two tabs, each its own URL. "Issues" lists every issue
// Visor has recorded; "supervisord log" reads supervisord's own log. Both
// keep their filters in the URL query.
import { computed, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import IssuesList from '../components/IssuesList.vue'
import MainLogTab from '../components/MainLogTab.vue'

const route = useRoute()
const onLog = computed(() => route.name === 'issues-log')
watch(
  () => [route.name, route.query] as const,
  ([name, query]) => {
    if (name === 'issues' || name === 'issues-log') lastQuery[name] = query
  },
  { immediate: true },
)
</script>

<template>
  <section class="page">
    <header>
      <div class="title">
        <h1>Issues</h1>
        <p v-if="!onLog" class="help">An issue opens when a process can’t start, exits when it shouldn’t, or keeps restarting. Can’t-start issues close by themselves.</p>
      </div>
      <nav class="tabs" aria-label="Issues sections">
        <RouterLink :to="{ path: '/issues', query: lastQuery.issues }" exact-active-class="on">Issues</RouterLink>
        <RouterLink :to="{ path: '/issues/log', query: lastQuery['issues-log'] }" exact-active-class="on">supervisord log</RouterLink>
      </nav>
    </header>
    <div v-if="onLog" role="region" aria-label="supervisord log">
      <MainLogTab />
    </div>
    <IssuesList v-else />
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; }
header { display: flex; flex-direction: column; gap: 18px; }
.title { display: flex; flex-direction: column; gap: 6px; }
h1 { margin: 0; font-family: var(--display); font-optical-sizing: auto; font-size: 30px; font-weight: 650; line-height: 1.1; letter-spacing: -0.015em; }
.help { margin: 0; color: var(--ink2); }
</style>
