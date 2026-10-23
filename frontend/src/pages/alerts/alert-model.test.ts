import { expect, it } from 'vitest'
import { ruleState, emptyRule } from './alert-model'
import { type AlertRule } from '@/lib/api'
it('moves acknowledged incidents out of firing while a fresh event demands attention', () => {
  const rule = {
    ...emptyRule(),
    instances: [{ state: 'firing', acknowledged_at: 123 }],
  } as AlertRule
  expect(ruleState(rule)).toBe('acknowledged')
  rule.instances?.push({ state: 'firing', acknowledged_at: null } as NonNullable<
    AlertRule['instances']
  >[number])
  expect(ruleState(rule)).toBe('firing')
})
