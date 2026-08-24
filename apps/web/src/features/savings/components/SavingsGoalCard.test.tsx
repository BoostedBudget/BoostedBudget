import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { SavingsGoalCard } from './SavingsGoalCard'
import type { SavingsGoal } from '../types'

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

      expect(screen.getByText('30%')).toBeInTheDocument();
    });

    it('should show remaining amount', () => {
      render(<SavingsGoalCard goal={mockGoal} />);

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

      const deadlineElement = document.querySelector('.savings-goal-card__deadline');
      expect(deadlineElement).toBeInTheDocument();
    });

    it('should not show deadline when not provided', () => {
      const noDeadlineGoal = { ...mockGoal, deadline: undefined };
      render(<SavingsGoalCard goal={noDeadlineGoal} />);

      const deadlineElement = document.querySelector('.savings-goal-card__deadline');
      expect(deadlineElement).not.toBeInTheDocument();
    });

    it('renders the deadline as a local-day formatted date', () => {
      render(<SavingsGoalCard goal={{ ...mockGoal, deadline: '2025-07-01T00:00:00Z' }} />);
      expect(screen.getByText(/Due:.*Jul 1, 2025/)).toBeInTheDocument();
    });
  });

  describe('Interactions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {});
      render(<SavingsGoalCard goal={mockGoal} onEdit={onEdit} />);

      const editButton = screen.getByRole('button', { name: /edit/i });
      fireEvent.click(editButton);

      expect(onEdit).toHaveBeenCalledWith(mockGoal);
    });

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = mock(() => {});
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

    // R3-3 C8: Contributions affordance gated on progress_basis === 'contributions'
    it('hides Contributions button when progress_basis is not "contributions"', () => {
      const manualGoal = { ...mockGoal, progress_basis: 'manual' as const }
      render(<SavingsGoalCard goal={manualGoal} onEdit={mock(() => {})} onDelete={mock(() => {})} />)
      expect(screen.queryByRole('button', { name: /view contributions/i })).not.toBeInTheDocument()
    })

    it('hides Contributions button when progress_basis is undefined (default manual)', () => {
      render(<SavingsGoalCard goal={mockGoal} onEdit={mock(() => {})} onDelete={mock(() => {})} />)
      expect(screen.queryByRole('button', { name: /view contributions/i })).not.toBeInTheDocument()
    })

    it('shows Contributions button when progress_basis is "contributions"', () => {
      const contributionsGoal = { ...mockGoal, progress_basis: 'contributions' as const }
      render(<SavingsGoalCard goal={contributionsGoal} onEdit={mock(() => {})} onDelete={mock(() => {})} />)
      expect(screen.getByRole('button', { name: /view contributions/i })).toBeInTheDocument()
    })

    it('should call onAddFunds when add funds button is clicked', () => {
      const onAddFunds = mock(() => {});
      render(<SavingsGoalCard goal={mockGoal} onAddFunds={onAddFunds} />);

      const addButton = screen.getByRole('button', { name: /add funds/i });
      fireEvent.click(addButton);

      expect(onAddFunds).toHaveBeenCalledWith(mockGoal);
    });
  });

  // Issue #1569 (Task 13, E14): the API's `freshness` flag had zero frontend consumers, so a
  // goal whose linked account is gone showed a permanently frozen number with no explanation.
  // Only the actionable value is surfaced — `fresh`/`stale`/`unknown` are ordinary states and
  // must stay silent.
  describe('disconnected-account notice (freshness)', () => {
    it('explains the frozen amount when the basis account is disconnected', () => {
      render(<SavingsGoalCard goal={mockGoal} freshness="basis_account_disconnected" />)

      // Information must not be carried by colour alone: the badge states the condition in
      // words, and the notice gives the reason and the remedy.
      const badge = screen.getByTestId('savings-goal-disconnected-badge')
      expect(badge).toHaveTextContent('Account disconnected')

      const noticeId = badge.getAttribute('aria-describedby')
      expect(noticeId).toBeTruthy()
      const notice = document.getElementById(noticeId as string)
      expect(notice).not.toBeNull()
      expect(notice).toHaveTextContent(/no longer connected/i)
      expect(notice).toHaveTextContent(/frozen/i)
      expect(notice).toHaveTextContent(/reconnect/i)
    })

    // The 4.5:1 AA contrast measured for this notice is a property of the warning token pair
    // (bg-warning / text-warning-foreground), not of the badge in general. Swapping the
    // variant would silently change the measured colours, so pin the pair itself.
    it('uses the warning token pair whose contrast was measured for AA', () => {
      render(<SavingsGoalCard goal={mockGoal} freshness="basis_account_disconnected" />)
      const badge = screen.getByTestId('savings-goal-disconnected-badge')
      expect(badge.className).toContain('bg-warning')
      expect(badge.className).toContain('text-warning-foreground')
    })

    // ⚠ Scope of this test, stated precisely because the obvious reading is wrong: deleting
    // the explicit aria-hidden from SavingsGoalCard leaves this GREEN. lucide-react's Icon
    // applies `aria-hidden="true"` itself whenever no a11y prop is passed
    // (lucide-react/dist/esm/Icon.mjs:36), so the attribute is guaranteed by the library, not
    // by us. What this DOES catch is someone actively un-hiding the icon (aria-hidden={false}
    // or an added aria-label), which would announce a meaningless second node next to a badge
    // whose own text already says "Account disconnected". Verified by mutation both ways.
    it('leaves the decorative icon hidden from assistive technology', () => {
      render(<SavingsGoalCard goal={mockGoal} freshness="basis_account_disconnected" />)
      const badge = screen.getByTestId('savings-goal-disconnected-badge')
      const icon = badge.querySelector('svg')
      expect(icon).not.toBeNull()
      expect(icon).toHaveAttribute('aria-hidden', 'true')
    })

    it.each([['fresh'], ['stale'], ['unknown'], ['not_applicable']])(
      'stays silent for freshness=%s — those balances are still being updated',
      (value) => {
        render(<SavingsGoalCard goal={mockGoal} freshness={value as never} />)
        expect(screen.queryByTestId('savings-goal-disconnected-badge')).not.toBeInTheDocument()
        expect(screen.queryByText('Account disconnected')).not.toBeInTheDocument()
      },
    )

    it('stays silent when no freshness is supplied at all', () => {
      render(<SavingsGoalCard goal={mockGoal} />)
      expect(screen.queryByTestId('savings-goal-disconnected-badge')).not.toBeInTheDocument()
      expect(screen.queryByText('Account disconnected')).not.toBeInTheDocument()
    })

    // Task 12 lost a Critical to an opacity wash compositing onto the card. The earlier guard
    // here only checked for an `opacity-*` class on this component's own nodes, which misses
    // inline styles and fractional colour alpha (`bg-card/90`). It still cannot see an
    // ancestor wash applied by a parent — SavingsGoalsGrid is not rendered here — so that half
    // is covered by not introducing one rather than by this assertion.
    it('does not dim the card, by class, by colour alpha, or by inline style', () => {
      const { container } = render(
        <SavingsGoalCard goal={mockGoal} freshness="basis_account_disconnected" />,
      )
      for (const el of container.querySelectorAll<HTMLElement>('*')) {
        expect(el.className.toString()).not.toMatch(/(^|\s|:)opacity-/)
        // Tailwind colour-alpha shorthand: bg-card/90, text-foreground/50, …
        expect(el.className.toString()).not.toMatch(/(bg|text|border)-[a-z-]+\/\d/)
        expect(el.style.opacity).toBe('')
      }
    })
  })
});
