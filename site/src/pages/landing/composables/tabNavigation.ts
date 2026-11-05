export type TabNavigationKey =
  | 'ArrowDown'
  | 'ArrowLeft'
  | 'ArrowRight'
  | 'ArrowUp'
  | 'End'
  | 'Home'

export function nextTabIndex(key: string, current: number, count: number): number | null {
  if (count < 1) return null

  switch (key as TabNavigationKey) {
    case 'ArrowDown':
    case 'ArrowRight':
      return (current + 1) % count
    case 'ArrowLeft':
    case 'ArrowUp':
      return (current - 1 + count) % count
    case 'Home':
      return 0
    case 'End':
      return count - 1
    default:
      return null
  }
}
