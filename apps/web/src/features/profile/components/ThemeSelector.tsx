import { Sun, Moon, Monitor } from 'lucide-react'
import { useTheme } from '@/shared/theme'
import type { Theme } from '@/shared/theme'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'

interface ThemeOption {
  value: Theme
  label: string
  icon: React.ReactNode
}

const THEME_OPTIONS: ThemeOption[] = [
  { value: 'light', label: 'Light', icon: <Sun className="h-4 w-4" aria-hidden="true" /> },
  { value: 'dark', label: 'Dark', icon: <Moon className="h-4 w-4" aria-hidden="true" /> },
  { value: 'system', label: 'System', icon: <Monitor className="h-4 w-4" aria-hidden="true" /> },
]

export function ThemeSelector() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const trackEvent = useTrackEvent()

  function handleThemeChange(newTheme: Theme) {
    setTheme(newTheme)
    trackEvent(ANALYTICS_EVENTS.THEME_CHANGED, { theme: newTheme, resolved: resolvedTheme })
  }

  return (
    <div className="space-y-3">
      <div className="flex gap-2" role="group" aria-label="Theme selection">
        {THEME_OPTIONS.map(({ value, label, icon }) => {
          const isActive = theme === value
          return (
            <button
              key={value}
              type="button"
              role="button"
              data-active={isActive}
              aria-pressed={isActive}
              onClick={() => handleThemeChange(value)}
              className={`flex flex-1 items-center justify-center gap-2 rounded-lg border px-4 py-3 text-sm font-medium transition-colors ${
                isActive
                  ? 'border-primary bg-primary/5 text-foreground'
                  : 'border-border bg-card text-muted-foreground hover:bg-accent hover:text-foreground'
              }`}
            >
              {icon}
              {label}
            </button>
          )
        })}
      </div>

      {theme === 'system' && (
        <p className="text-sm text-muted-foreground">
          Currently using <span className="font-medium capitalize">{resolvedTheme}</span> mode
        </p>
      )}
    </div>
  )
}
