import { Settings2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { NetWorthSection } from '@/features/net-worth/components/NetWorthSection'
import { useTrackEvent } from '@/shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '@/shared/analytics/events'
import { AllocationDonutChart } from './AllocationDonutChart'
import type { AllocationConfig } from '../hooks/usePortfolioAllocationConfig'

interface AllocationCardProps {
  config: AllocationConfig
  onConfigChange: (next: AllocationConfig) => void
}

interface SettingsPopoverProps {
  config: AllocationConfig
  onConfigChange: (next: AllocationConfig) => void
}

function SettingsPopover({ config, onConfigChange }: SettingsPopoverProps) {
  const trackEvent = useTrackEvent()

  function handleChange(next: AllocationConfig) {
    onConfigChange(next)
    trackEvent(ANALYTICS_EVENTS.PORTFOLIO_ALLOCATION_CONFIG_CHANGED, {
      group_by: next.group_by,
      basis: next.basis,
      include_count: next.include.length,
    })
  }

  function handleIncludeToggle(value: string) {
    const next = config.include.includes(value)
      ? config.include.filter((v) => v !== value)
      : [...config.include, value]
    handleChange({ ...config, include: next })
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          aria-label="Allocation settings"
          className="h-8 w-8 p-0"
        >
          <Settings2 className="size-4" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-64 p-4 space-y-4">
        <fieldset>
          <legend className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
            Include
          </legend>
          <div className="space-y-1.5">
            {(['cash', 'liabilities'] as const).map((value) => (
              <label key={value} className="flex items-center gap-2 text-sm cursor-pointer">
                <input
                  type="checkbox"
                  name={`include-${value}`}
                  checked={config.include.includes(value)}
                  onChange={() => handleIncludeToggle(value)}
                  aria-label={value.charAt(0).toUpperCase() + value.slice(1)}
                />
                {value.charAt(0).toUpperCase() + value.slice(1)}
              </label>
            ))}
          </div>
        </fieldset>

        <fieldset>
          <legend className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
            Group by
          </legend>
          <div className="space-y-1.5">
            {([
              { value: 'security_type', label: 'Security Type' },
              { value: 'asset_class', label: 'Asset Class' },
              { value: 'account', label: 'Account' },
            ] as const).map(({ value, label }) => (
              <label key={value} className="flex items-center gap-2 text-sm cursor-pointer">
                <input
                  type="radio"
                  name="group-by"
                  value={value}
                  checked={config.group_by === value}
                  onChange={() => handleChange({ ...config, group_by: value })}
                  aria-label={label}
                />
                {label}
              </label>
            ))}
          </div>
        </fieldset>

        <fieldset>
          <legend className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mb-2">
            Basis
          </legend>
          <div className="space-y-1.5">
            {([
              { value: 'assets', label: 'Assets only' },
              { value: 'net', label: 'Net of liabilities' },
            ] as const).map(({ value, label }) => (
              <label key={value} className="flex items-center gap-2 text-sm cursor-pointer">
                <input
                  type="radio"
                  name="basis"
                  value={value}
                  checked={config.basis === value}
                  onChange={() => handleChange({ ...config, basis: value })}
                  aria-label={label}
                />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
      </PopoverContent>
    </Popover>
  )
}

export function AllocationCard({ config, onConfigChange }: AllocationCardProps) {
  return (
    <NetWorthSection
      title="Allocation"
      action={<SettingsPopover config={config} onConfigChange={onConfigChange} />}
    >
      <AllocationDonutChart config={config} />
    </NetWorthSection>
  )
}
