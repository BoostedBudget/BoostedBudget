/**
 * PortfolioTimeRange — time-range preset selector for the portfolio chart.
 * Issue #1296: Renders 1W/1M/3M/YTD/1Y/All as a single-select ToggleGroup.
 */

import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { PORTFOLIO_PRESETS } from '../hooks/usePortfolioTimeRange'
import type { PortfolioPreset } from '../hooks/usePortfolioTimeRange'

interface PortfolioTimeRangeProps {
  activePreset: PortfolioPreset
  onPresetChange: (preset: PortfolioPreset) => void
}

export function PortfolioTimeRange({ activePreset, onPresetChange }: PortfolioTimeRangeProps) {
  const handleValueChange = (value: string) => {
    if (!value) return // ToggleGroup single-select fires empty on deselect — ignore
    onPresetChange(value as PortfolioPreset)
  }

  return (
    <ToggleGroup
      type="single"
      value={activePreset}
      onValueChange={handleValueChange}
      aria-label="Portfolio time range"
      size="sm"
    >
      {PORTFOLIO_PRESETS.map((preset) => (
        <ToggleGroupItem key={preset} value={preset} aria-label={`${preset} time range`}>
          {preset}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
