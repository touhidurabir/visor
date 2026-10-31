// A minimal hash router: the main window lives at #/ (and #/settings), the
// menu-bar popover at #/tray. Task 14 replaces it with vue-router.
import { readonly, ref, type Ref } from 'vue'

export type Route = 'main' | 'tray' | 'settings'

export function routeOf(hash: string): Route {
  const path = hash.replace(/^#/, '').replace(/\/+$/, '')
  switch (path) {
    case '/tray':
      return 'tray'
    case '/settings':
      return 'settings'
    default:
      return 'main'
  }
}

let current: Ref<Route> | undefined

/** useRoute returns the current route, kept in step with location.hash. */
export function useRoute(): Readonly<Ref<Route>> {
  if (!current) {
    const r = ref(routeOf(window.location.hash))
    window.addEventListener('hashchange', () => {
      r.value = routeOf(window.location.hash)
    })
    current = r
  }
  return readonly(current)
}
