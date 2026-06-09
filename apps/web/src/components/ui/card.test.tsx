/**
 * Card component tests
 * Issue #1267 R2-A: shared card shell primitive
 * TDD RED: written before implementation
 */
import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { Card, CardHeader, CardTitle, CardContent } from './card'

describe('Card', () => {
  describe('Rendering', () => {
    it('renders children', () => {
      render(<Card>Hello card</Card>)
      expect(screen.getByText('Hello card')).toBeInTheDocument()
    })

    it('applies rounded-xl, border-border, bg-card classes', () => {
      const { container } = render(<Card>content</Card>)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('rounded-xl')
      expect(el.className).toContain('border')
      expect(el.className).toContain('bg-card')
    })

    it('applies default padding', () => {
      const { container } = render(<Card>content</Card>)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('p-4')
    })

    it('merges custom className', () => {
      const { container } = render(<Card className="my-custom">content</Card>)
      const el = container.firstChild as HTMLElement
      expect(el.className).toContain('my-custom')
    })

    it('passes through HTML attributes', () => {
      render(<Card data-testid="my-card">content</Card>)
      expect(screen.getByTestId('my-card')).toBeInTheDocument()
    })
  })

  describe('CardHeader', () => {
    it('renders children inside CardHeader', () => {
      render(<Card><CardHeader>Header content</CardHeader></Card>)
      expect(screen.getByText('Header content')).toBeInTheDocument()
    })

    it('merges className on CardHeader', () => {
      const { container } = render(<Card><CardHeader className="mb-2">header</CardHeader></Card>)
      const header = container.querySelector('[data-slot="card-header"]') as HTMLElement
      expect(header?.className).toContain('mb-2')
    })
  })

  describe('CardTitle', () => {
    it('renders as h3 heading', () => {
      render(<Card><CardTitle>My Title</CardTitle></Card>)
      expect(screen.getByRole('heading', { level: 3, name: 'My Title' })).toBeInTheDocument()
    })

    it('merges className on CardTitle', () => {
      const { container } = render(<Card><CardTitle className="text-lg">title</CardTitle></Card>)
      const title = container.querySelector('[data-slot="card-title"]') as HTMLElement
      expect(title?.className).toContain('text-lg')
    })
  })

  describe('CardContent', () => {
    it('renders children inside CardContent', () => {
      render(<Card><CardContent>Body text</CardContent></Card>)
      expect(screen.getByText('Body text')).toBeInTheDocument()
    })

    it('merges className on CardContent', () => {
      const { container } = render(<Card><CardContent className="pt-2">body</CardContent></Card>)
      const content = container.querySelector('[data-slot="card-content"]') as HTMLElement
      expect(content?.className).toContain('pt-2')
    })
  })

  describe('Semantic tokens', () => {
    it('uses semantic token classes (no raw palette)', () => {
      const { container } = render(<Card>content</Card>)
      const el = container.firstChild as HTMLElement
      // Should not use raw colors like bg-white, bg-gray-100, etc.
      expect(el.className).not.toMatch(/bg-white|bg-gray-|bg-zinc-|bg-slate-/)
      // Should use semantic bg-card
      expect(el.className).toContain('bg-card')
    })
  })
})
