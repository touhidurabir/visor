// Settings shared by screens, and the unit conversions the Settings page
// needs: Go sends durations as nanoseconds, the page shows seconds and
// minutes. The limits mirror internal/settings (Go clamps; these only
// explain what it did).
import { ref } from 'vue'
import { SettingsService } from '../../bindings/github.com/touhidurabir/visor/internal/app'
import type { SettingsView } from '../../bindings/github.com/touhidurabir/visor/internal/app'

const NS_PER_SECOND = 1_000_000_000
const NS_PER_MINUTE = 60 * NS_PER_SECOND

export const LIMITS = {
  pollSeconds: { min: 1, max: 10 },
  restarts: { min: 2, max: 100 },
  windowMinutes: { min: 1, max: 24 * 60 },
  retentionDays: { min: 7, max: 3650 },
} as const

export type Limit = { min: number; max: number }

export const nsToSeconds = (ns: number): number => Math.round((ns / NS_PER_SECOND) * 10) / 10
export const secondsToNs = (s: number): number => Math.round(s * NS_PER_SECOND)
export const nsToMinutes = (ns: number): number => Math.round((ns / NS_PER_MINUTE) * 10) / 10
export const minutesToNs = (m: number): number => Math.round(m * NS_PER_MINUTE)

/**
 * typedNumber reads a number field. It returns null for an empty or
 * non-numeric entry, which the page treats as "put the saved value back".
 */
export function typedNumber(text: string | number): number | null {
  if (typeof text === 'number') return Number.isFinite(text) ? text : null
  const t = text.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : null
}

/**
 * clampNote explains a value Visor changed on save: typed is what the
 * person entered, saved what Go kept. It is '' when nothing changed.
 */
export function clampNote(typed: number | null, saved: number, limit: Limit, unit: string): string {
  if (typed === null || typed === saved) return ''
  const plural = (n: number) => `${n} ${unit}${n === 1 ? '' : 's'}`
  if (saved === limit.min && typed < saved) return `Set to ${plural(saved)}, the shortest allowed.`
  if (saved === limit.max && typed > saved) return `Set to ${plural(saved)}, the longest allowed.`
  return `Set to ${plural(saved)}.`
}

export type RetentionChange = { kind: 'same' | 'invalid' } | { kind: 'raise' | 'lower'; days: number }

/**
 * retentionChange says what applying the typed retention would do, in whole
 * days clamped as Go clamps them, so a confirmation names the days that
 * will apply. Lowering deletes history and needs confirming (R27).
 */
export function retentionChange(text: string | number, saved: number): RetentionChange {
  const n = typedNumber(text)
  if (n === null) return { kind: 'invalid' }
  const { min, max } = LIMITS.retentionDays
  const days = Math.min(max, Math.max(min, Math.round(n)))
  if (days === saved) return { kind: 'same' }
  return { kind: days < saved ? 'lower' : 'raise', days }
}

/** What to ask before lowering retention deletes older issues and history. */
export function retentionConfirm(days: number): { title: string; message: string; confirmLabel: string } {
  return {
    title: `Delete issues and history older than ${days} days?`,
    message: 'This can’t be undone.',
    confirmLabel: 'Delete older issues',
  }
}

/** The settings in use, loaded once and refreshed by every save. */
export const currentSettings = ref<SettingsView | null>(null)

export async function loadSettings(): Promise<SettingsView> {
  const v = await SettingsService.Get()
  currentSettings.value = v
  return v
}

/** How macOS treats Visor's notifications (NotifyService.NotificationStatus). */
export type NotifyPermission = 'allowed' | 'denied' | 'unavailable' | ''

/**
 * notifyPermission words the notification permission for Settings, with
 * the badge tone to show it in (the shared b-* and d-* classes). '' means
 * it is still being checked.
 */
export function notifyPermission(status: string): { text: string; tone: 'ok' | 'bad' | 'idle' } {
  switch (status) {
    case 'allowed':
      return { text: 'Allowed', tone: 'ok' }
    case 'denied':
      // Not answered yet, or turned off: macOS reports both the same way.
      return { text: 'Not allowed. Allow Visor in System Settings → Notifications.', tone: 'bad' }
    case 'unavailable':
      return { text: 'Not available in this build', tone: 'idle' }
  }
  return { text: 'Checking…', tone: 'idle' }
}
