<script setup lang="ts">
// Remote servers (the sidebar, above Doctor and Settings): every server with
// its connection, adding, editing and deleting them, and the settings that
// cover them all (how many may be connected at once). Each server's own
// notifications are in its form.
import { onMounted, ref } from 'vue'
import { SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import ServersCard from '../components/ServersCard.vue'
import { errorMessage } from '../lib/overview'
import { showToast } from '../lib/toast'

const max = ref(0)

onMounted(async () => {
  max.value = (await SettingsService.Get()).Settings.maxServers || 3
})

async function setLimit(n: number) {
  try {
    const v = await SettingsService.Get()
    v.Settings.maxServers = n
    max.value = (await SettingsService.Save(v.Settings)).Settings.maxServers
  } catch (e) {
    showToast(errorMessage(e), 'error')
  }
}
</script>

<template>
  <section class="page" aria-labelledby="servers-h">
    <header>
      <h1 id="servers-h">Remote servers</h1>
      <p class="muted">Visor manages supervisord on your Linux servers over SSH, each in a tab beside this computer’s. Nothing is installed on a server.</p>
    </header>
    <ServersCard v-if="max" :max-servers="max" @limit="setLimit" />
  </section>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 18px; container-type: inline-size; }
header { display: flex; flex-direction: column; gap: 6px; }
h1 { margin: 0; }
header p { margin: 0; }
</style>
