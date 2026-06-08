/** Formats a unix (seconds) timestamp for display, or "never" when zero. */
export function formatLastSeen(unixSeconds: number): string {
  if (!unixSeconds) {
    return 'never'
  }
  return new Date(unixSeconds * 1000).toLocaleString()
}

/**
 * Human label for a wireless-backhaul mode (matches models.BackhaulMode*):
 * '802.11s' mesh, 'multi_ap' wired multi-AP, otherwise unknown.
 */
export function backhaulModeLabel(mode?: string): string {
  switch (mode) {
    case '802.11s':
      return '802.11s mesh'
    case 'multi_ap':
      return 'Wired multi-AP'
    default:
      return 'Unknown'
  }
}
