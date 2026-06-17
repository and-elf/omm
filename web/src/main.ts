import { createApp } from 'vue'

import App from './App.vue'
import { initNative } from './native'
import { router } from './router'
import './styles.css'
import { syncHostTheme } from './theme'

// When embedded in LuCI (same-origin iframe), adopt the host page's theme so
// the manager blends into LuCI's look; a no-op for the standalone PWA. Run
// before mount so the first paint already carries the host colours.
syncHostTheme()

// Activate native capabilities (mDNS, …) when running inside a Capacitor shell;
// a no-op in the browser PWA. Fire-and-forget so it never blocks first paint.
void initNative()

createApp(App).use(router).mount('#app')
