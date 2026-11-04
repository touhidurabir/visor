// A tiny toast queue shared by screens. Errors stay longer than info, and
// so does a toast with an action, so there is time to reach its button.
import { ref } from 'vue'

/** A toast's one button (e.g. "Open Doctor"). Running it dismisses the toast. */
export interface ToastAction {
  label: string
  run: () => void
}

export interface Toast {
  id: number
  kind: 'error' | 'info'
  text: string
  action?: ToastAction
}

export const toasts = ref<Toast[]>([])
let next = 1

export function dismissToast(id: number) {
  toasts.value = toasts.value.filter((t) => t.id !== id)
}

/** How long a toast stays, in ms. */
export function toastLifetime(kind: Toast['kind'], action?: ToastAction): number {
  return kind === 'error' || action ? 8000 : 4000
}

export function showToast(text: string, kind: Toast['kind'] = 'info', action?: ToastAction) {
  const id = next++
  toasts.value = [...toasts.value.slice(-2), { id, kind, text, ...(action ? { action } : {}) }]
  setTimeout(() => dismissToast(id), toastLifetime(kind, action))
}

/** Runs a toast's action and dismisses the toast. */
export function runToastAction(id: number) {
  const t = toasts.value.find((x) => x.id === id)
  dismissToast(id)
  t?.action?.run()
}
