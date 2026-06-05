/**
 * ReviewFilterPresets — segmented control for common review inbox filters.
 *
 * Presets:
 *   unreviewed         → { reviewed: false }
 *   reviewed           → { reviewed: true }
 *   auto-categorized   → { reviewed: false, category_sources: ['plaid', 'rule'] }
 *   manually-reviewed  → { reviewed: true, category_sources: ['manual'] }
 */

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { presetToFilters } from './reviewFilterPresetsUtils'
import type { ReviewPreset } from './reviewFilterPresetsUtils'
import type { TransactionFilters } from '../../types'

export type { ReviewPreset }

const PRESET_LABELS: Record<ReviewPreset, string> = {
  'unreviewed': 'Unreviewed',
  'reviewed': 'Reviewed',
  'auto-categorized': 'Auto-categorized',
  'manually-reviewed': 'Manually reviewed',
}

interface ReviewFilterPresetsProps {
  activePreset: ReviewPreset
  onChange: (preset: ReviewPreset, filters: Pick<TransactionFilters, 'reviewed' | 'category_sources'>) => void
}

export function ReviewFilterPresets({ activePreset, onChange }: ReviewFilterPresetsProps) {
  const presets = Object.keys(PRESET_LABELS) as ReviewPreset[]

  return (
    <ToggleGroup
      type="single"
      value={activePreset}
      onValueChange={(value) => {
        if (value && presets.includes(value as ReviewPreset)) {
          const preset = value as ReviewPreset
          onChange(preset, presetToFilters(preset))
        }
      }}
      aria-label="Review filter presets"
    >
      {presets.map((preset) => (
        <ToggleGroupItem key={preset} value={preset} aria-label={PRESET_LABELS[preset]}>
          {PRESET_LABELS[preset]}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

