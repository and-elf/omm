import { createApp } from 'vue'

import App from './App.vue'
import { initNative } from './native'
import { router } from './router'
import './styles.css'

// Activate native capabilities (mDNS, …) when running inside a Capacitor shell;
// a no-op in the browser PWA. Fire-and-forget so it never blocks first paint.
void initNative()

createApp(App).use(router).mount('#app')
