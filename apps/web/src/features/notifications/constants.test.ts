/**
 * NOTIFICATION_TYPE_CONFIG tests
 * Issue #972: B3 — verify transfer_suggestion_match has a config entry so the
 * fuzzy-confirm notification renders with the correct icon/urgency/label/category
 * instead of falling back to defaults.
 */

import { describe, it, expect } from 'bun:test'
import { NOTIFICATION_TYPE_CONFIG } from './constants'

describe('NOTIFICATION_TYPE_CONFIG', () => {
  it('includes transfer_suggestion_match with required fields', () => {
    const config = NOTIFICATION_TYPE_CONFIG['transfer_suggestion_match']
    expect(config).toBeDefined()
    expect(config.icon).toBeDefined()
    expect(config.urgency).toBeDefined()
    expect(config.category).toBeDefined()
    expect(config.defaultLabel).toBeDefined()
  })

  it('transfer_suggestion_match has correct values for fuzzy-match confirm UI', () => {
    const config = NOTIFICATION_TYPE_CONFIG['transfer_suggestion_match']
    // Fuzzy-match is high urgency (user needs to confirm an amount before it's finalized).
    expect(config.urgency).toBe('high')
    // Belongs to the savings-suggestion category.
    expect(config.category).toBe('suggestion')
    // Label should describe the confirm action.
    expect(config.defaultLabel).toContain('Confirm')
  })

  it('also has the original transfer_suggestion entry (not removed)', () => {
    const config = NOTIFICATION_TYPE_CONFIG['transfer_suggestion']
    expect(config).toBeDefined()
    expect(config.urgency).toBe('medium')
    expect(config.category).toBe('suggestion')
  })

  it('all entries have icon, urgency, category, and defaultLabel', () => {
    for (const [type, config] of Object.entries(NOTIFICATION_TYPE_CONFIG)) {
      expect(config.icon, `${type} must have icon`).toBeDefined()
      expect(config.urgency, `${type} must have urgency`).toBeDefined()
      expect(config.category, `${type} must have category`).toBeDefined()
      expect(config.defaultLabel, `${type} must have defaultLabel`).toBeDefined()
    }
  })

  // #933: subscription_resumed removed (pause/resume unsupported).
  // Remapped to subscription_activated in migration 000104.
  it('does not include subscription_resumed (#933: pause/resume unsupported)', () => {
    expect(NOTIFICATION_TYPE_CONFIG['subscription_resumed']).toBeUndefined()
  })
})
