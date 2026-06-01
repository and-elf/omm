import { describe, expect, it } from 'vitest'
import { useAsync } from './useAsync'

const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('useAsync', () => {
  it('exposes loading then data on success', async () => {
    const { data, error, loading, run } = useAsync(() => Promise.resolve(42))

    const promise = run()
    expect(loading.value).toBe(true)

    await promise
    await flush()

    expect(loading.value).toBe(false)
    expect(data.value).toBe(42)
    expect(error.value).toBeNull()
  })

  it('captures the error message on failure', async () => {
    const { data, error, loading, run } = useAsync(() => Promise.reject(new Error('nope')))

    await run()
    await flush()

    expect(loading.value).toBe(false)
    expect(data.value).toBeNull()
    expect(error.value).toBe('nope')
  })

  it('clears a previous error on a successful retry', async () => {
    let shouldFail = true
    const { error, run } = useAsync(() => (shouldFail ? Promise.reject(new Error('x')) : Promise.resolve(1)))

    await run()
    await flush()
    expect(error.value).toBe('x')

    shouldFail = false
    await run()
    await flush()
    expect(error.value).toBeNull()
  })
})
