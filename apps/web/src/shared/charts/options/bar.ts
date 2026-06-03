import type { EChartsOption } from 'echarts'
import { getCurrencyOption } from './currency'

export function getBarOption(isDark: boolean): EChartsOption {
  return {
    ...getCurrencyOption(isDark),
    tooltip: { trigger: 'axis' },
  };
}
