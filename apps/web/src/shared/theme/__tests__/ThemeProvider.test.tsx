import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, act } from '@testing-library/react'
import { ThemeProvider } from '../ThemeProvider'
import { useTheme } from '../useTheme'

function ThemeConsumer() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <span data-testid="resolved">{resolvedTheme}</span>
      <button onClick={() => setTheme('dark')}>Set Dark</button>
      <button onClick={() => setTheme('light')}>Set Light</button>
      <button onClick={() => setTheme('system')}>Set System</button>
    </div>
  )
}

describe('ThemeProvider', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('defaults to system theme', () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    expect(screen.getByTestId('theme').textContent).toBe('system')
  })

  it('applies dark class when theme is set to dark', async () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Dark').click()
    })
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(screen.getByTestId('resolved').textContent).toBe('dark')
  })

  it('removes dark class when theme is set to light', async () => {
    document.documentElement.classList.add('dark')
    render(
      <ThemeProvider defaultTheme="dark">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Light').click()
    })
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(screen.getByTestId('resolved').textContent).toBe('light')
  })

  it('persists theme to localStorage', async () => {
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    await act(async () => {
      screen.getByText('Set Dark').click()
    })
    expect(localStorage.getItem('bb-theme')).toBe('dark')
  })

  it('reads theme from localStorage on mount', () => {
    localStorage.setItem('bb-theme', 'dark')
    render(
      <ThemeProvider defaultTheme="system">
        <ThemeConsumer />
      </ThemeProvider>
    )
    expect(screen.getByTestId('theme').textContent).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
