import { ref, type Ref } from 'vue'

import { ApiError } from '@/api/client'

export interface UseAsync<T> {
  data: Ref<T | null>
  error: Ref<string | null>
  loading: Ref<boolean>
  run: () => Promise<void>
}

/**
 * Wraps an async function, tracking loading / data / error reactive state.
 *
 * Each call to `run` resets the error, flips `loading`, and stores either the
 * resolved value or a human-readable error message.
 */
export function useAsync<T>(fn: () => Promise<T>): UseAsync<T> {
  const data = ref<T | null>(null) as Ref<T | null>
  const error = ref<string | null>(null)
  const loading = ref(false)

  const run = async () => {
    loading.value = true
    error.value = null
    try {
      data.value = await fn()
    } catch (err) {
      data.value = null
      error.value = toMessage(err)
    } finally {
      loading.value = false
    }
  }

  return { data, error, loading, run }
}

function toMessage(err: unknown): string {
  if (err instanceof ApiError || err instanceof Error) {
    return err.message
  }
  return String(err)
}
