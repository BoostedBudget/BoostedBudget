import * as React from "react"
import { cn } from "@/lib/utils"

export interface SwitchProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, 'onChange'> {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
}

/**
 * Switch — boolean on/off toggle control.
 * Uses a native <button role="switch"> for WCAG compliance without Radix.
 * On state: bg-success · Off state: bg-muted-foreground/30
 */
function Switch({
  checked,
  onCheckedChange,
  disabled,
  className,
  onClick: callerOnClick,
  onKeyDown: callerOnKeyDown,
  ...restProps
}: SwitchProps) {
  const toggle = () => {
    if (!disabled) onCheckedChange(!checked)
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggle()
    }
    callerOnKeyDown?.(e)
  }

  return (
    <button
      type="button"
      {...restProps}
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={(e) => { toggle(); callerOnClick?.(e) }}
      onKeyDown={handleKeyDown}
      className={cn(
        "relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        "disabled:cursor-not-allowed disabled:opacity-50",
        checked ? "bg-success" : "bg-muted-foreground/30",
        className,
      )}
    >
      <span
        data-slot="switch-thumb"
        aria-hidden="true"
        className={cn(
          "pointer-events-none block h-4 w-4 rounded-full bg-white shadow-sm ring-0 transition-transform",
          checked ? "translate-x-4" : "translate-x-0.5",
        )}
      />
    </button>
  )
}

export { Switch }
