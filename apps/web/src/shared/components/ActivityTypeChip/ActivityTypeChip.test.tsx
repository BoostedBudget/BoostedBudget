import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { ActivityTypeChip } from './ActivityTypeChip'

describe('ActivityTypeChip', () => {
  describe('known activity types render correct label', () => {
    const cases = [
      ['buy', 'Buy'],
      ['sell', 'Sell'],
      ['dividend', 'Dividend'],
      ['reinvest', 'Reinvest'],
      ['fee', 'Fee'],
      ['cash', 'Cash'],
      ['other', 'Other'],
    ] as const

    for (const [type, label] of cases) {
      it(`renders "${label}" for type "${type}"`, () => {
        render(<ActivityTypeChip activityType={type} />)
        expect(screen.getByText(label)).toBeTruthy()
      })
    }
  })

  describe('each type renders an icon element', () => {
    const types = ['buy', 'sell', 'dividend', 'reinvest', 'fee', 'cash', 'other'] as const

    for (const type of types) {
      it(`renders an svg icon for type "${type}"`, () => {
        const { container } = render(<ActivityTypeChip activityType={type} />)
        const svg = container.querySelector('svg')
        expect(svg).not.toBeNull()
      })
    }
  })

  describe('fallback for unknown / nullish values', () => {
    it('falls back to "Other" for an unknown string value', () => {
      render(<ActivityTypeChip activityType="xyzzy" />)
      expect(screen.getByText('Other')).toBeTruthy()
    })

    it('falls back to "Other" for null', () => {
      render(<ActivityTypeChip activityType={null} />)
      expect(screen.getByText('Other')).toBeTruthy()
    })

    it('falls back to "Other" for undefined', () => {
      render(<ActivityTypeChip activityType={undefined} />)
      expect(screen.getByText('Other')).toBeTruthy()
    })
  })

  describe('size prop', () => {
    it('renders with size="sm" (default)', () => {
      const { container } = render(<ActivityTypeChip activityType="buy" />)
      expect(container.firstChild).toBeTruthy()
    })

    it('renders with size="default" without error', () => {
      const { container } = render(<ActivityTypeChip activityType="buy" size="default" />)
      expect(container.firstChild).toBeTruthy()
    })
  })

  describe('no raw hex colors in rendered output', () => {
    it('does not contain raw hex color values in className attributes', () => {
      const { container } = render(<ActivityTypeChip activityType="buy" />)
      const html = container.innerHTML
      // Raw hex colors like #fff, #3b82f6, etc. should not appear in class names
      expect(html).not.toMatch(/class="[^"]*#[0-9a-fA-F]{3,6}/)
    })
  })
})
