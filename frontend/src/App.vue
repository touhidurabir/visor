<script setup lang="ts">
import { useRoute } from './lib/router'
import ProcessList from './views/ProcessList.vue'
import Tray from './views/Tray.vue'

const route = useRoute()
</script>

<template>
  <Tray v-if="route === 'tray'" />
  <main v-else class="shell">
    <template v-if="route === 'settings'">
      <h1>Settings</h1>
      <p class="muted">Settings arrive in a later version of Visor.</p>
    </template>
    <template v-else>
      <h1>Visor</h1>
      <ProcessList />
    </template>
  </main>
</template>

<style scoped>
.shell {
  padding: 56px 24px 24px;
}
h1 {
  margin: 0 0 12px;
  font-size: 20px;
  font-weight: 600;
}
.muted { color: var(--text-muted); }
</style>
