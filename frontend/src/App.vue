<script setup lang="ts">
import { RouterView, useRoute } from 'vue-router'
import AppShell from './components/AppShell.vue'

const route = useRoute()
</script>

<template>
  <!-- The tray popover (#/tray) is a bare route: no sidebar. -->
  <!-- Render nothing until the first navigation resolves, so #/tray never
       mounts AppShell (and its overview feed) for a frame. -->
  <template v-if="route.matched.length === 0" />
  <RouterView v-else-if="route.meta.bare" />
  <AppShell v-else>
    <RouterView />
  </AppShell>
</template>
