import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeProvider } from '@/shared/theme'
import { ThemeSelector } from './ThemeSelector'

function renderWithTheme(defaultTheme: 'light' | 'dark' | 'system' = 'system') {
  return render(
    <ThemeProvider defaultTheme={defaultTheme}>
      <ThemeSelector />
    </ThemeProvider>
  )
}

describe('ThemeSelector', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  afterEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('renders all three theme options', () => {
    renderWithTheme()

    expect(screen.getByRole('button', { name: /light/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /dark/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /system/i })).toBeInTheDocument()
  })

  it('highlights the active theme option', () => {
    renderWithTheme('dark')

    const darkButton = screen.getByRole('button', { name: /dark/i })
    expect(darkButton).toHaveAttribute('data-active', 'true')

    const lightButton = screen.getByRole('button', { name: /light/i })
    expect(lightButton).toHaveAttribute('data-active', 'false')

    const systemButton = screen.getByRole('button', { name: /system/i })
    expect(systemButton).toHaveAttribute('data-active', 'false')
  })

  it('updates localStorage when Dark is clicked', async () => {
    const user = userEvent.setup()
    renderWithTheme('light')

    await user.click(screen.getByRole('button', { name: /dark/i }))

    expect(localStorage.getItem('bb-theme')).toBe('dark')
  })

  it('updates localStorage when Light is clicked', async () => {
    const user = userEvent.setup()
    renderWithTheme('dark')

    await user.click(screen.getByRole('button', { name: /light/i }))

    expect(localStorage.getItem('bb-theme')).toBe('light')
  })

  it('updates localStorage when System is clicked', async () => {
    const user = userEvent.setup()
    renderWithTheme('dark')

    await user.click(screen.getByRole('button', { name: /system/i }))

    expect(localStorage.getItem('bb-theme')).toBe('system')
  })

  it('shows system hint text when system theme is active', () => {
    renderWithTheme('system')

    expect(screen.getByText(/currently using/i)).toBeInTheDocument()
  })

  it('does not show system hint text when non-system theme is active', () => {
    renderWithTheme('light')

    expect(screen.queryByText(/currently using/i)).not.toBeInTheDocument()
  })

  it('marks newly clicked option as active', async () => {
    const user = userEvent.setup()
    renderWithTheme('light')

    await user.click(screen.getByRole('button', { name: /dark/i }))

    expect(screen.getByRole('button', { name: /dark/i })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('button', { name: /light/i })).toHaveAttribute('data-active', 'false')
  })
})
