import { Shield } from 'lucide-react'
import type { AdminSettingsSectionProps } from '../types'

export function AdminSettingsSection({ adminModeEnabled, onToggle }: AdminSettingsSectionProps) {
  return (
    <section className="bg-card border border-border border-l-4 border-l-amber-500 rounded-lg p-6 mt-6">
      <h3 className="flex items-center gap-2 text-xl mt-0 mb-2 text-foreground">
        <Shield className="text-amber-500" size={20} />
        Admin Settings
      </h3>
      <p className="text-muted-foreground mb-6 leading-relaxed">
        You have administrator privileges. Enable admin mode to access the admin
        dashboard and management tools.
      </p>
      <div>
        <label className="flex items-center cursor-pointer font-medium text-foreground">
          <input
            type="checkbox"
            checked={adminModeEnabled}
            onChange={onToggle}
            className="w-5 h-5 mr-3 cursor-pointer accent-amber-500"
            aria-label="Enable Admin Mode"
          />
          <span className="text-base font-semibold">
            Enable Admin Mode
          </span>
        </label>
        <p className="mt-2 ml-8 text-muted-foreground text-sm leading-relaxed">
          When enabled, admin navigation will appear in the header.
        </p>
      </div>
    </section>
  )
}
