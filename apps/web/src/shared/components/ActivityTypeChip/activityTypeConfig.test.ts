import { describe, expect, it } from 'bun:test'
import { ACTIVITY_TYPE_CONFIG, ACTIVITY_TYPES, ACTIVITY_TYPE_FALLBACK } from './activityTypeConfig'

describe('ACTIVITY_TYPES', () => {
  // ACTIVITY_TYPES is a hand-written literal on purpose: deriving it from Object.keys at module
  // level triggers a cross-chunk TDZ error in the Rolldown production bundle. This test is where
  // that derivation is safe, so it is where drift is caught — #1631.
  it('lists exactly the keys of ACTIVITY_TYPE_CONFIG', () => {
    const listed: string[] = [...ACTIVITY_TYPES]
    expect(listed.sort()).toEqual(Object.keys(ACTIVITY_TYPE_CONFIG).sort())
  })

  it('is in the config declaration order, so filter UIs and the chip agree', () => {
    const listed: string[] = [...ACTIVITY_TYPES]
    expect(listed).toEqual(Object.keys(ACTIVITY_TYPE_CONFIG))
  })

  it('includes the buckets #1631 added', () => {
    expect(ACTIVITY_TYPES).toContain('interest')
    expect(ACTIVITY_TYPES).toContain('transfer')
  })

  it('gives every type an icon and a label, so colour is never load-bearing', () => {
    for (const t of ACTIVITY_TYPES) {
      const cfg = ACTIVITY_TYPE_CONFIG[t]
      expect(cfg.label.length).toBeGreaterThan(0)
      expect(cfg.Icon).toBeDefined()
    }
  })

  it('falls back to the Other config for an unknown type', () => {
    expect(ACTIVITY_TYPE_FALLBACK).toBe(ACTIVITY_TYPE_CONFIG.other)
  })
})
