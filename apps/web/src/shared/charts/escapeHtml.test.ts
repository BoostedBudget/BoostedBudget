import { describe, it, expect } from 'bun:test'
import { escapeHtml } from './escapeHtml'

describe('escapeHtml', () => {
  it('escapes < and >', () => {
    expect(escapeHtml('<img src=x>')).toBe('&lt;img src=x&gt;')
  })

  it('escapes &', () => {
    expect(escapeHtml('a & b')).toBe('a &amp; b')
  })

  it('escapes double quotes', () => {
    expect(escapeHtml('"quoted"')).toBe('&quot;quoted&quot;')
  })

  it('escapes single quotes', () => {
    expect(escapeHtml("it's")).toBe('it&#39;s')
  })

  it('neutralises a script injection payload', () => {
    const payload = '<script>alert(1)</script>'
    const escaped = escapeHtml(payload)
    expect(escaped).not.toContain('<script>')
    expect(escaped).toBe('&lt;script&gt;alert(1)&lt;/script&gt;')
  })

  it('neutralises an img onerror payload', () => {
    const payload = '<img src=x onerror=alert(1)>'
    const escaped = escapeHtml(payload)
    expect(escaped).not.toContain('<img')
  })

  it('leaves safe strings unchanged', () => {
    expect(escapeHtml('Groceries')).toBe('Groceries')
    expect(escapeHtml('Food & Dining')).toBe('Food &amp; Dining')
    expect(escapeHtml('')).toBe('')
  })
})
