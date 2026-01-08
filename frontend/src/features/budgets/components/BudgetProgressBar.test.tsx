/**
 * BudgetProgressBar Component Tests - Issue #575
 * Tests the visual progress bar for budget usage
 */

import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { BudgetProgressBar } from './BudgetProgressBar'

describe('BudgetProgressBar', () => {
  describe('Rendering', () => {
    it('should render progress bar with percentage', () => {
      render(<BudgetProgressBar spent={500} total={1000} />)

      expect(screen.getByRole('progressbar')).toBeInTheDocument()
      expect(screen.getByText('50%')).toBeInTheDocument()
    })

    it('should render spent and total amounts', () => {
      render(<BudgetProgressBar spent={500} total={1000} currencyCode="USD" />)

      expect(screen.getByText('$500.00')).toBeInTheDocument()
      expect(screen.getByText(/\$1,000\.00/)).toBeInTheDocument()
    })

    it('should render with default currency (USD)', () => {
      render(<BudgetProgressBar spent={250} total={500} />)

      expect(screen.getByText('$250.00')).toBeInTheDocument()
    })
  })

  describe('Progress Calculation', () => {
    it('should calculate 0% when nothing spent', () => {
      render(<BudgetProgressBar spent={0} total={1000} />)

      expect(screen.getByText('0%')).toBeInTheDocument()
    })

    it('should calculate 100% when fully spent', () => {
      render(<BudgetProgressBar spent={1000} total={1000} />)

      expect(screen.getByText('100%')).toBeInTheDocument()
    })

    it('should calculate correct percentage for partial spending', () => {
      render(<BudgetProgressBar spent={750} total={1000} />)

      expect(screen.getByText('75%')).toBeInTheDocument()
    })

    it('should show over 100% when over budget', () => {
      render(<BudgetProgressBar spent={1500} total={1000} />)

      expect(screen.getByText('150%')).toBeInTheDocument()
    })

    it('should handle zero total gracefully', () => {
      render(<BudgetProgressBar spent={0} total={0} />)

      expect(screen.getByText('0%')).toBeInTheDocument()
    })
  })

  describe('Status Indicators', () => {
    it('should have normal status when under 75%', () => {
      const { container } = render(<BudgetProgressBar spent={500} total={1000} />)

      expect(container.querySelector('.budget-progress--normal')).toBeInTheDocument()
    })

    it('should have warning status between 75% and 100%', () => {
      const { container } = render(<BudgetProgressBar spent={800} total={1000} />)

      expect(container.querySelector('.budget-progress--warning')).toBeInTheDocument()
    })

    it('should have danger status when at or over 100%', () => {
      const { container } = render(<BudgetProgressBar spent={1000} total={1000} />)

      expect(container.querySelector('.budget-progress--danger')).toBeInTheDocument()
    })

    it('should have danger status when over budget', () => {
      const { container } = render(<BudgetProgressBar spent={1200} total={1000} />)

      expect(container.querySelector('.budget-progress--danger')).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have correct aria attributes', () => {
      render(<BudgetProgressBar spent={500} total={1000} />)

      const progressbar = screen.getByRole('progressbar')
      expect(progressbar).toHaveAttribute('aria-valuenow', '50')
      expect(progressbar).toHaveAttribute('aria-valuemin', '0')
      expect(progressbar).toHaveAttribute('aria-valuemax', '100')
    })

    it('should have accessible label', () => {
      render(<BudgetProgressBar spent={500} total={1000} label="Monthly Budget" />)

      const progressbar = screen.getByRole('progressbar')
      expect(progressbar).toHaveAttribute('aria-label', 'Monthly Budget')
    })
  })

  describe('Size Variants', () => {
    it('should render small variant', () => {
      const { container } = render(<BudgetProgressBar spent={500} total={1000} size="small" />)

      expect(container.querySelector('.budget-progress--small')).toBeInTheDocument()
    })

    it('should render medium variant by default', () => {
      const { container } = render(<BudgetProgressBar spent={500} total={1000} />)

      expect(container.querySelector('.budget-progress--medium')).toBeInTheDocument()
    })

    it('should render large variant', () => {
      const { container } = render(<BudgetProgressBar spent={500} total={1000} size="large" />)

      expect(container.querySelector('.budget-progress--large')).toBeInTheDocument()
    })
  })

  describe('Label Visibility', () => {
    it('should show amounts by default', () => {
      render(<BudgetProgressBar spent={500} total={1000} />)

      expect(screen.getByText('$500.00')).toBeInTheDocument()
    })

    it('should hide amounts when showAmounts is false', () => {
      render(<BudgetProgressBar spent={500} total={1000} showAmounts={false} />)

      expect(screen.queryByText('$500.00')).not.toBeInTheDocument()
    })

    it('should still show percentage when amounts are hidden', () => {
      render(<BudgetProgressBar spent={500} total={1000} showAmounts={false} />)

      expect(screen.getByText('50%')).toBeInTheDocument()
    })
  })
})
