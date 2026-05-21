import type { EChartsOption } from 'echarts'
import { formatCurrency, formatCurrencyCompact } from '../formatters'
import { getBaseOption } from './base'

export function getCurrencyOption(isDark: boolean, compact = false): EChartsOption {
  const base = getBaseOption(isDark)
  const formatter = compact
    ? (v: unknown) => (typeof v === 'number' ? formatCurrencyCompact(v) : String(v))
    : (v: unknown) => (typeof v === 'number' ? formatCurrency(v) : String(v))

  return {
    ...base,
    yAxis: {
      ...(base.yAxis as object),
      axisLabel: {
        ...((base.yAxis as Record<string, unknown>).axisLabel as object),
        formatter,
      },
    },
  }
}
