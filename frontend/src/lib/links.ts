// Links between screens. Kept apart from router.ts so components can build
// links without importing every view.
import type { RouteLocationRaw } from 'vue-router'

/**
 * configRoute links to a config's detail page. The path is absolute; as a
 * route param it is encoded (slashes, spaces, #) and comes back exactly.
 * Query keys: tab (overview|logs|config|history), process (group:name).
 */
export function configRoute(path: string, query: Record<string, string> = {}): RouteLocationRaw {
  return { name: 'config', params: { path }, query }
}
