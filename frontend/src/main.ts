import { createApp } from 'vue'
import App from './App.vue'
import { createAppRouter } from './lib/router'
import './style.css'
import { startThemeSync } from './lib/theme-sync'

startThemeSync()

createApp(App).use(createAppRouter()).mount('#app')
