import type { TransactionFilters, CategorySource } from '../../types'

export type ReviewPreset = 'unreviewed' | 'reviewed' | 'auto-categorized' | 'manually-reviewed'

export function presetToFilters(preset: ReviewPreset): Pick<TransactionFilters, 'reviewed' | 'category_sources'> {
  switch (preset) {
    case 'unreviewed':
      return { reviewed: false, category_sources: undefined }
    case 'reviewed':
      return { reviewed: true, category_sources: undefined }
    case 'auto-categorized':
      return { reviewed: false, category_sources: ['plaid', 'rule'] as CategorySource[] }
    case 'manually-reviewed':
      return { reviewed: true, category_sources: ['manual'] as CategorySource[] }
  }
}
