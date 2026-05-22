import type { EChartsOption } from 'echarts'
import { formatCurrency, formatCurrencyCompact } from '../formatters'
import { getBaseOption } from './base'

export function getCurrencyOption(isDark: boolean, compact = false): EChartsOption {
  const base = getBaseOption(isDark);
  const formatter = (v: unknown) => {
    if (typeof v !== 'number') return String(v);
    return compact ? formatCurrencyCompact(v) : formatCurrency(v);
  };

  return {
    ...base,
    yAxis: {
      ...(base.yAxis as object),
      axisLabel: {
        ...((base.yAxis as Record<string, unknown>).axisLabel as object),
        formatter,
      },
    },
  };
}
