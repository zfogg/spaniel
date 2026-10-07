// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'

const toastCustom = vi.fn()
vi.mock('sonner', () => ({
  toast: { custom: (...args: unknown[]) => toastCustom(...args), dismiss: vi.fn() },
}))

import { showNotification } from './NotificationToast'

describe('showNotification', () => {
  it('sends one native notification for the same event across tabs', () => {
    const notify = vi.fn()
    Object.defineProperty(window, 'Notification', {
      configurable: true,
      value: Object.assign(notify, { permission: 'granted' }),
    })
    window.localStorage.clear()

    const event = {
      title: 'Alert firing',
      detail: 'warning alert',
      link: '/alerts',
      linkLabel: 'open alerts',
      severity: 'warning' as const,
      dedupeKey: 'alert:rule:repeat:123',
      native: true,
    }
    showNotification(event)
    showNotification(event)

    expect(notify).toHaveBeenCalledTimes(1)
    expect(notify).toHaveBeenCalledWith(
      'Spaniel · Alert firing',
      expect.objectContaining({ tag: 'spaniel-native-notification:alert:rule:repeat:123' }),
    )
  })
})
