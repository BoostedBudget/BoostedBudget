/**
 * CategoryIcon renders the lucide icon for a custom-category icon key (Issue #1203).
 * Falls back to the Tag icon for an empty or unknown key.
 *
 * Uses createElement (rather than `<Icon/>`) because the icon component is resolved
 * dynamically from a string key — this keeps the React Compiler's static-component
 * lint rule satisfied while still rendering a module-level lucide component.
 */
import { createElement, type ComponentType } from 'react'
import { resolveCategoryIcon } from './icons'

interface CategoryIconProps {
  name?: string | null
  className?: string
  'aria-hidden'?: boolean
}

export function CategoryIcon({ name, className, 'aria-hidden': ariaHidden = true }: CategoryIconProps) {
  // Cast to a permissive component type so data-* attributes (used in tests) pass
  // createElement's strict prop typing; lucide icons render them on the <svg> at runtime.
  const Icon = resolveCategoryIcon(name) as ComponentType<Record<string, unknown>>
  return createElement(Icon, {
    className,
    'aria-hidden': ariaHidden,
    'data-testid': 'category-icon',
  })
}
