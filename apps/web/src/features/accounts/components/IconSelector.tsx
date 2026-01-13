/**
 * IconSelector Component
 *
 * A grid of predefined icons for account customization.
 * Uses lucide-react icons matching the backend ValidAccountIcons.
 *
 * Issue #307: Account Customization
 */

import { cn } from '@/lib/utils'
import { X } from 'lucide-react'
import { AccountIconDisplay } from './AccountIconDisplay'
import { VALID_ACCOUNT_ICONS, type AccountIcon } from '../types'

// User-friendly labels for icons
const ICON_LABELS: Record<AccountIcon, string> = {
  'wallet': 'Wallet',
  'piggy-bank': 'Savings',
  'credit-card': 'Card',
  'bank': 'Bank',
  'building': 'Building',
  'briefcase': 'Business',
  'dollar': 'Dollar',
  'euro': 'Euro',
  'pound': 'Pound',
  'yen': 'Yen',
  'chart': 'Chart',
  'trending-up': 'Growth',
  'trending-down': 'Decline',
  'lock': 'Locked',
  'unlock': 'Unlocked',
  'star': 'Star',
  'heart': 'Heart',
  'home': 'Home',
  'car': 'Car',
  'plane': 'Travel',
  'shopping-cart': 'Shopping',
  'gift': 'Gift',
  'coffee': 'Coffee',
  'utensils': 'Food',
  'graduation-cap': 'Education',
}

export interface IconSelectorProps {
  /** Currently selected icon */
  value?: AccountIcon | null
  /** Callback when icon is selected */
  onChange: (icon: AccountIcon | null) => void
  /** Additional class name for the container */
  className?: string
  /** Color to use for icons (hex format) */
  iconColor?: string
}

/**
 * IconSelector component for choosing account icons
 *
 * @example
 * ```tsx
 * <IconSelector
 *   value={account.custom_icon}
 *   onChange={(icon) => setIcon(icon)}
 * />
 * ```
 */
export function IconSelector({
  value,
  onChange,
  className,
  iconColor,
}: IconSelectorProps) {
  return (
    <div className={cn('space-y-3', className)}>
      {/* None option */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => onChange(null)}
          className={cn(
            'flex items-center gap-2 px-3 py-2 rounded-md border transition-colors',
            value === null || value === undefined
              ? 'border-primary bg-primary/10 text-primary'
              : 'border-input hover:bg-accent hover:text-accent-foreground'
          )}
          aria-pressed={value === null || value === undefined}
        >
          <X className="h-4 w-4" />
          <span className="text-sm">No Icon</span>
        </button>
        {value && (
          <div
            className="flex items-center gap-2 px-3 py-2 rounded-md border border-input bg-muted/50"
          >
            <AccountIconDisplay
              icon={value}
              className="h-4 w-4"
              style={iconColor ? { color: iconColor } : undefined}
            />
            <span className="text-sm text-muted-foreground">
              Current: {ICON_LABELS[value]}
            </span>
          </div>
        )}
      </div>

      {/* Icon grid */}
      <div className="grid grid-cols-5 gap-2">
        {VALID_ACCOUNT_ICONS.map((icon) => {
          const isSelected = value === icon

          return (
            <button
              key={icon}
              type="button"
              onClick={() => onChange(icon)}
              className={cn(
                'flex flex-col items-center gap-1 p-2 rounded-md border transition-all hover:scale-105',
                isSelected
                  ? 'border-primary bg-primary/10 ring-2 ring-primary ring-offset-1'
                  : 'border-input hover:bg-accent hover:text-accent-foreground'
              )}
              title={ICON_LABELS[icon]}
              aria-label={`Select ${ICON_LABELS[icon]} icon`}
              aria-pressed={isSelected}
            >
              <AccountIconDisplay
                icon={icon}
                className="h-5 w-5"
                style={iconColor && isSelected ? { color: iconColor } : undefined}
              />
              <span className="text-[10px] text-muted-foreground truncate w-full text-center">
                {ICON_LABELS[icon]}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
