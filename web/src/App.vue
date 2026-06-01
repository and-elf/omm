<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'

const links = [
  { to: '/', label: 'Dashboard' },
  { to: '/homes', label: 'Homes' },
  { to: '/enroll', label: 'Enroll' },
  { to: '/nodes', label: 'Nodes' },
  { to: '/topology', label: 'Topology' },
]
</script>

<template>
  <div class="app">
    <header class="app__bar">
      <span class="app__brand">OMM</span>
      <nav class="app__nav">
        <RouterLink v-for="link in links" :key="link.to" :to="link.to" class="app__link">
          {{ link.label }}
        </RouterLink>
      </nav>
    </header>
    <main class="app__main">
      <RouterView />
    </main>
  </div>
</template>
