import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "bun:test"
import { TrendingUp } from "lucide-react"
import { EmptyState } from "./empty-state"

const TestIcon = <TrendingUp />

describe('EmptyState', () => {
  describe('Rendering', () => {
    it('renders icon and title with required props only', () => {
      render(<EmptyState icon={TestIcon} title="No data" />)
      expect(screen.getByText('No data')).toBeInTheDocument()
    })

    it('renders description when provided', () => {
      render(
        <EmptyState icon={TestIcon} title="No data" description="Connect an account to get started" />
      )
      expect(screen.getByText('Connect an account to get started')).toBeInTheDocument()
    })

    it('does not render description when omitted', () => {
      render(<EmptyState icon={TestIcon} title="No data" />)
      expect(screen.queryByText('Connect an account to get started')).not.toBeInTheDocument()
    })

    it('renders action when provided', () => {
      render(
        <EmptyState
          icon={TestIcon}
          title="No data"
          action={<button>Connect now</button>}
        />
      )
      expect(screen.getByRole('button', { name: 'Connect now' })).toBeInTheDocument()
    })

    it('does not render action when omitted', () => {
      render(<EmptyState icon={TestIcon} title="No data" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })
  })

  describe('Variants', () => {
    it('applies dashed border classes in default size', () => {
      const { container } = render(<EmptyState icon={TestIcon} title="No data" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('border-dashed')
      expect(el.className).toContain('rounded-xl')
    })

    it('does not apply border in compact size', () => {
      const { container } = render(<EmptyState icon={TestIcon} title="No data" size="compact" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).not.toContain('border-dashed')
    })

    it('merges custom className', () => {
      const { container } = render(
        <EmptyState icon={TestIcon} title="No data" className="h-full" />
      )
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('h-full')
    })
  })

  describe('Accessibility', () => {
    it('has role="status"', () => {
      render(<EmptyState icon={TestIcon} title="No data" />)
      expect(screen.getByRole('status')).toBeInTheDocument()
    })

    it('wraps icon with aria-hidden container', () => {
      const { container } = render(<EmptyState icon={TestIcon} title="No data" />)
      const iconWrapper = container.querySelector('[aria-hidden="true"]')
      expect(iconWrapper).toBeInTheDocument()
    })

    it('renders title as h3 heading', () => {
      render(<EmptyState icon={TestIcon} title="No data" />)
      expect(screen.getByRole('heading', { level: 3, name: 'No data' })).toBeInTheDocument()
    })
  })

  describe('Dark mode classes', () => {
    it('uses semantic token classes that auto-adapt to dark mode', () => {
      const { container } = render(<EmptyState icon={TestIcon} title="No data" />)
      const el = container.firstChild as HTMLElement
      // Semantic tokens (bg-muted, border-border, text-muted-foreground) adapt to dark mode
      // via CSS custom properties — no explicit dark: prefix needed
      expect(el.className).toMatch(/bg-muted|border-border|text-muted/)
    })
  })
})
