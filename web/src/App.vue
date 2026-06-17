<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'

import { api } from '@/api/client'

const route = useRoute()
const router = useRouter()

// Some routes (the first-boot wizard) render full-screen without the app chrome.
const showChrome = computed(() => route.meta.chrome !== false)

const links = [
  { to: '/', label: 'Dashboard' },
  { to: '/homes', label: 'Homes' },
  { to: '/enroll', label: 'Enroll' },
  { to: '/onboard', label: 'Add Node' },
  { to: '/nodes', label: 'Nodes' },
  { to: '/topology', label: 'Topology' },
  { to: '/settings', label: 'Settings' },
]

// On first load, send the operator through onboarding if the device is not set up.
onMounted(async () => {
  if (route.name === 'setup') return
  try {
    const setup = await api.getSetup()
    if (!setup.setup_complete) {
      router.replace('/setup')
    }
  } catch {
    // If the daemon is unreachable, stay put; views will surface the error.
  }
})
</script>

<template>
  <div class="app">
    <header v-if="showChrome" class="app__bar">
      <span class="app__brand">OMM</span>
      <nav class="app__nav">
        <ul class="app__menu">
          <li v-for="link in links" :key="link.to" class="app__item">
            <RouterLink :to="link.to" class="app__link">{{ link.label }}</RouterLink>
          </li>
        </ul>
      </nav>
    </header>
    <main :class="showChrome ? 'app__main' : ''">
      <RouterView />
    </main>
  </div>
</template>
