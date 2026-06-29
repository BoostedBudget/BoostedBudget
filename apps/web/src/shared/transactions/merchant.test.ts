import { describe, it, expect } from 'bun:test'
import { getMerchantDisplayName } from './merchant'

describe('getMerchantDisplayName', () => {
  it('prefers effective_merchant_name when present', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: 'norm', effective_merchant_name: 'override' })).toBe('override')
  })

  it('falls back to merchant_name when effective_merchant_name is null', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: 'norm', effective_merchant_name: null })).toBe('norm')
  })

  it('falls back to merchant_name when effective_merchant_name is undefined', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: 'norm' })).toBe('norm')
  })

  it('falls back to name when merchant_name is null and effective is absent', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: null })).toBe('raw')
  })

  it('falls back to name when both merchant fields are null', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: null, effective_merchant_name: null })).toBe('raw')
  })

  it('falls back to name when no merchant fields provided', () => {
    expect(getMerchantDisplayName({ name: 'raw' })).toBe('raw')
  })

  it('treats empty string effective_merchant_name as falsy and falls through', () => {
    // The || operator treats empty string as falsy — consistent with the existing inline behaviour
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: 'norm', effective_merchant_name: '' })).toBe('norm')
  })

  it('treats empty string merchant_name as falsy and falls through to name', () => {
    expect(getMerchantDisplayName({ name: 'raw', merchant_name: '', effective_merchant_name: null })).toBe('raw')
  })

  it('returns empty string when name is empty (name is always present per type)', () => {
    // name: string is required — an empty name is passed through unchanged (no further fallback exists)
    expect(getMerchantDisplayName({ name: '', merchant_name: null, effective_merchant_name: null })).toBe('')
  })
})
