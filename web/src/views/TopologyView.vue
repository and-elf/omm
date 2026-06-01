<script setup lang="ts">
// Placeholder for the mesh topology visualization. The backend does not yet
// expose a /topology endpoint; this view will render the graph with
// Cytoscape.js (per the spec) once that data is available.
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Topology</h2>
    </header>
    <div class="placeholder">
      <p>Mesh topology visualization is coming soon.</p>
      <p class="placeholder__hint">
        This view will render node links, RSSI and client mapping with Cytoscape.js once the
        <code>meshd.topology</code> API is available.
      </p>
    </div>
  </section>
</template>
