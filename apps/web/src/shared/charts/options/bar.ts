import type { EChartsOption } from 'echarts'
import { getCurrencyOption } from './currency'

export function getBarOption(isDark: boolean): EChartsOption {
  const base = getCurrencyOption(isDark)
  return {
    ...base,
    tooltip: { trigger: 'axis' },
  }
}
