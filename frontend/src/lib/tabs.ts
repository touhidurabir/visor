// Which tab is on screen: 'local' (this computer) or a saved server's id.
// Every per-tab service call (lib/api.ts) goes to this tab.
import { ref } from 'vue'

export const LOCAL = 'local'

export const currentTab = ref<string>(LOCAL)

/** setCurrentTab switches the tab the views and api calls use. */
export function setCurrentTab(id: string) {
  currentTab.value = id || LOCAL
}
