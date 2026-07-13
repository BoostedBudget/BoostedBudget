/**
 * RoleBadge component tests
 * Issue #1370: Household feature frontend scaffold
 * TDD RED: written before implementation
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { RoleBadge } from './RoleBadge'

describe('RoleBadge', () => {
  describe('Rendering', () => {
    it('renders "Owner" label for owner role', () => {
      render(<RoleBadge role="owner" />)
      expect(screen.getByText('Owner')).toBeInTheDocument()
    })

    it('renders "Member" label for member role', () => {
      render(<RoleBadge role="member" />)
      expect(screen.getByText('Member')).toBeInTheDocument()
    })
  })

  describe('Badge Variants', () => {
    it('applies owner variant class for owner role', () => {
      const { container } = render(<RoleBadge role="owner" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('bg-primary')
      expect(badge.className).toContain('text-primary-foreground')
    })

    it('applies member variant class for member role', () => {
      const { container } = render(<RoleBadge role="member" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('bg-secondary')
      expect(badge.className).toContain('text-secondary-foreground')
    })
  })

  describe('Semantic tokens (no raw colors)', () => {
    it('uses semantic token classes (no raw palette)', () => {
      const { container } = render(<RoleBadge role="owner" />)
      const badge = container.firstChild as HTMLElement
      // Should not use raw colors like bg-blue-500, bg-blue-600, etc.
      expect(badge.className).not.toMatch(/bg-blue-|bg-red-|bg-green-|bg-yellow-|bg-gray-/)
      // Should use semantic tokens
      expect(badge.className).toMatch(/bg-(primary|secondary)/)
    })
  })

  describe('Size prop', () => {
    it('applies default size when not specified', () => {
      const { container } = render(<RoleBadge role="owner" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('text-xs')
      expect(badge.className).toContain('px-2')
    })

    it('applies custom size when specified', () => {
      const { container } = render(<RoleBadge role="owner" size="lg" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('text-sm')
    })
  })

  describe('HTML attributes', () => {
    it('merges custom className', () => {
      const { container } = render(<RoleBadge role="owner" className="my-custom" />)
      const badge = container.firstChild as HTMLElement
      expect(badge.className).toContain('my-custom')
    })

    it('passes through data-testid', () => {
      render(<RoleBadge role="owner" data-testid="role-badge-owner" />)
      expect(screen.getByTestId('role-badge-owner')).toBeInTheDocument()
    })
  })

  describe('Variant prop handling', () => {
    it('does not leak variant attribute onto DOM element', () => {
      const { container } = render(<RoleBadge role="owner" />)
      const badge = container.firstChild as HTMLElement
      // Variant should never appear as a DOM attribute since it's omitted from props type
      expect(badge.getAttribute('variant')).toBeNull()
      expect(badge.hasAttribute('variant')).toBe(false)
    })

    it('correctly computes variant from role prop only', () => {
      const { container: ownerContainer } = render(<RoleBadge role="owner" />)
      const ownerBadge = ownerContainer.firstChild as HTMLElement
      expect(ownerBadge.className).toContain('bg-primary')

      const { container: memberContainer } = render(
        <RoleBadge role="member" />
      )
      const memberBadge = memberContainer.firstChild as HTMLElement
      expect(memberBadge.className).toContain('bg-secondary')
    })
  })
})
