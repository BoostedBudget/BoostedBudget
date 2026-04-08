import { describe, it, expect, beforeEach } from 'vitest'

describe('FOUC prevention script logic', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  function applyTheme() {
    const stored = localStorage.getItem('bb-theme')
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    if (stored === 'dark' || (stored !== 'light' && prefersDark)) {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }
  }

  it('adds dark class when localStorage is "dark"', () => {
    localStorage.setItem('bb-theme', 'dark')
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not add dark class when localStorage is "light"', () => {
    localStorage.setItem('bb-theme', 'light')
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('does not add dark class when no preference and system is light', () => {
    applyTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
