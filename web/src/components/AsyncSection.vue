<script setup lang="ts">
// Wraps content that depends on an async load, rendering the appropriate
// loading / error / empty / content state.
withDefaults(
  defineProps<{
    loading: boolean
    error: string | null
    empty?: boolean
    emptyLabel?: string
  }>(),
  {
    empty: false,
    emptyLabel: 'Nothing to show.',
  },
)
</script>

<template>
  <div class="async">
    <p v-if="loading" class="async__state async__state--loading">Loading…</p>
    <p v-else-if="error" class="async__state async__state--error" role="alert">{{ error }}</p>
    <p v-else-if="empty" class="async__state async__state--empty">{{ emptyLabel }}</p>
    <slot v-else />
  </div>
</template>
