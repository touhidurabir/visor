// Hiding the dashboard's "changed on disk" banner: it stays hidden while it
// would say the same thing (the same files changed, not loaded or
// uncertain), and comes back when that changes. Kept until Visor quits.
import { ref } from 'vue'

interface Named {
  Path: string
}

const dismissed = ref('')

/** What the banner is about, as one string; "" when it has nothing to say. */
export function bannerKey(changed: Named[], unloaded: Named[], uncertain: Named[]): string {
  if (!changed.length && !unloaded.length && !uncertain.length) return ''
  const paths = (xs: Named[]) => xs.map((x) => x.Path).sort()
  return JSON.stringify([paths(changed), paths(unloaded), paths(uncertain)])
}

export function dismissBanner(key: string) {
  dismissed.value = key
}

export function isBannerDismissed(key: string): boolean {
  return key !== '' && dismissed.value === key
}
