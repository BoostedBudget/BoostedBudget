import { describe, it, expect, mock } from 'bun:test'
import { render, screen, fireEvent } from '@testing-library/react'
import { ReadOnlyProvider } from '@/auth/ReadOnlyContext'
import { RuleTable } from './RuleTable'
import type { AutomationRule } from '@boostedbudget/api-client/models'

const mockRules: AutomationRule[] = [
  {
    id: 'rule-1',
    user_id: 'user-1',
    name: 'Daily Budget Check',
    description: 'Check budget usage daily',
    rule_type: 'budget_alert',
    enabled: true,
    priority: 10,
    execution_count: 5,
    trigger_config: {
      type: 'schedule',
      cron: '0 9 * * *',
      timezone: 'America/New_York',
    },
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-15T00:00:00Z',
    last_executed_at: '2024-01-15T09:00:00Z',
    last_successful_at: '2024-01-15T09:00:00Z',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
  {
    id: 'rule-2',
    user_id: 'user-1',
    name: 'Monthly Savings',
    description: 'Transfer to savings monthly',
    rule_type: 'savings_goal',
    enabled: false,
    priority: 5,
    execution_count: 12,
    trigger_config: {
      type: 'schedule',
      cron: '0 9 1 * *',
      timezone: 'America/New_York',
    },
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-10T00:00:00Z',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
  {
    id: 'rule-3',
    user_id: 'user-1',
    name: 'Event Transfer',
    rule_type: 'transfer_suggestion',
    enabled: true,
    priority: 15,
    execution_count: 3,
    trigger_config: {
      type: 'event',
      event: 'transaction_created',
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
    } as any,
    conditions: [],
    condition_logic: 'AND',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    actions: {} as any,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-05T00:00:00Z',
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } as any,
]

describe('RuleTable', () => {
  describe('Rendering', () => {
    it('should render a table', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should render table headers', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Type')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
      expect(screen.getByText('Trigger')).toBeInTheDocument()
      expect(screen.getByText('Last Run')).toBeInTheDocument()
    })

    it('should render all rules', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.getByText('Daily Budget Check')).toBeInTheDocument()
      expect(screen.getByText('Monthly Savings')).toBeInTheDocument()
      expect(screen.getByText('Event Transfer')).toBeInTheDocument()
    })

    it('should render rule types', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.getByText('Budget Alert')).toBeInTheDocument()
      expect(screen.getByText('Savings Goal')).toBeInTheDocument()
      expect(screen.getByText('Transfer Suggestion')).toBeInTheDocument()
    })

    it('should render enabled/disabled status', () => {
      render(<RuleTable rules={mockRules} />)

      const enabledBadges = screen.getAllByText('Enabled')
      const disabledBadges = screen.getAllByText('Disabled')

      expect(enabledBadges.length).toBe(2)
      expect(disabledBadges.length).toBe(1)
    })

    it('should render trigger types', () => {
      render(<RuleTable rules={mockRules} />)

      const scheduledBadges = screen.getAllByText(/^scheduled$/i)
      const triggerCells = screen.getAllByText(/^event$/i)

      expect(scheduledBadges.length).toBe(2)
      expect(triggerCells.length).toBe(1)
    })

    it('should render last run time for rules with executions', () => {
      render(<RuleTable rules={mockRules} />)

      const lastRunCells = screen.getAllByText(/last run/i)
      expect(lastRunCells.length).toBeGreaterThan(0)
    })

    it('should show "Never" for rules without executions', () => {
      render(<RuleTable rules={mockRules} />)

      const neverCells = screen.getAllByText(/never/i)
      expect(neverCells.length).toBeGreaterThan(0)
    })

    it('should show empty state when no rules', () => {
      render(<RuleTable rules={[]} />)

      expect(screen.getByText(/no rules/i)).toBeInTheDocument()
    })
  })

  describe('Row Actions', () => {
    it('should call onEdit when edit button is clicked', () => {
      const onEdit = mock(() => {})
      render(<RuleTable rules={mockRules} onEdit={onEdit} />)

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      fireEvent.click(editButtons[0])

      expect(onEdit).toHaveBeenCalledWith(mockRules[0])
    })

    it('should call onDelete when delete button is clicked', () => {
      const onDelete = mock(() => {})
      render(<RuleTable rules={mockRules} onDelete={onDelete} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete/i })
      fireEvent.click(deleteButtons[0])

      expect(onDelete).toHaveBeenCalledWith(mockRules[0])
    })

    it('should call onToggle when toggle is clicked', () => {
      const onToggle = mock(() => {})
      render(<RuleTable rules={mockRules} onToggle={onToggle} />)

      const toggles = screen.getAllByRole('switch')
      fireEvent.click(toggles[0])

      expect(onToggle).toHaveBeenCalledWith(mockRules[0], false)
    })

    it('should call onToggle with true when enabling disabled rule', () => {
      const onToggle = mock(() => {})
      render(<RuleTable rules={mockRules} onToggle={onToggle} />)

      const toggles = screen.getAllByRole('switch')
      // Second rule is disabled
      fireEvent.click(toggles[1])

      expect(onToggle).toHaveBeenCalledWith(mockRules[1], true)
    })

    it('should not show actions column when no handlers provided', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.queryByText('Actions')).not.toBeInTheDocument()
    })

    it('should show actions column when handlers provided', () => {
      render(<RuleTable rules={mockRules} onEdit={() => {}} />)

      expect(screen.getByText('Actions')).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should disable actions when loading', () => {
      render(
        <RuleTable
          rules={mockRules}
          onEdit={() => {}}
          onToggle={() => {}}
          loading
        />
      )

      const editButtons = screen.getAllByRole('button', { name: /edit/i })
      const toggles = screen.getAllByRole('switch')

      editButtons.forEach((btn) => expect(btn).toBeDisabled())
      toggles.forEach((toggle) => expect(toggle).toBeDisabled())
    })

    it('should show loading indicator', () => {
      render(<RuleTable rules={mockRules} loading />)

      expect(screen.getByRole('table')).toHaveClass('rule-table--loading')
    })
  })

  describe('Sorting', () => {
    it('makes name column sortable when onSortChange is provided', () => {
      render(<RuleTable rules={mockRules} onSortChange={mock(() => {})} />)

      const nameHeader = screen.getByText('Name')
      expect(nameHeader.closest('button')).toBeInTheDocument()
    })

    it('calls onSortChange with asc on first click', () => {
      const onSortChange = mock(() => {})
      render(<RuleTable rules={mockRules} onSortChange={onSortChange} />)

      fireEvent.click(screen.getByText('Name').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'name', direction: 'asc' })
    })

    it('calls onSortChange with desc when sorted asc and clicked', () => {
      const onSortChange = mock(() => {})
      render(
        <RuleTable
          rules={mockRules}
          onSortChange={onSortChange}
          sort={{ columnId: 'name', direction: 'asc' }}
        />
      )

      fireEvent.click(screen.getByText('Name').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith({ columnId: 'name', direction: 'desc' })
    })

    it('calls onSortChange(null) on third click — clears sort', () => {
      const onSortChange = mock(() => {})
      render(
        <RuleTable
          rules={mockRules}
          onSortChange={onSortChange}
          sort={{ columnId: 'name', direction: 'desc' }}
        />
      )

      fireEvent.click(screen.getByText('Name').closest('button')!)
      expect(onSortChange).toHaveBeenCalledWith(null)
    })

    it('shows sort indicator on sorted column', () => {
      render(
        <RuleTable
          rules={mockRules}
          onSortChange={mock(() => {})}
          sort={{ columnId: 'name', direction: 'asc' }}
        />
      )

      const nameTh = screen.getByRole('columnheader', { name: /name/i })
      expect(nameTh).toHaveAttribute('aria-sort', 'ascending')
    })
  })

  describe('Row Click', () => {
    it('should call onRowClick when row is clicked', () => {
      const onRowClick = mock(() => {})
      render(<RuleTable rules={mockRules} onRowClick={onRowClick} />)

      const rows = screen.getAllByRole('row')
      fireEvent.click(rows[1])

      expect(onRowClick).toHaveBeenCalledWith(mockRules[0])
    })

    it('should make rows clickable when onRowClick provided', () => {
      render(<RuleTable rules={mockRules} onRowClick={() => {}} />)

      const rows = screen.getAllByRole('row')
      expect(rows[1]).toHaveClass('rule-table__row--clickable')
    })
  })

  describe('Selection', () => {
    it('should show checkboxes when selectable', () => {
      render(<RuleTable rules={mockRules} selectable />)

      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes.length).toBe(4)
    })

    it('should call onSelectionChange when checkbox clicked', () => {
      const onSelectionChange = mock(() => {})
      render(
        <RuleTable
          rules={mockRules}
          selectable
          onSelectionChange={onSelectionChange}
        />
      )

      const checkboxes = screen.getAllByRole('checkbox')
      fireEvent.click(checkboxes[1])

      expect(onSelectionChange).toHaveBeenCalledWith(['rule-1'])
    })

    it('should select all when header checkbox clicked', () => {
      const onSelectionChange = mock(() => {})
      render(
        <RuleTable
          rules={mockRules}
          selectable
          onSelectionChange={onSelectionChange}
        />
      )

      const checkboxes = screen.getAllByRole('checkbox')
      fireEvent.click(checkboxes[0])

      expect(onSelectionChange).toHaveBeenCalledWith(['rule-1', 'rule-2', 'rule-3'])
    })

    it('row checkboxes do not embed rule name in aria-label (attribute-path PII leak)', () => {
      // R4-2: session replay records attributes verbatim; data-ph-mask cannot cover them.
      // The per-row checkbox aria-label must NOT contain the rule name.
      render(<RuleTable rules={mockRules} selectable />)
      const checkboxes = screen.getAllByRole('checkbox')
      // Skip index 0 (Select all) — verify row checkboxes (1..n)
      for (let i = 1; i < checkboxes.length; i++) {
        const label = checkboxes[i].getAttribute('aria-label') ?? ''
        expect(label).not.toContain('Daily Budget Check')
        expect(label).not.toContain('Monthly Savings')
        expect(label).not.toContain('Event Transfer')
      }
    })

    it('should show selected rows', () => {
      render(
        <RuleTable
          rules={mockRules}
          selectable
          selectedIds={['rule-1', 'rule-3']}
        />
      )

      const checkboxes = screen.getAllByRole('checkbox')
      expect(checkboxes[1]).toBeChecked()
      expect(checkboxes[2]).not.toBeChecked()
      expect(checkboxes[3]).toBeChecked()
    })
  })

  describe('Accessibility', () => {
    it('should have accessible table structure', () => {
      render(<RuleTable rules={mockRules} />)

      expect(screen.getByRole('table')).toBeInTheDocument()
      const rowgroups = screen.getAllByRole('rowgroup')
      expect(rowgroups.length).toBe(2)
      expect(screen.getAllByRole('row').length).toBe(4)
    })

    it('should have accessible column headers', () => {
      render(<RuleTable rules={mockRules} />)

      const headers = screen.getAllByRole('columnheader')
      expect(headers.length).toBeGreaterThan(0)
    })

    it('should have accessible toggle switches', () => {
      render(<RuleTable rules={mockRules} onToggle={() => {}} />)

      const toggles = screen.getAllByRole('switch')
      toggles.forEach((toggle, i) => {
        expect(toggle).toHaveAttribute(
          'aria-checked',
          mockRules[i].enabled ? 'true' : 'false'
        )
      })
    })
  })

  describe('Details column — condition + action summary (R2-C)', () => {
    const ruleWithConditions: AutomationRule = {
      ...(mockRules[0] as AutomationRule),
      conditions: [{ field: 'budget.percent_used', operator: 'gte', value: 80 }],
    }

    const transferRuleWithAmount: AutomationRule = {
      ...(mockRules[2] as AutomationRule),
      rule_type: 'transfer_suggestion',
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      actions: { amount: 200 } as any,
      conditions: [],
    }

    it('renders condition summary in details column', () => {
      render(<RuleTable rules={[ruleWithConditions]} />)
      const bodyText = document.body.textContent ?? ''
      // condition: "When Budget % Used ≥ 80"
      expect(/Budget % Used/i.test(bodyText)).toBe(true)
    })

    it('renders action summary for transfer_suggestion rule', () => {
      render(<RuleTable rules={[transferRuleWithAmount]} />)
      const bodyText = document.body.textContent ?? ''
      expect(bodyText).toContain('Suggest transfer $200')
    })

    it('renders "Details" column header', () => {
      render(<RuleTable rules={mockRules} />)
      expect(screen.getByText('Details')).toBeInTheDocument()
    })

    it('applies data-ph-mask to details column cells (PII — financial amounts in action summary)', () => {
      const { container } = render(<RuleTable rules={[transferRuleWithAmount]} />)
      expect(container.querySelectorAll('[data-ph-mask]').length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('Read-only guarding on row actions (R2-C)', () => {
    it('wraps row toggle/edit/delete/backtest in ReadOnlyGuard (pointer-events-none in read-only mode)', () => {
      const { container } = render(
        <ReadOnlyProvider isReadOnly={true}>
          <RuleTable
            rules={mockRules}
            onEdit={mock(() => {})}
            onDelete={mock(() => {})}
            onToggle={mock(() => {})}
            onTest={mock(() => {})}
          />
        </ReadOnlyProvider>
      )
      expect(container.querySelector('.pointer-events-none')).toBeTruthy()
    })

    it('does NOT disable onRowClick navigation in read-only mode', () => {
      const onRowClick = mock(() => {})
      render(
        <ReadOnlyProvider isReadOnly={true}>
          <RuleTable rules={mockRules} onRowClick={onRowClick} />
        </ReadOnlyProvider>
      )
      const rows = screen.getAllByRole('row')
      fireEvent.click(rows[1])
      expect(onRowClick).toHaveBeenCalledWith(mockRules[0])
    })
  })

  describe('Action Button Tooltips', () => {
    it('should have tooltip on toggle button', () => {
      render(<RuleTable rules={mockRules} onToggle={mock(() => {})} />)

      const toggles = screen.getAllByRole('switch')
      expect(toggles[0]).toHaveAttribute('title', 'Disable rule')
      expect(toggles[1]).toHaveAttribute('title', 'Enable rule')
    })

    it('should have tooltip on backtest button', () => {
      render(<RuleTable rules={mockRules} onTest={mock(() => {})} />)

      const backtestButtons = screen.getAllByRole('button', { name: /backtest rule/i })
      backtestButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Backtest - See what this rule would have done with your recent data')
      })
    })

    it('should have tooltip on edit button', () => {
      render(<RuleTable rules={mockRules} onEdit={mock(() => {})} />)

      const editButtons = screen.getAllByRole('button', { name: /edit rule/i })
      editButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Edit rule')
      })
    })

    it('should have tooltip on delete button', () => {
      render(<RuleTable rules={mockRules} onDelete={mock(() => {})} />)

      const deleteButtons = screen.getAllByRole('button', { name: /delete rule/i })
      deleteButtons.forEach((btn) => {
        expect(btn).toHaveAttribute('title', 'Delete rule')
      })
    })
  })
})
