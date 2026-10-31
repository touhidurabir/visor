// A tiny toast queue shared by screens. Errors stay longer than info.
import { ref } from 'vue'

export interface Toast {
  id: number
  kind: 'error' | 'info'
  text: string
}

export const toasts = ref<Toast[]>([])
let next = 1

export function dismissToast(id: number) {
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

export function showToast(text: string, kind: Toast['kind'] = 'info') {
  const id = next++
  toasts.value = [...toasts.value.slice(-2), { id, kind, text }]
  setTimeout(() => dismissToast(id), kind === 'error' ? 8000 : 4000)
}
