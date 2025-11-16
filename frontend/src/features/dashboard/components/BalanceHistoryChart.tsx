import { format, parseISO } from 'date-fns'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import type { BalanceHistory } from '../types'

interface BalanceHistoryChartProps {
  history: BalanceHistory
  showTypeBreakdown?: boolean
}

// Type for custom Recharts tooltip component
interface CustomTooltipProps {
  active?: boolean
  payload?: Array<{
    value: number | string
    name: string
    color?: string
    dataKey?: string
    payload?: Record<string, unknown>
  }>
  label?: string | number
}

// Currency formatter
const formatCurrency = (value: number) => {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(value)
}

// Custom tooltip component (outside of render to avoid recreation)
function CustomTooltip({ active, payload }: CustomTooltipProps) {
  if (active && payload && payload.length > 0) {
    const data = payload[0].payload as { fullDate: string }

    return (
      <div className="bg-white border border-gray-200 rounded-lg shadow-lg p-3">
        <p className="text-sm font-semibold text-gray-900 mb-2">
          {format(parseISO(data.fullDate), 'MMMM d, yyyy')}
        </p>
        {payload.map((entry, index: number) => (
          <div key={index} className="flex items-center justify-between space-x-4">
            <span
              className="text-sm"
              style={{ color: entry.color }}
            >
              {entry.name}:
            </span>
            <span className="text-sm font-semibold">
              {formatCurrency(entry.value as number)}
            </span>
          </div>
        ))}
      </div>
    )
  }

  return null
}

/**
 * BalanceHistoryChart component displays balance trends over time
 *
 * Features:
 * - Interactive line chart with Recharts
 * - Responsive design
 * - Optional breakdown by account type
 * - Formatted currency values
 * - Date-based x-axis
 * - Hover tooltips with detailed information
 *
 * Issue #353: Balance Tracking Dashboard
 */
export function BalanceHistoryChart({
  history,
  showTypeBreakdown = false,
}: BalanceHistoryChartProps) {
  // Transform data for Recharts
  const chartData = history.snapshots.map((snapshot) => {
    const formattedDate = format(parseISO(snapshot.date), 'MMM d')

    return {
      date: formattedDate,
      fullDate: snapshot.date,
      balance: snapshot.balance,
      ...(showTypeBreakdown && snapshot.by_type
        ? snapshot.by_type
        : {}),
    }
  })

  // Account type colors
  const typeColors: Record<string, string> = {
    balance: '#3b82f6', // blue-500
    depository: '#10b981', // green-500
    credit: '#ef4444', // red-500
    investment: '#8b5cf6', // purple-500
    loan: '#f59e0b', // amber-500
  }

  return (
    <div className="bg-white rounded-lg shadow p-6">
      <h2 className="text-lg font-semibold text-gray-900 mb-4">
        Balance History
      </h2>

      <ResponsiveContainer width="100%" height={400}>
        <LineChart
          data={chartData}
          margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
          <XAxis
            dataKey="date"
            tick={{ fontSize: 12 }}
            stroke="#6b7280"
          />
          <YAxis
            tickFormatter={formatCurrency}
            tick={{ fontSize: 12 }}
            stroke="#6b7280"
          />
          <Tooltip content={<CustomTooltip />} />
          <Legend
            wrapperStyle={{ fontSize: '14px' }}
            iconType="line"
          />

          {!showTypeBreakdown && (
            <Line
              type="monotone"
              dataKey="balance"
              stroke={typeColors.balance}
              strokeWidth={2}
              dot={{ r: 3 }}
              activeDot={{ r: 5 }}
              name="Total Balance"
            />
          )}

          {showTypeBreakdown &&
            history.snapshots.some((s) => s.by_type) &&
            Object.keys(history.snapshots[0].by_type || {}).map(
              (typeKey) => (
                <Line
                  key={typeKey}
                  type="monotone"
                  dataKey={typeKey}
                  stroke={typeColors[typeKey] || '#6b7280'}
                  strokeWidth={2}
                  dot={{ r: 3 }}
                  activeDot={{ r: 5 }}
                  name={
                    typeKey.charAt(0).toUpperCase() + typeKey.slice(1)
                  }
                />
              )
            )}
        </LineChart>
      </ResponsiveContainer>

      {history.snapshots.length === 0 && (
        <div className="flex items-center justify-center h-64">
          <p className="text-gray-500">No balance history available</p>
        </div>
      )}
    </div>
  )
}
