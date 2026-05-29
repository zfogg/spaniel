// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// Capture the WS handler IssueToast registers.
let captured: ((ev: unknown) => void) | null = null
vi.mock('@/lib/ws', () => ({
  useWS: (onEvent: (ev: unknown) => void) => { captured = onEvent },
}))

const toastCustom = vi.fn()
vi.mock('sonner', () => ({
  toast: { custom: (...a: unknown[]) => toastCustom(...a), dismiss: vi.fn() },
}))

import IssueToast from './IssueToast'

describe('IssueToast', () => {
  beforeEach(() => { captured = null; toastCustom.mockClear() })
  afterEach(cleanup)

  it('fires a sonner toast on an issue event and ignores others', () => {
    render(<MemoryRouter><IssueToast /></MemoryRouter>)
    expect(captured).toBeTypeOf('function')

    captured!({ type: 'span', timestamp_ns: 0, payload: {} })
    expect(toastCustom).not.toHaveBeenCalled()

    captured!({ type: 'issue', timestamp_ns: 0, payload: { traceId: 't1', kind: 'n_plus_one', count: 47, fingerprint: 'fp', wastedNs: 1 } })
    expect(toastCustom).toHaveBeenCalledTimes(1)
    expect(toastCustom.mock.calls[0][1]).toMatchObject({ duration: 5000 })
  })
})
