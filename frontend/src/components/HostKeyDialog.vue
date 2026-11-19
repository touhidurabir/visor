<script setup lang="ts">
// The host key prompts. A server Visor hasn't seen: its key's fingerprint
// to check, and Trust and connect (Visor's own known_hosts; ~/.ssh/known_hosts
// is never changed). A key that changed: refused, with the command that
// removes the old key, and no way to connect anyway.
import { ref } from 'vue'
import { RemoteService, type HostKeyView } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import { errorMessage } from '../lib/overview'
import CommandBlock from './CommandBlock.vue'
import DialogShell from './DialogShell.vue'

const props = defineProps<{ view: HostKeyView; server: string }>()
const emit = defineEmits<{ trusted: []; close: [] }>()

const busy = ref(false)
const failed = ref('')
const hostName = () => props.view.Host.replace(/:22$/, '')
const keyType = (t: string) => ({ 'ssh-ed25519': 'ED25519', 'ssh-rsa': 'RSA', 'ecdsa-sha2-nistp256': 'ECDSA', 'ecdsa-sha2-nistp384': 'ECDSA', 'ecdsa-sha2-nistp521': 'ECDSA' })[t] ?? t

async function trust() {
  busy.value = true
  failed.value = ''
  try {
    await RemoteService.Trust(props.view.Host, props.view.Fingerprint)
    emit('trusted') // before close, which drops the prompt (and its retry)
    emit('close')
  } catch (e) {
    failed.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <DialogShell v-if="view.Status === 'unknown'" :title="`Trust ${hostName()}?`" @close="emit('close')">
    <p class="muted">Visor hasn’t connected to {{ server || 'this server' }} before. Check that this key is the server’s before you trust it, for example with its admin or your hosting dashboard.</p>
    <CommandBlock :text="`${keyType(view.Type)} key fingerprint\n${view.Fingerprint}`" :copy="false" data-testid="fingerprint" />
    <p class="meta">Visor remembers it in its own list. Your <span class="mono">~/.ssh/known_hosts</span> isn’t changed.</p>
    <p v-if="failed" class="err">{{ failed }}</p>
    <template #actions>
      <button class="btn" type="button" @click="emit('close')">Cancel</button>
      <button class="btn primary" type="button" :disabled="busy" data-autofocus @click="trust">Trust and connect</button>
    </template>
  </DialogShell>
  <DialogShell v-else :title="`${server || hostName()}’s host key changed`" @close="emit('close')">
    <p class="muted">Visor didn’t connect. The server answered with a different key from the one it had before. That happens after a reinstall, but it can also mean someone is in the middle.</p>
    <CommandBlock :text="`Before  ${(view.Old ?? []).join(', ')}\nNow     ${view.Fingerprint}`" :copy="false" />
    <p class="meta">If you know why it changed, remove the old key, then connect again:</p>
    <CommandBlock :text="view.Fix" />
    <template #actions>
      <button class="btn primary" type="button" data-autofocus @click="emit('close')">Close</button>
    </template>
  </DialogShell>
</template>

<style scoped>
p { margin: 10px 0; }
.err { color: var(--bad); }
</style>
