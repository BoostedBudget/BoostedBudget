/**
 * RuleExecutionHistory Component Tests
 * Issue #39: Rule Builder UI
 *
 * Tests for the rule execution history display component.
 * TDD RED: Writing tests before implementation.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RuleExecutionHistory } from './RuleExecutionHistory'
import type { RuleExecution } from '@/api/generated/models'

// Mock the API hook
vi.mock('@/api/generated/endpoints/automation-rules/automation-rules', () => ({
  useGetRuleExecutions: vi.fn(),
}))

import { useGetRuleExecutions } from '@/api/generated/endpoints/automation-rules/automation-rules'

const mockExecutions: RuleExecution[] = [
  {
    id: 'exec-1',
    rule_id: 'rule-1',
    status: 'completed',
    started_at: '2024-01-15T10:00:00Z',
    completed_at: '2024-01-15T10:00:05Z',
    execution_duration_ms: 5000,
    retry_count: 0,
    max_retries: 3,
    trigger_context: { type: 'schedule' },
  },
  {
    id: 'exec-2',
    rule_id: 'rule-1',
    status: 'failed',
    started_at: '2024-01-14T10:00:00Z',
    completed_at: '2024-01-14T10:00:02Z',
    execution_duration_ms: 2000,
    error_message: 'Connection timeout',
    error_code: 'TIMEOUT',
    retry_count: 3,
    max_retries: 3,
    trigger_context: { type: 'manual' },
  },
  {
    id: 'exec-3',
    rule_id: 'rule-1',
    status: 'running',
    started_at: '2024-01-16T10:00:00Z',
    retry_count: 0,
    max_retries: 3,
    trigger_context: { type: 'event' },
  },
]

function createTestWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })
  return function TestWrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    )
  }
}

describe('RuleExecutionHistory', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Loading State', () => {
    it('should show loading state when fetching', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: undefined,
        isLoading: true,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText(/loading/i)).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show empty message when no executions', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: [] },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText(/no executions/i)).toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should show error message on fetch failure', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Failed to load'),
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText(/error/i)).toBeInTheDocument()
    })

    it('should have retry button on error', () => {
      const refetch = vi.fn()
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: undefined,
        isLoading: false,
        error: new Error('Failed to load'),
        refetch,
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      const retryButton = screen.getByRole('button', { name: /retry/i })
      fireEvent.click(retryButton)

      expect(refetch).toHaveBeenCalled()
    })
  })

  describe('Execution List', () => {
    beforeEach(() => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: mockExecutions },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)
    })

    it('should render execution table', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByRole('table')).toBeInTheDocument()
    })

    it('should display execution status', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText('completed')).toBeInTheDocument()
      expect(screen.getByText('failed')).toBeInTheDocument()
      expect(screen.getByText('running')).toBeInTheDocument()
    })

    it('should display execution time', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      // Look for formatted dates (partial match for year)
      expect(screen.getByText(/Jan 15, 2024/)).toBeInTheDocument()
    })

    it('should display execution duration', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      // 5000ms = 5s, 2000ms = 2s
      expect(screen.getByText(/5\.0s/)).toBeInTheDocument()
      expect(screen.getByText(/2\.0s/)).toBeInTheDocument()
    })

    it('should display trigger type', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText(/schedule/i)).toBeInTheDocument()
      expect(screen.getByText(/manual/i)).toBeInTheDocument()
      expect(screen.getByText(/event/i)).toBeInTheDocument()
    })

    it('should display error message for failed executions', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByText(/connection timeout/i)).toBeInTheDocument()
    })
  })

  describe('Status Badges', () => {
    beforeEach(() => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: mockExecutions },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)
    })

    it('should apply success styling to completed status', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      const completedBadge = screen.getByText('completed')
      expect(completedBadge.className).toContain('completed')
    })

    it('should apply error styling to failed status', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      const failedBadge = screen.getByText('failed')
      expect(failedBadge.className).toContain('failed')
    })

    it('should apply running styling to running status', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      const runningBadge = screen.getByText('running')
      expect(runningBadge.className).toContain('running')
    })
  })

  describe('Pagination', () => {
    it('should show pagination controls when limit is provided', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: mockExecutions, total: 25 },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(<RuleExecutionHistory ruleId="rule-1" limit={10} />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByRole('button', { name: /next/i })).toBeInTheDocument()
    })

    it('should call onPageChange when pagination button clicked', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: mockExecutions, total: 25 },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      const onPageChange = vi.fn()
      render(
        <RuleExecutionHistory
          ruleId="rule-1"
          limit={10}
          onPageChange={onPageChange}
        />,
        { wrapper: createTestWrapper() }
      )

      const nextButton = screen.getByRole('button', { name: /next/i })
      fireEvent.click(nextButton)

      expect(onPageChange).toHaveBeenCalledWith(1)
    })
  })

  describe('Accessibility', () => {
    beforeEach(() => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: mockExecutions },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)
    })

    it('should have accessible table structure', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(screen.getByRole('table')).toBeInTheDocument()
      expect(screen.getAllByRole('columnheader').length).toBeGreaterThan(0)
      expect(screen.getAllByRole('row').length).toBeGreaterThan(1)
    })

    it('should have heading', () => {
      render(<RuleExecutionHistory ruleId="rule-1" />, {
        wrapper: createTestWrapper(),
      })

      expect(
        screen.getByRole('heading', { name: /execution history/i })
      ).toBeInTheDocument()
    })
  })

  describe('Props', () => {
    it('should pass limit and offset to API hook', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: [] },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      render(
        <RuleExecutionHistory ruleId="rule-1" limit={20} offset={40} />,
        { wrapper: createTestWrapper() }
      )

      expect(useGetRuleExecutions).toHaveBeenCalledWith(
        'rule-1',
        expect.objectContaining({ limit: 20, offset: 40 }),
        undefined
      )
    })

    it('should apply custom className', () => {
      vi.mocked(useGetRuleExecutions).mockReturnValue({
        data: { executions: [] },
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      } as unknown as ReturnType<typeof useGetRuleExecutions>)

      const { container } = render(
        <RuleExecutionHistory ruleId="rule-1" className="custom-class" />,
        { wrapper: createTestWrapper() }
      )

      expect(container.firstChild).toHaveClass('custom-class')
    })
  })
})
