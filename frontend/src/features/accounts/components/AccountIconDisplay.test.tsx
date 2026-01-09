/**
 * AccountIconDisplay Component Tests
 *
 * Tests for the icon rendering component that maps AccountIcon identifiers
 * to their corresponding Lucide icons.
 *
 * Issue #307: Account Customization
 * Issue #544: Add unit tests for account customization components
 */

import { describe, it, expect } from 'bun:test'
import { render } from '@testing-library/react'
import { AccountIconDisplay } from './AccountIconDisplay'
import { VALID_ACCOUNT_ICONS } from '../types'
import type { AccountIcon } from '../types'

describe('AccountIconDisplay', () => {
  describe('Icon Rendering', () => {
    // Test each valid icon type renders an SVG element
    it.each(VALID_ACCOUNT_ICONS)('renders %s icon correctly', (iconName) => {
      const { container } = render(<AccountIconDisplay icon={iconName} />)

      // Each Lucide icon renders as an SVG
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
    })

    // Test specific icon mappings to ensure correct icons are rendered
    it('renders wallet icon with correct Lucide component', () => {
      const { container } = render(<AccountIconDisplay icon="wallet" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-wallet')
    })

    it('renders piggy-bank icon with correct Lucide component', () => {
      const { container } = render(<AccountIconDisplay icon="piggy-bank" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-piggy-bank')
    })

    it('renders credit-card icon with correct Lucide component', () => {
      const { container } = render(<AccountIconDisplay icon="credit-card" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-credit-card')
    })

    it('renders bank icon with Building component', () => {
      const { container } = render(<AccountIconDisplay icon="bank" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-building')
    })

    it('renders building icon with Building2 component', () => {
      const { container } = render(<AccountIconDisplay icon="building" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-building-2')
    })

    it('renders chart icon with BarChart3 component', () => {
      const { container } = render(<AccountIconDisplay icon="chart" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      // Lucide renamed bar-chart-3 to chart-column
      expect(svg).toHaveClass('lucide-chart-column')
    })

    it('renders dollar icon with DollarSign component', () => {
      const { container } = render(<AccountIconDisplay icon="dollar" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-dollar-sign')
    })

    it('renders euro icon with Euro component', () => {
      const { container } = render(<AccountIconDisplay icon="euro" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-euro')
    })

    it('renders pound icon with PoundSterling component', () => {
      const { container } = render(<AccountIconDisplay icon="pound" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-pound-sterling')
    })

    it('renders yen icon with JapaneseYen component', () => {
      const { container } = render(<AccountIconDisplay icon="yen" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-japanese-yen')
    })

    it('renders graduation-cap icon with GraduationCap component', () => {
      const { container } = render(<AccountIconDisplay icon="graduation-cap" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-graduation-cap')
    })

    it('renders shopping-cart icon with ShoppingCart component', () => {
      const { container } = render(<AccountIconDisplay icon="shopping-cart" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('lucide-shopping-cart')
    })
  })

  describe('Default Case', () => {
    it('returns null for unknown icon type', () => {
      // Type assertion to test the default case with an invalid value
      const { container } = render(
        <AccountIconDisplay icon={'unknown-icon' as AccountIcon} />
      )
      expect(container.firstChild).toBeNull()
    })

    it('returns null for empty string icon', () => {
      const { container } = render(
        <AccountIconDisplay icon={'' as AccountIcon} />
      )
      expect(container.firstChild).toBeNull()
    })
  })

  describe('Props', () => {
    it('applies className prop to the icon', () => {
      const { container } = render(
        <AccountIconDisplay icon="wallet" className="h-5 w-5 text-blue-500" />
      )
      const svg = container.querySelector('svg')
      expect(svg).toHaveClass('h-5', 'w-5', 'text-blue-500')
    })

    it('applies style prop to the icon', () => {
      const customStyle = { color: 'red', width: '24px' }
      const { container } = render(
        <AccountIconDisplay icon="wallet" style={customStyle} />
      )
      const svg = container.querySelector('svg')
      // Check style attribute directly - happy-dom sets inline styles
      const styleAttr = svg?.getAttribute('style') ?? ''
      expect(styleAttr).toContain('color')
      expect(styleAttr).toContain('width')
    })

    it('applies both className and style props', () => {
      const { container } = render(
        <AccountIconDisplay
          icon="heart"
          className="custom-class"
          style={{ color: 'pink' }}
        />
      )
      const svg = container.querySelector('svg')
      expect(svg).toHaveClass('custom-class')
      // Check style attribute directly - happy-dom sets inline styles
      const styleAttr = svg?.getAttribute('style') ?? ''
      expect(styleAttr).toContain('color')
    })

    it('works without optional props', () => {
      const { container } = render(<AccountIconDisplay icon="star" />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
    })
  })

  describe('Icon Count Verification', () => {
    it('has 25 valid icon types defined', () => {
      // Ensure all icons in VALID_ACCOUNT_ICONS are supported
      expect(VALID_ACCOUNT_ICONS.length).toBe(25)
    })

    it('renders all icons from VALID_ACCOUNT_ICONS', () => {
      // Verify each icon type renders without errors
      VALID_ACCOUNT_ICONS.forEach((iconName) => {
        const { container, unmount } = render(
          <AccountIconDisplay icon={iconName} />
        )
        const svg = container.querySelector('svg')
        expect(svg).toBeInTheDocument()
        unmount()
      })
    })
  })
})
