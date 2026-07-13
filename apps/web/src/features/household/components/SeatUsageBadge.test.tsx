/**
 * SeatUsageBadge component tests
 * Issue #1370: Household feature frontend scaffold
 * TDD RED: written before implementation
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { SeatUsageBadge } from './SeatUsageBadge'

describe('SeatUsageBadge', () => {
  describe('Label rendering', () => {
    it('renders used/total format correctly', () => {
      render(<SeatUsageBadge used={2} total={3} />)
      expect(screen.getByText('2/3')).toBeInTheDocument()
    })

    it('renders 1/1 when fully occupied', () => {
      render(<SeatUsageBadge used={1} total={1} />)
      expect(screen.getByText('1/1')).toBeInTheDocument()
    })

    it('renders 0/5 when no seats used', () => {
      render(<SeatUsageBadge used={0} total={5} />)
      expect(screen.getByText('0/5')).toBeInTheDocument()
    })

    it('renders over-limit state (5/4)', () => {
      render(<SeatUsageBadge used={5} total={4} />)
      expect(screen.getByText('5/4')).toBeInTheDocument()
    })
  })

  describe('State determination', () => {
    it('applies available variant when seats available', () => {
      const { container } = render(<SeatUsageBadge used={1} total={3} />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('bg-success')
      expect(badge.className).toContain('text-success-foreground')
    })

    it('applies full variant when all seats used', () => {
      const { container } = render(<SeatUsageBadge used={3} total={3} />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('bg-warning')
      expect(badge.className).toContain('text-warning-foreground')
    })

    it('applies over-limit variant when used exceeds total', () => {
      const { container } = render(<SeatUsageBadge used={4} total={3} />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('bg-destructive')
      expect(badge.className).toContain('text-destructive-foreground')
    })
  })

  describe('Semantic tokens (no raw colors)', () => {
    it('uses semantic token classes (no raw palette)', () => {
      const { container } = render(<SeatUsageBadge used={1} total={3} />)
      const badge = container.firstChild as HTMLElement
      // Should not use raw colors
      expect(badge.className).not.toMatch(/bg-blue-|bg-red-|bg-green-|bg-yellow-|bg-gray-/)
      // Should use semantic tokens
      expect(badge.className).toMatch(/bg-(success|warning|destructive)/)
    })
  })

  describe('Size prop', () => {
    it('applies default size when not specified', () => {
      const { container } = render(<SeatUsageBadge used={1} total={3} />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('text-xs')
      expect(badge.className).toContain('px-2')
    })

    it('applies custom size when specified', () => {
      const { container } = render(<SeatUsageBadge used={1} total={3} size="lg" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('text-sm')
    })
  })

  describe('HTML attributes', () => {
    it('merges custom className', () => {
      const { container } = render(
        <SeatUsageBadge used={1} total={3} className="my-custom" />
      )
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('my-custom')
    })

    it('passes through data-testid', () => {
      render(<SeatUsageBadge used={1} total={3} data-testid="seat-badge" />)
      expect(screen.getByTestId('seat-badge')).toBeInTheDocument()
    })
  })
})
