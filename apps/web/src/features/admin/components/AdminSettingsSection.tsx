import { Shield } from 'lucide-react'
import type { AdminSettingsSectionProps } from '../types'
import './AdminSettingsSection.css'

export function AdminSettingsSection({ adminModeEnabled, onToggle }: AdminSettingsSectionProps) {
  return (
    <section className="admin-settings-section">
      <h3 className="admin-settings-section__title">
        <Shield className="admin-settings-section__icon" size={20} />
        Admin Settings
      </h3>
      <p className="admin-settings-section__description">
        You have administrator privileges. Enable admin mode to access the admin
        dashboard and management tools.
      </p>
      <div className="admin-settings-section__option">
        <label className="admin-settings-section__label">
          <input
            type="checkbox"
            checked={adminModeEnabled}
            onChange={onToggle}
            className="admin-settings-section__checkbox"
            aria-label="Enable Admin Mode"
          />
          <span className="admin-settings-section__label-text">
            Enable Admin Mode
          </span>
        </label>
        <p className="admin-settings-section__help">
          When enabled, admin navigation will appear in the header.
        </p>
      </div>
    </section>
  )
}
