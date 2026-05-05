import { describe, it, expect } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { SavingsAggregateHeader } from './SavingsAggregateHeader'
import type { SavingsGoal } from '../types'

const usdGoal: SavingsGoal = {
  id: '1',
  user_id: 'u1',
  name: 'Vacation',
  description: null,
  target_amount: 10000,
  current_amount: 2500,
  currency_code: 'USD',
  account_id: 'acc1',
  deadline: null,
  is_completed: false,
  completed_at: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

const usdGoal2: SavingsGoal = {
  id: '2',
  user_id: 'u1',
  name: 'Emergency Fund',
  description: null,
  target_amount: 5000,
  current_amount: 5000,
  currency_code: 'USD',
  account_id: 'acc1',
  deadline: null,
  is_completed: true,
  completed_at: '2024-06-01',
  created_at: '2024-01-01',
  updated_at: '2024-06-01',
}

const eurGoal: SavingsGoal = {
  id: '3',
  user_id: 'u1',
  name: 'Paris Trip',
  description: null,
  target_amount: 2000,
  current_amount: 500,
  currency_code: 'EUR',
  account_id: 'acc2',
  deadline: null,
  is_completed: false,
  completed_at: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

describe('SavingsAggregateHeader', () => {
  describe('single currency', () => {
    it('renders the total saved amount formatted', () => {
      render(<SavingsAggregateHeader goals={[usdGoal]} />)
      const savedEl = screen.getByLabelText('Total saved')
      expect(savedEl.textContent).toContain('2,500')
    })

    it('renders the total target amount formatted', () => {
      render(<SavingsAggregateHeader goals={[usdGoal]} />)
      const targetEl = screen.getByLabelText('Savings target')
      expect(targetEl.textContent).toContain('10,000')
    })

    it('renders the progress percentage', () => {
      render(<SavingsAggregateHeader goals={[usdGoal]} />)
      expect(screen.getByText('25%')).toBeTruthy()
    })

    it('applies data-ph-mask to the saved amount element', () => {
      render(<SavingsAggregateHeader goals={[usdGoal]} />)
      const savedEl = screen.getByLabelText('Total saved')
      expect(savedEl.hasAttribute('data-ph-mask')).toBe(true)
    })

    it('applies data-ph-mask to the target amount element', () => {
      render(<SavingsAggregateHeader goals={[usdGoal]} />)
      const targetEl = screen.getByLabelText('Savings target')
      expect(targetEl.hasAttribute('data-ph-mask')).toBe(true)
    })

    it('shows correct active and completed counts', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, usdGoal2]} />)
      expect(screen.getByLabelText('1 active goals').textContent).toContain('1 active')
      expect(screen.getByLabelText('1 completed goals').textContent).toContain('1 completed')
    })
  })

  describe('multiple currencies', () => {
    it('renders one row per currency', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, eurGoal]} />)
      expect(screen.getByLabelText('USD saved amount')).toBeTruthy()
      expect(screen.getByLabelText('EUR saved amount')).toBeTruthy()
    })

    it('renders correct totals and percentage for each currency', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, eurGoal]} />)
      // USD: 2500/10000 = 25%
      // EUR: 500/2000 = 25%
      const progressBadges = screen.getAllByText('25%')
      expect(progressBadges).toHaveLength(2)
    })

    it('applies data-ph-mask to saved amounts in multi-currency mode', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, eurGoal]} />)
      const usdSaved = screen.getByLabelText('USD saved amount')
      const eurSaved = screen.getByLabelText('EUR saved amount')
      expect(usdSaved.hasAttribute('data-ph-mask')).toBe(true)
      expect(eurSaved.hasAttribute('data-ph-mask')).toBe(true)
    })

    it('applies data-ph-mask to target amounts in multi-currency mode', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, eurGoal]} />)
      const usdTarget = screen.getByLabelText('USD target amount')
      const eurTarget = screen.getByLabelText('EUR target amount')
      expect(usdTarget.hasAttribute('data-ph-mask')).toBe(true)
      expect(eurTarget.hasAttribute('data-ph-mask')).toBe(true)
    })

    it('shows combined active and completed counts', () => {
      render(<SavingsAggregateHeader goals={[usdGoal, usdGoal2, eurGoal]} />)
      expect(screen.getByLabelText('2 active goals').textContent).toContain('2 active')
      expect(screen.getByLabelText('1 completed goals').textContent).toContain('1 completed')
    })
  })

  describe('zero target safety', () => {
    it('shows 0% when target_amount is 0', () => {
      const zeroTarget: SavingsGoal = { ...usdGoal, target_amount: 0, current_amount: 0 }
      render(<SavingsAggregateHeader goals={[zeroTarget]} />)
      expect(screen.getByText('0%')).toBeTruthy()
    })

    it('does not show NaN or Infinity when target is 0', () => {
      const zeroTarget: SavingsGoal = { ...usdGoal, target_amount: 0, current_amount: 500 }
      render(<SavingsAggregateHeader goals={[zeroTarget]} />)
      const text = document.body.textContent ?? ''
      expect(text).not.toContain('NaN')
      expect(text).not.toContain('Infinity')
    })
  })

  describe('empty goals array', () => {
    it('renders the section wrapper with a no-goals message', () => {
      render(<SavingsAggregateHeader goals={[]} />)
      const section = screen.getByRole('region', { name: 'Savings overview' })
      expect(section).toBeTruthy()
      expect(section.textContent).toContain('No savings goals yet')
    })
  })
})
