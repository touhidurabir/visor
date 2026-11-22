import { createApp } from 'vue'
import App from './App.vue'
import { createAppRouter } from './lib/router'
import '@fontsource-variable/bricolage-grotesque/opsz.css'
import '@fontsource-variable/jetbrains-mono/wght.css'
import './style.css'
import { startThemeSync } from './lib/theme-sync'

import { loadPlatform } from './lib/platform'

startThemeSync()

// The wording (⌘ or Ctrl, Keychain or keyring…) is known before anything renders.
void loadPlatform().then(() => createApp(App).use(createAppRouter()).mount('#app'))
