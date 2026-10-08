// @vitest-environment jsdom
import { renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { useNewItemIDs } from './use-new-item-ids'

describe('useNewItemIDs', () => {
  it('does not animate the initial result but identifies newly arrived records', () => {
    const { result, rerender } = renderHook(({ ids }) => useNewItemIDs(ids, (id) => id), {
      initialProps: { ids: ['one', 'two'] },
    })

    expect(result.current).toEqual(new Set())

    rerender({ ids: ['three', 'one', 'two'] })

    expect(result.current).toEqual(new Set(['three']))
  })
})
