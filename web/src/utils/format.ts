/** Formats a unix (seconds) timestamp for display, or "never" when zero. */
export function formatLastSeen(unixSeconds: number): string {
  if (!unixSeconds) {
    return 'never'
  }
  return new Date(unixSeconds * 1000).toLocaleString()
}
