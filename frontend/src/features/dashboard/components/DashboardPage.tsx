import { useState } from 'react'
import { format, subDays } from 'date-fns'
import { useDashboardData } from '../hooks/useDashboardData'
import { BalanceOverview } from './BalanceOverview'
import { BalanceHistoryChart } from './BalanceHistoryChart'
import { AccountTypeFilter } from './AccountTypeFilter'
import { TimeframeSelector } from './TimeframeSelector'
import { AccountBalanceCards } from './AccountBalanceCards'
import type { DashboardFilters } from '../types'

/**
 * DashboardPage - Main dashboard view for authenticated users
 *
 * Features:
 * - Balance overview with period comparison
 * - Interactive balance history chart
 * - Account type filtering
 * - Timeframe selection
 * - Individual account balance cards
 * - Loading and error states
 * - Responsive grid layout
 *
 * This is the homepage for authenticated users.
 *
 * Issue #353: Balance Tracking Dashboard
 */
export function DashboardPage() {
  // Default to last 30 days
  const today = new Date()
  const thirtyDaysAgo = subDays(today, 30)

  const [filters, setFilters] = useState<DashboardFilters>({
    startDate: format(thirtyDaysAgo, 'yyyy-MM-dd'),
    endDate: format(today, 'yyyy-MM-dd'),
    accountTypes: [],
  })

  const [showTypeBreakdown, setShowTypeBreakdown] = useState(false)

  const { summary, history, accountTypes, loading, error, refetch } =
    useDashboardData(filters)

  const handleAccountTypeChange = (types: string[]) => {
    setFilters({ ...filters, accountTypes: types })
  }

  const handleTimeframeChange = (startDate: string, endDate: string) => {
    setFilters({ ...filters, startDate, endDate })
  }

  // Loading state
  if (loading) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="text-center">
          <div className="inline-block animate-spin rounded-full h-12 w-12 border-b-2 border-blue-600"></div>
          <p className="mt-4 text-gray-600">Loading dashboard...</p>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="bg-white rounded-lg shadow-lg p-8 max-w-md text-center">
          <div className="text-red-600 text-5xl mb-4">⚠️</div>
          <h2 className="text-xl font-semibold text-gray-900 mb-2">
            Failed to Load Dashboard
          </h2>
          <p className="text-gray-600 mb-6">{error.message}</p>
          <button
            onClick={refetch}
            className="px-6 py-3 bg-blue-600 text-white font-medium rounded-lg hover:bg-blue-700 transition-colors"
          >
            Try Again
          </button>
        </div>
      </div>
    )
  }

  // Empty state
  if (!summary || !history || !accountTypes) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="bg-white rounded-lg shadow-lg p-8 max-w-md text-center">
          <div className="text-gray-400 text-5xl mb-4">📊</div>
          <h2 className="text-xl font-semibold text-gray-900 mb-2">
            No Data Available
          </h2>
          <p className="text-gray-600">
            Connect your accounts to start tracking your balance history.
          </p>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50 py-8">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-3xl font-bold text-gray-900">Dashboard</h1>
          <p className="mt-2 text-gray-600">
            Track your balance across all accounts
          </p>
        </div>

        {/* Main Grid Layout */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          {/* Left Column - Overview and Chart */}
          <div className="lg:col-span-2 space-y-6">
            {/* Balance Overview */}
            <BalanceOverview summary={summary} />

            {/* Balance History Chart */}
            <div className="relative">
              <BalanceHistoryChart
                history={history}
                showTypeBreakdown={showTypeBreakdown}
              />

              {/* Chart Controls */}
              <div className="absolute top-6 right-6">
                <button
                  onClick={() => setShowTypeBreakdown(!showTypeBreakdown)}
                  className="px-4 py-2 bg-white border border-gray-300 rounded-lg text-sm font-medium text-gray-700 hover:bg-gray-50 transition-colors shadow-sm"
                >
                  {showTypeBreakdown ? 'Total Only' : 'Show Breakdown'}
                </button>
              </div>
            </div>

            {/* Account Balance Cards */}
            <div>
              <h2 className="text-xl font-semibold text-gray-900 mb-4">
                Accounts
              </h2>
              <AccountBalanceCards summary={summary} />
            </div>
          </div>

          {/* Right Column - Filters */}
          <div className="space-y-6">
            {/* Timeframe Selector */}
            <TimeframeSelector onRangeChange={handleTimeframeChange} />

            {/* Account Type Filter */}
            <AccountTypeFilter
              accountTypes={accountTypes}
              selectedTypes={filters.accountTypes || []}
              onChange={handleAccountTypeChange}
            />
          </div>
        </div>
      </div>
    </div>
  )
}
