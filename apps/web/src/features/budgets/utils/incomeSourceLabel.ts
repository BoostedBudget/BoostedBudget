import type { IncomeSource } from '../types'
import { formatPFCPrimary } from '@/shared/categories/formatPfc'

export function getPFCIncomeSourceLabel(source: IncomeSource): string {
  return source.value_display ?? formatPFCPrimary(source.value)
}
