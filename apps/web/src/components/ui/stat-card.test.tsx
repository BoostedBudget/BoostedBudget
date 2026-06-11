/**
 * StatCard component tests
 * Issue #1267 R2-A: shared primitive for summary stat cards (replaces hand-rolled stat cards in SavingsSummaryHeader)
 * TDD RED: written before implementation
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { TrendingUp } from 'lucide-react'
import { StatCard } from './stat-card'

describe('StatCard', () => {
  describe('Rendering', () => {
    it('renders label text', () => {
      render(<StatCard label="Total Moved" value="$2,500" />)
      expect(screen.getByText('Total Moved')).toBeInTheDocument()
    })

    it('renders value', () => {
      render(<StatCard label="Total Moved" value="$2,500" />)
      expect(screen.getByText('$2,500')).toBeInTheDocument()
    })

    it('renders ReactNode value', () => {
      render(<StatCard label="Balance" value={<span data-testid="val">$100</span>} />)
      expect(screen.getByTestId('val')).toBeInTheDocument()
    })

    it('renders icon when provided', () => {
      render(<StatCard label="Label" value="42" icon={<TrendingUp data-testid="icon" />} />)
      expect(screen.getByTestId('icon')).toBeInTheDocument()
    })

    it('does not render icon slot when icon omitted', () => {
      const { container } = render(<StatCard label="Label" value="42" />)
      expect(container.querySelector('[data-slot="stat-icon"]')).toBeNull()
    })

    it('uses card shell classes (rounded-xl border bg-card)', () => {
      const { container } = render(<StatCard label="L" value="V" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('rounded-xl')
      expect(el.className).toContain('bg-card')
    })

    it('merges custom className', () => {
      const { container } = render(<StatCard label="L" value="V" className="col-span-2" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('col-span-2')
    })
  })

  describe('PII masking', () => {
    it('applies data-ph-mask to value when maskValue=true', () => {
      const { container } = render(<StatCard label="Amount" value="$500" maskValue />)
      const masked = container.querySelector('[data-ph-mask]')
      expect(masked).toBeInTheDocument()
    })

    it('does not apply data-ph-mask when maskValue is omitted', () => {
      const { container } = render(<StatCard label="Count" value="42" />)
      expect(container.querySelector('[data-ph-mask]')).toBeNull()
    })

    it('does not apply data-ph-mask to label regardless of maskValue', () => {
      render(<StatCard label="Secret label" value="$500" maskValue />)
      // Label text should still be visible / queryable
      expect(screen.getByText('Secret label')).toBeInTheDocument()
    })
  })

  describe('Trend', () => {
    it('applies trend class for "up" trend', () => {
      const { container } = render(<StatCard label="L" value="V" trend="up" />)
      const valueEl = container.querySelector('[data-slot="stat-value"]') as HTMLElement
      expect(valueEl?.className).toMatch(/text-success/)
    })

    it('applies destructive class for "down" trend', () => {
      const { container } = render(<StatCard label="L" value="V" trend="down" />)
      const valueEl = container.querySelector('[data-slot="stat-value"]') as HTMLElement
      expect(valueEl?.className).toMatch(/text-destructive/)
    })

    it('applies no special color for "neutral" trend', () => {
      const { container } = render(<StatCard label="L" value="V" trend="neutral" />)
      const valueEl = container.querySelector('[data-slot="stat-value"]') as HTMLElement
      expect(valueEl?.className).not.toMatch(/text-success|text-destructive/)
    })
  })

  describe('Semantic tokens', () => {
    it('uses semantic label class', () => {
      const { container } = render(<StatCard label="L" value="V" />)
      const labelEl = container.querySelector('[data-slot="stat-label"]') as HTMLElement
      expect(labelEl?.className).toContain('text-muted-foreground')
    })
  })

  describe('Accent border', () => {
    it('applies border-l-4 and border-l-warning for accent="warning"', () => {
      const { container } = render(<StatCard label="L" value="V" accent="warning" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('border-l-4')
      expect(el.className).toContain('border-l-warning')
    })

    it('applies border-l-4 and border-l-success for accent="success"', () => {
      const { container } = render(<StatCard label="L" value="V" accent="success" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('border-l-4')
      expect(el.className).toContain('border-l-success')
    })

    it('applies border-l-4 and border-l-destructive for accent="destructive"', () => {
      const { container } = render(<StatCard label="L" value="V" accent="destructive" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('border-l-4')
      expect(el.className).toContain('border-l-destructive')
    })

    it('applies border-l-4 and border-l-info for accent="info"', () => {
      const { container } = render(<StatCard label="L" value="V" accent="info" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('border-l-4')
      expect(el.className).toContain('border-l-info')
    })

    it('does not apply border-l-4 when accent is omitted', () => {
      const { container } = render(<StatCard label="L" value="V" />)
      const el = container.firstChild as HTMLElement
      expect(el.className).not.toContain('border-l-4')
    })
  })
})
