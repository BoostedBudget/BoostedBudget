/**
 * Cache-level regression: deleting the LAST goal must not leave the Deleted Goals panel hidden.
 * #1569, Codex finding 4.
 *
 * THE BUG THIS PINS. `deleteGoal()` refetched only the live-goals query. The deleted-goals query
 * (`/api/v1/savings-goals/deleted`) kept whatever was in the cache, and main.tsx sets a GLOBAL
 * five-minute `staleTime`. So when the user deleted their last goal, SavingsPage switched to the
 * empty branch, mounted <DeletedGoalsPanel> for the first time — and React Query served the
 * cached, empty deleted-goals result rather than fetching. The panel self-hides on an empty
 * listing, so the user saw "No savings goals yet", the purge affordance was unreachable, and the
 * confirm dialog's promise that their contributions were kept had nothing to back it up. That is
 * the exact Critical a whole-branch review already raised; the structural fix landed and the
 * cache path was missed.
 *
 * ⚠ WHY THIS TEST DOES NOT MOCK THE HOOK. A test that stubs useGetDeletedSavingsGoals cannot see
 * this defect at all: the defect IS the cache, and a stubbed hook has none. So this file drives
 * the REAL generated hooks against a REAL QueryClient carrying main.tsx's real staleTime, with a
 * PRIMED, already-empty deleted-goals entry, and substitutes only the axios adapter — the network
 * itself. If it is ever "simplified" into a hook mock it stops testing anything.
 *
 * ⚠ WHY THIS FILE RUNS PROCESS-ISOLATED (test-conflict-files.sh Group F, no savings-goals-api
 * preload). bun's mock.module() is process-global, and src/test/mocks/savings-goals-api.ts —
 * imported by the sibling savings tests — replaces exactly the hooks this file needs to be real.
 * In the shared process it would silently test the mocks. See
 * feedback_bun_test_mock_module_global.md.
 */
import { describe, it, expect, beforeEach, afterEach } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { AxiosAdapter, AxiosRequestConfig, AxiosResponse } from 'axios'
import { AXIOS_INSTANCE } from '@boostedbudget/api-client/client'
import { getGetDeletedSavingsGoalsQueryKey } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import { useSavingsGoals } from './useSavingsGoals'
import { DeletedGoalsPanel } from '../components/DeletedGoalsPanel'

const liveGoal = {
  id: 'goal-1',
  user_id: 'u1',
  name: 'Japan Trip',
  target_amount: 4000,
  current_amount: 1250,
  currency_code: 'USD',
  account_id: 'acc-1',
  is_completed: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-06-01T00:00:00Z',
  progress_basis: 'balance',
}

const deletedGoal = { ...liveGoal, deleted_at: '2025-06-02T00:00:00Z' }

// Server state the fake network answers from. `deleted` starts EMPTY and is filled by the DELETE,
// so a client that never re-asks after deleting cannot possibly see the goal — which is the whole
// point of the assertion below.
let live: unknown[] = []
let deleted: unknown[] = []
let deletedFetchCount = 0

const originalAdapter = AXIOS_INSTANCE.defaults.adapter

function respond(config: AxiosRequestConfig, data: unknown): AxiosResponse {
  return {
    data,
    status: 200,
    statusText: 'OK',
    headers: {},
    config: config as AxiosResponse['config'],
  }
}

const fakeNetwork: AxiosAdapter = async (config) => {
  const url = config.url ?? ''
  const method = (config.method ?? 'get').toLowerCase()
  if (method === 'delete' && /\/savings-goals\/[^/]+$/.test(url)) {
    live = []
    deleted = [deletedGoal]
    return respond(config, {})
  }
  if (url.endsWith('/savings-goals/deleted')) {
    deletedFetchCount += 1
    return respond(config, deleted)
  }
  if (url.endsWith('/savings-goals')) {
    return respond(config, live)
  }
  // Contribution ledgers etc. — the panel only asks for these once expanded, which it is not here.
  return respond(config, [])
}

/**
 * Mirrors SavingsPage's structure exactly where it matters: the panel is rendered ONLY in the
 * zero-goals branch, so deleting the last goal MOUNTS it fresh against whatever the cache holds.
 */
function Harness() {
  const { goals, loading, deleteGoal } = useSavingsGoals()
  if (loading) return <div>loading</div>
  if (goals.length === 0) return <DeletedGoalsPanel />
  return (
    <button type="button" onClick={() => void deleteGoal(goals[0].id)}>
      Delete last goal
    </button>
  )
}

function renderWithPrimedCache() {
  const queryClient = new QueryClient({
    defaultOptions: {
      // main.tsx's real defaults. The five-minute staleTime is the mechanism: without it a
      // freshly-primed entry would be refetched on mount anyway and the defect would be invisible.
      queries: { staleTime: 5 * 60 * 1000, retry: false, refetchOnWindowFocus: false },
    },
  })
  // The user opened the savings page before deleting anything, so the deleted-goals listing was
  // already fetched and was already empty. Prime exactly that.
  queryClient.setQueryData(getGetDeletedSavingsGoalsQueryKey(), [])
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
  return render(<Harness />, { wrapper: Wrapper })
}

describe('deleting the last savings goal (cache level)', () => {
  beforeEach(() => {
    live = [liveGoal]
    deleted = []
    deletedFetchCount = 0
    AXIOS_INSTANCE.defaults.adapter = fakeNetwork
  })

  afterEach(() => {
    AXIOS_INSTANCE.defaults.adapter = originalAdapter
  })

  it('shows the Deleted goals panel even though the deleted-goals query was cached empty', async () => {
    renderWithPrimedCache()

    const deleteButton = await screen.findByRole('button', { name: 'Delete last goal' })
    // The primed entry really was served from cache — nothing re-asked the server for it.
    expect(deletedFetchCount).toBe(0)

    fireEvent.click(deleteButton)

    await waitFor(() => {
      expect(screen.getByText('Deleted goals (1)')).toBeTruthy()
    })
    expect(deletedFetchCount).toBeGreaterThan(0)
  })
})
