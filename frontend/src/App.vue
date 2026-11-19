<script setup lang="ts">
import { computed, effectScope, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import AppShell from './components/AppShell.vue'
import Doctor from './views/Doctor.vue'
import { useOverview } from './lib/overview'
import { loadSettings } from './lib/settings'
import { currentTab, LOCAL } from './lib/tabs'

const route = useRoute()
const router = useRouter()
// The tab the page on screen belongs to (from its route, not currentTab,
// which changes a moment before the new tab's route is in place).
const routeTab = computed(() => (typeof route.params.server === 'string' ? route.params.server : LOCAL))

// First run (spec §5.1): Doctor fills the main window until setup is
// finished. null while the settings load; a failed load opens the app.
const firstRun = ref<boolean | null>(null)
// "Open Settings" from first-run Doctor leaves it for this launch only.
const leftSetup = ref(false)

let started = false
watch(
  () => route.matched.length,
  (n) => {
    if (!n || started || route.meta.bare) return // the tray popover never gates
    started = true
    loadSettings()
      .then((v) => (firstRun.value = !v.Settings.onboarded))
      .catch(() => (firstRun.value = false))
    openDoctorIfDown()
  },
  { immediate: true },
)

/**
 * When the first connection after launch fails, an onboarded Visor opens on
 * Doctor instead of an empty dashboard. Only from the dashboard, and once.
 */
function openDoctorIfDown() {
  const scope = effectScope()
  scope.run(() => {
    const { overview } = useOverview()
    watch([overview, firstRun], ([v, first]) => {
      const server = v?.Server
      if (first === null || !server || server.Health === 'unknown') return
      queueMicrotask(() => scope.stop()) // decided once: stop listening
      if (!server.Reachable && first === false && route.name === 'dashboard' && currentTab.value === 'local') void router.replace('/doctor')
    })
  })
}

function finishSetup() {
  firstRun.value = false
  void router.replace('/')
}

function leaveSetup(to: string) {
  leftSetup.value = true
  void router.push(to)
}
</script>

<template>
  <!-- The tray popover (#/tray) is a bare route: no sidebar. -->
  <!-- Render nothing until the first navigation resolves, so #/tray never
       mounts AppShell (and its overview feed) for a frame. -->
  <template v-if="route.matched.length === 0" />
  <RouterView v-else-if="route.meta.bare" />
  <template v-else-if="firstRun === null" />
  <Doctor v-else-if="firstRun && !leftSetup" first-run @done="finishSetup" @leave="leaveSetup" />
  <AppShell v-else>
    <!-- Keyed by tab, so a page shared by two tabs reloads for the new one. -->
    <RouterView :key="routeTab" />
  </AppShell>
</template>
