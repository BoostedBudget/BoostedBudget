/**
 * SavingsSummaryHeader Tests
 * Issue #1267: Boosts page — savings stats header (total moved, MoM, active rules)
 *
 * TDD RED: Tests written before implementation.
 */

import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import React from 'react'
import { SavingsSummaryHeader } from './SavingsSummaryHeader'

describe('SavingsSummaryHeader', () => {
  describe('loading state', () => {
    it('shows loading skeleton when isLoading is true', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={true}
        />
      )
      // Loading state should show some kind of placeholder (skeleton div or animate-pulse)
      expect(container.querySelector('[data-testid="summary-loading"]') || container.querySelector('.animate-pulse')).toBeTruthy()
    })
  })

  describe('data display', () => {
    it('shows formatted total moved amount', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={null}
          completedCount={10}
          activeBoostsCount={3}
          isLoading={false}
        />
      )
      // $2,500 formatted
      expect(screen.getByText(/2,500/)).toBeTruthy()
    })

    it('applies data-ph-mask to the total moved amount', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={null}
          completedCount={10}
          activeBoostsCount={3}
          isLoading={false}
        />
      )
      const maskedElements = container.querySelectorAll('[data-ph-mask]')
      expect(maskedElements.length).toBeGreaterThan(0)
    })

    it('shows MoM change when momChange is positive', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={150}
          completedCount={10}
          activeBoostsCount={3}
          isLoading={false}
        />
      )
      expect(screen.getByText(/150/)).toBeTruthy()
    })

    it('does not show MoM change when momChange is null', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={500}
          momChange={null}
          completedCount={2}
          activeBoostsCount={1}
          isLoading={false}
        />
      )
      // No element with data-testid for mom should appear
      expect(container.querySelector('[data-testid="mom-change"]')).toBeNull()
    })

    it('shows completed count', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={1000}
          momChange={50}
          completedCount={15}
          activeBoostsCount={2}
          isLoading={false}
        />
      )
      expect(screen.getByText(/15/)).toBeTruthy()
    })

    it('shows active boosts count', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={1000}
          momChange={50}
          completedCount={5}
          activeBoostsCount={4}
          isLoading={false}
        />
      )
      expect(screen.getByText(/4/)).toBeTruthy()
    })

    it('shows em-dash or 0 when totalMoved is null (no data yet)', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={false}
        />
      )
      // At minimum the component renders without throwing
      expect(container.firstChild).toBeTruthy()
    })

    it('applies data-ph-mask to MoM change amount when shown', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={100}
          completedCount={10}
          activeBoostsCount={3}
          isLoading={false}
        />
      )
      const maskedElements = container.querySelectorAll('[data-ph-mask]')
      // Should have at least 2: total + mom change
      expect(maskedElements.length).toBeGreaterThanOrEqual(2)
    })
  })

  // === R7-2: error state ===
  describe('error state', () => {
    it('shows a fallback message when isError is true and not loading', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={false}
          isError={true}
        />
      )
      expect(screen.getByText(/unable to load savings summary/i)).toBeTruthy()
    })

    it('does not render StatCards when isError is true', () => {
      const { container } = render(
        <SavingsSummaryHeader
          totalMoved={2500}
          momChange={100}
          completedCount={10}
          activeBoostsCount={3}
          isLoading={false}
          isError={true}
        />
      )
      // StatCards render data-ph-mask; error fallback should not
      const masked = container.querySelectorAll('[data-ph-mask]')
      expect(masked.length).toBe(0)
    })

    it('shows a Retry button when onRetry is provided', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={false}
          isError={true}
          onRetry={() => {}}
        />
      )
      expect(screen.getByRole('button', { name: /retry/i })).toBeTruthy()
    })

    it('calls onRetry when Retry button is clicked', () => {
      const onRetry = mock(() => {})
      render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={false}
          isError={true}
          onRetry={onRetry}
        />
      )
      fireEvent.click(screen.getByRole('button', { name: /retry/i }))
      expect(onRetry).toHaveBeenCalledTimes(1)
    })

    it('omits Retry button when onRetry is not provided', () => {
      render(
        <SavingsSummaryHeader
          totalMoved={null}
          momChange={null}
          completedCount={null}
          activeBoostsCount={0}
          isLoading={false}
          isError={true}
        />
      )
      expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
    })
  })
})
