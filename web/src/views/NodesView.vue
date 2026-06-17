<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

import { RouterLink } from 'vue-router'

import { api, ApiClient } from '@/api/client'
import AsyncSection from '@/components/AsyncSection.vue'
import { useAsync } from '@/composables/useAsync'
import { formatLastSeen } from '@/utils/format'

const props = withDefaults(defineProps<{ client?: ApiClient }>(), {
  client: () => api,
})

const { data: nodes, error, loading, run } = useAsync(() => props.client.listNodes())

// Selected node IDs for bulk removal. Pruned to the currently-listed nodes after
// each refresh so a deleted (or vanished) node never lingers as a phantom
// selection.
const selected = ref<Set<string>>(new Set())
const removing = ref(false)
const rowError = ref<string | null>(null)

const selectedCount = computed(() => selected.value.size)
const allSelected = computed(
  () => !!nodes.value && nodes.value.length > 0 && selected.value.size === nodes.value.length,
)

function isSelected(id: string): boolean {
  return selected.value.has(id)
}

function toggle(id: string, on: boolean) {
  const next = new Set(selected.value)
  if (on) next.add(id)
  else next.delete(id)
  selected.value = next
}

function toggleAll(on: boolean) {
  selected.value = on ? new Set((nodes.value ?? []).map((n) => n.id)) : new Set()
}

async function refresh() {
  await run()
  // Keep only selections that still exist after the reload.
  const live = new Set((nodes.value ?? []).map((n) => n.id))
  selected.value = new Set([...selected.value].filter((id) => live.has(id)))
}

// Remove every selected node. Deletions run sequentially so a mid-batch failure
// surfaces with a clear message and the rest of the list is left intact; a 404
// (already gone) is treated as success since the goal — that node absent — holds.
async function removeSelected() {
  const ids = [...selected.value]
  if (ids.length === 0) return
  if (!window.confirm(`Remove ${ids.length} node${ids.length === 1 ? '' : 's'}? This cannot be undone.`)) {
    return
  }
  removing.value = true
  rowError.value = null
  try {
    for (const id of ids) {
      try {
        await props.client.deleteNode(id)
      } catch (err) {
        if (!(err && typeof err === 'object' && 'status' in err && err.status === 404)) {
          throw err
        }
      }
    }
  } catch (err) {
    rowError.value = err instanceof Error ? err.message : String(err)
  } finally {
    removing.value = false
    await refresh()
  }
}

onMounted(refresh)
</script>

<template>
  <section class="view">
    <header class="view__header">
      <h2>Nodes</h2>
      <div class="view__actions">
        <button
          class="btn btn--small btn--danger"
          data-test="delete-selected"
          :disabled="selectedCount === 0 || removing"
          @click="removeSelected"
        >
          Delete selected{{ selectedCount ? ` (${selectedCount})` : '' }}
        </button>
        <button class="btn" :disabled="loading || removing" @click="refresh">Refresh</button>
      </div>
    </header>

    <AsyncSection
      :loading="loading"
      :error="error"
      :empty="!nodes || nodes.length === 0"
      empty-label="No nodes enrolled yet."
    >
      <table class="table">
        <thead>
          <tr>
            <th class="table__check">
              <input
                type="checkbox"
                aria-label="Select all nodes"
                data-test="select-all"
                :checked="allSelected"
                @change="toggleAll(($event.target as HTMLInputElement).checked)"
              />
            </th>
            <th>Serial</th>
            <th>Current home</th>
            <th>Trusted homes</th>
            <th>Last seen</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="node in nodes" :key="node.id" data-test="node-row">
            <td class="table__check">
              <input
                type="checkbox"
                :aria-label="`Select ${node.serial || node.id}`"
                data-test="select-node"
                :checked="isSelected(node.id)"
                @change="toggle(node.id, ($event.target as HTMLInputElement).checked)"
              />
            </td>
            <td><RouterLink :to="`/nodes/${node.id}`">{{ node.serial || node.id }}</RouterLink></td>
            <td>{{ node.current_home || '—' }}</td>
            <td>{{ (node.trusted_homes ?? []).length }}</td>
            <td>{{ formatLastSeen(node.last_seen) }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="rowError" class="form__error" role="alert">{{ rowError }}</p>
    </AsyncSection>
  </section>
</template>
