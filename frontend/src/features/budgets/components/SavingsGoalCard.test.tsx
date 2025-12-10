/**
 * SavingsGoalCard Component Tests
 *
 * TDD RED: These tests are written first.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SavingsGoalCard } from './SavingsGoalCard';
import type { SavingsGoal } from '../types';

describe('SavingsGoalCard', () => {
  const mockGoal: SavingsGoal = {
    id: 'goal-1',
    user_id: 'user-123',
    name: 'Vacation Fund',
    description: 'Save for summer vacation',
    target_amount: 5000,
    current_amount: 1500,
    currency_code: 'USD',
    account_id: 'account-789',
    deadline: '2025-07-01T00:00:00Z',
    is_completed: false,
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  };

  describe('Rendering', () => {
    it('should render goal name', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.getByText('Vacation Fund')).toBeInTheDocument();
    });

    it('should render goal description', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.getByText('Save for summer vacation')).toBeInTheDocument();
    });

    it('should render target amount', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.getByText('$5,000.00')).toBeInTheDocument();
    });

    it('should render current amount', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.getByText('$1,500.00')).toBeInTheDocument();
    });

    it('should show completed badge when goal is completed', () => {
      const completedGoal = { ...mockGoal, is_completed: true };
      render(<SavingsGoalCard goal={completedGoal} />);

      expect(screen.getByText('Completed')).toBeInTheDocument();
    });

    it('should not show completed badge when goal is not completed', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.queryByText('Completed')).not.toBeInTheDocument();
    });
  });

  describe('Progress Display', () => {
    it('should show progress percentage', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      // 1500 / 5000 * 100 = 30%
      expect(screen.getByText('30%')).toBeInTheDocument();
    });

    it('should show remaining amount', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      // 5000 - 1500 = 3500
      expect(screen.getByText(/\$3,500\.00.*to go/i)).toBeInTheDocument();
    });

    it('should handle 0% progress', () => {
      const noProgressGoal = { ...mockGoal, current_amount: 0 };
      render(<SavingsGoalCard goal={noProgressGoal} />);

      expect(screen.getByText('0%')).toBeInTheDocument();
    });

    it('should handle 100% progress', () => {
      const completedGoal = { ...mockGoal, current_amount: 5000 };
      render(<SavingsGoalCard goal={completedGoal} />);

      expect(screen.getByText('100%')).toBeInTheDocument();
    });
  });

  describe('Deadline Display', () => {
    it('should show deadline when provided', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      // Check deadline section exists
      const deadlineElement = document.querySelector('.savings-goal-card__deadline');
      expect(deadlineElement).toBeInTheDocument();
    });

    it('should not show deadline when not provided', () => {
      const noDeadlineGoal = { ...mockGoal, deadline: undefined };
      render(<SavingsGoalCard goal={noDeadlineGoal} />);

      const deadlineElement = document.querySelector('.savings-goal-card__deadline');
      expect(deadlineElement).not.toBeInTheDocument();
    });
  });

  describe('Interactions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = vi.fn();
      render(<SavingsGoalCard goal={mockGoal} onEdit={onEdit} />);

      const editButton = screen.getByRole('button', { name: /edit/i });
      fireEvent.click(editButton);

      expect(onEdit).toHaveBeenCalledWith(mockGoal);
    });

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = vi.fn();
      render(<SavingsGoalCard goal={mockGoal} onDelete={onDelete} />);

      const deleteButton = screen.getByRole('button', { name: /delete/i });
      fireEvent.click(deleteButton);

      expect(onDelete).toHaveBeenCalledWith('goal-1');
    });

    it('should not show action buttons when callbacks not provided', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument();
    });

    it('should call onAddFunds when add funds button is clicked', () => {
      const onAddFunds = vi.fn();
      render(<SavingsGoalCard goal={mockGoal} onAddFunds={onAddFunds} />);

      const addButton = screen.getByRole('button', { name: /add funds/i });
      fireEvent.click(addButton);

      expect(onAddFunds).toHaveBeenCalledWith(mockGoal);
    });
  });
});
