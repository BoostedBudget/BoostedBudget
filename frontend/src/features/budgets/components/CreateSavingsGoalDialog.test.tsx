/**
 * CreateSavingsGoalDialog Tests
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * TDD RED Phase: Tests for savings goal creation dialog.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CreateSavingsGoalDialog } from './CreateSavingsGoalDialog';
import * as budgetsApi from '../api/budgetsApi';
import * as plaidApi from '../../accounts/api/plaidApi';
import type { Account } from '../../accounts/types';
import type { SavingsGoal } from '../types';

// Mock APIs
vi.mock('../api/budgetsApi');
vi.mock('../../accounts/api/plaidApi');

const mockAccounts: Account[] = [
  {
    id: 'acc-1',
    linked_item_id: 'item-1',
    user_id: 'user-1',
    plaid_account_id: 'plaid-acc-1',
    name: 'Checking Account',
    type: 'depository',
    subtype: 'checking',
    mask: '1234',
    current_balance: 5000,
    currency_code: 'USD',
    institution_name: 'Chase',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'acc-2',
    linked_item_id: 'item-1',
    user_id: 'user-1',
    plaid_account_id: 'plaid-acc-2',
    name: 'Savings Account',
    type: 'depository',
    subtype: 'savings',
    mask: '5678',
    current_balance: 10000,
    currency_code: 'USD',
    institution_name: 'Chase',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'acc-3',
    linked_item_id: 'item-2',
    user_id: 'user-1',
    plaid_account_id: 'plaid-acc-3',
    name: 'Credit Card',
    type: 'credit',
    subtype: 'credit card',
    mask: '9999',
    current_balance: -2000,
    currency_code: 'USD',
    institution_name: 'AmEx',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  },
];

const mockCreatedGoal: SavingsGoal = {
  id: 'goal-1',
  user_id: 'user-1',
  name: 'Vacation Fund',
  description: 'Save for summer vacation',
  target_amount: 5000,
  current_amount: 0,
  currency_code: 'USD',
  account_id: 'acc-2',
  deadline: '2025-06-01',
  is_completed: false,
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
};

describe('CreateSavingsGoalDialog', () => {
  const mockOnCreate = vi.fn();
  const mockOnCancel = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(plaidApi.fetchAccounts).mockResolvedValue({
      accounts: mockAccounts,
      total_count: mockAccounts.length,
    });
    vi.mocked(budgetsApi.createSavingsGoal).mockResolvedValue(mockCreatedGoal);
  });

  // ==========================================================================
  // Rendering Tests
  // ==========================================================================

  describe('Rendering', () => {
    it('renders dialog when isOpen is true', async () => {
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });
      expect(screen.getByText('Create Savings Goal')).toBeInTheDocument();
    });

    it('does not render dialog when isOpen is false', () => {
      render(
        <CreateSavingsGoalDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('displays all required form fields', async () => {
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });
      expect(screen.getByLabelText(/target amount/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/account/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
      expect(screen.getByLabelText(/deadline/i)).toBeInTheDocument();
    });

    it('displays goal templates', async () => {
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByText('Emergency Fund')).toBeInTheDocument();
      });
      expect(screen.getByText('Vacation Fund')).toBeInTheDocument();
      expect(screen.getByText('Home Down Payment')).toBeInTheDocument();
      expect(screen.getByText('Car Purchase')).toBeInTheDocument();
    });

    it('only shows depository accounts in account selector', async () => {
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/account/i)).toBeInTheDocument();
      });

      // Open the account dropdown
      const accountSelect = screen.getByLabelText(/account/i);
      await userEvent.click(accountSelect);

      // Should see depository accounts
      expect(screen.getByText(/Checking Account/)).toBeInTheDocument();
      expect(screen.getByText(/Savings Account/)).toBeInTheDocument();

      // Should NOT see credit account
      expect(screen.queryByText(/Credit Card/)).not.toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Template Selection Tests
  // ==========================================================================

  describe('Template Selection', () => {
    it('pre-fills form when template is selected', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByText('Emergency Fund')).toBeInTheDocument();
      });

      // Click on Emergency Fund template
      await user.click(screen.getByText('Emergency Fund'));

      // Name should be pre-filled
      const nameInput = screen.getByLabelText(/name/i) as HTMLInputElement;
      expect(nameInput.value).toBe('Emergency Fund');
    });

    it('allows editing pre-filled values after template selection', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByText('Vacation Fund')).toBeInTheDocument();
      });

      // Select template
      await user.click(screen.getByText('Vacation Fund'));

      // Modify the name
      const nameInput = screen.getByLabelText(/name/i);
      await user.clear(nameInput);
      await user.type(nameInput, 'Hawaii Trip 2025');

      expect(nameInput).toHaveValue('Hawaii Trip 2025');
    });
  });

  // ==========================================================================
  // Validation Tests
  // ==========================================================================

  describe('Validation', () => {
    it('shows error when name is empty on submit', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /create goal/i })).toBeInTheDocument();
      });

      // Try to submit without filling name
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      expect(screen.getByText(/name is required/i)).toBeInTheDocument();
    });

    it('shows error when target amount is zero or negative', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/target amount/i)).toBeInTheDocument();
      });

      // Fill name but enter 0 for amount
      await user.type(screen.getByLabelText(/name/i), 'Test Goal');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '0');

      await user.click(screen.getByRole('button', { name: /create goal/i }));

      expect(screen.getByText(/amount must be greater than 0/i)).toBeInTheDocument();
    });

    it('shows error when no account is selected', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill name and amount but not account
      await user.type(screen.getByLabelText(/name/i), 'Test Goal');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '1000');

      await user.click(screen.getByRole('button', { name: /create goal/i }));

      expect(screen.getByText(/please select an account/i)).toBeInTheDocument();
    });
  });

  // ==========================================================================
  // Submission Tests
  // ==========================================================================

  describe('Submission', () => {
    it('calls createSavingsGoal with correct payload on submit', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form - same pattern as passing tests
      await user.type(screen.getByLabelText(/name/i), 'Vacation Fund');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '5000');

      // Select account
      const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
      await user.selectOptions(accountSelect, 'acc-2');

      // Submit
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      // API should be called with required fields (description and deadline are undefined when not provided)
      await waitFor(() => {
        expect(budgetsApi.createSavingsGoal).toHaveBeenCalledWith({
          name: 'Vacation Fund',
          description: undefined,
          target_amount: 5000,
          account_id: 'acc-2',
          deadline: undefined,
        });
      });
    }, 60000); // Extended timeout for CI environment

    it('includes optional fields in payload when provided', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill required fields
      await user.type(screen.getByLabelText(/name/i), 'Vacation Fund');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '5000');

      // Fill optional fields
      await user.type(screen.getByLabelText(/description/i), 'Save for summer vacation');

      // Select account
      const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
      await user.selectOptions(accountSelect, 'acc-2');

      // Submit
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      // API should be called with description included
      await waitFor(() => {
        expect(budgetsApi.createSavingsGoal).toHaveBeenCalledWith(
          expect.objectContaining({
            name: 'Vacation Fund',
            description: 'Save for summer vacation',
            target_amount: 5000,
            account_id: 'acc-2',
          })
        );
      });
    }, 60000); // Extended timeout for CI environment

    it('shows loading state during API call', async () => {
      // Make API call slow
      vi.mocked(budgetsApi.createSavingsGoal).mockImplementation(
        () => new Promise((resolve) => setTimeout(() => resolve(mockCreatedGoal), 1000))
      );

      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill minimal valid form
      await user.type(screen.getByLabelText(/name/i), 'Test Goal');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '1000');
      const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
      await user.selectOptions(accountSelect, 'acc-1');

      // Submit
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      // Should show loading state
      await waitFor(() => {
        expect(screen.getByText(/creating/i)).toBeInTheDocument();
      });
    }, 60000); // Extended timeout for CI environment

    it('closes dialog and calls onGoalCreated on successful creation', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form
      await user.type(screen.getByLabelText(/name/i), 'Vacation Fund');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '5000');
      const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
      await user.selectOptions(accountSelect, 'acc-2');

      // Submit
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      await waitFor(() => {
        expect(mockOnCreate).toHaveBeenCalledWith(mockCreatedGoal);
      });
    }, 60000); // Extended timeout for CI environment

    it('shows error message on API failure', async () => {
      vi.mocked(budgetsApi.createSavingsGoal).mockRejectedValue(
        new Error('Failed to create goal')
      );

      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill form
      await user.type(screen.getByLabelText(/name/i), 'Test Goal');
      await user.clear(screen.getByLabelText(/target amount/i));
      await user.type(screen.getByLabelText(/target amount/i), '1000');
      const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
      await user.selectOptions(accountSelect, 'acc-1');

      // Submit
      await user.click(screen.getByRole('button', { name: /create goal/i }));

      await waitFor(() => {
        expect(screen.getByText(/failed to create goal/i)).toBeInTheDocument();
      });
    }, 60000); // Extended timeout for CI environment
  });

  // ==========================================================================
  // Cancel Tests
  // ==========================================================================

  describe('Cancel', () => {
    it('calls onCancel when cancel button clicked', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
      });

      await user.click(screen.getByRole('button', { name: /cancel/i }));

      expect(mockOnCancel).toHaveBeenCalled();
    });

    it('resets form when reopened after cancel', async () => {
      const user = userEvent.setup();
      const { rerender } = render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByLabelText(/name/i)).toBeInTheDocument();
      });

      // Fill some data
      await user.type(screen.getByLabelText(/name/i), 'Test Goal');

      // Close dialog (simulate cancel changing isOpen to false)
      rerender(
        <CreateSavingsGoalDialog
          isOpen={false}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      // Reopen dialog
      rerender(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        const nameInput = screen.getByLabelText(/name/i) as HTMLInputElement;
        expect(nameInput.value).toBe('');
      });
    });
  });

  // ==========================================================================
  // Issue #460: Dialog Close Behavior
  // ==========================================================================

  describe('Dialog Close Behavior (Issue #460)', () => {
    it('does not close when clicking outside the dialog (on backdrop)', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Click on the backdrop (the overlay element)
      // The backdrop is the element with bg-black/25 class
      const backdrop = document.querySelector('.fixed.inset-0.bg-black\\/25');
      if (backdrop) {
        await user.click(backdrop);
      }

      // onCancel should NOT have been called
      expect(mockOnCancel).not.toHaveBeenCalled();

      // Dialog should still be visible
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('closes when X button is clicked', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('dialog')).toBeInTheDocument();
      });

      // Find and click the X button (it's a button with XMarkIcon)
      const closeButtons = screen.getAllByRole('button');
      const xButton = closeButtons.find(btn =>
        btn.querySelector('svg') && !btn.textContent?.includes('Cancel') && !btn.textContent?.includes('Create')
      );

      if (xButton) {
        await user.click(xButton);
      }

      expect(mockOnCancel).toHaveBeenCalled();
    });

    it('closes when Cancel button is clicked', async () => {
      const user = userEvent.setup();
      render(
        <CreateSavingsGoalDialog
          isOpen={true}
          onCancel={mockOnCancel}
          onGoalCreated={mockOnCreate}
        />
      );

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument();
      });

      await user.click(screen.getByRole('button', { name: /cancel/i }));

      expect(mockOnCancel).toHaveBeenCalled();
    });
  });
});
