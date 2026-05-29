/**
 * Curated icon registry for custom categories (Issue #1203).
 *
 * Icons are stored as stable string keys (e.g. "utensils") on the category, and
 * resolved to lucide-react components here. Keeping a curated set (rather than all
 * of lucide) keeps the bundle small and the picker focused on finance-relevant icons.
 */
import {
  Tag,
  Utensils,
  Coffee,
  ShoppingCart,
  ShoppingBag,
  Car,
  Bus,
  Fuel,
  Plane,
  Home,
  HeartPulse,
  Pill,
  Dumbbell,
  Gift,
  Gamepad2,
  Music,
  Film,
  Scissors,
  Wifi,
  Droplet,
  Zap,
  Landmark,
  PiggyBank,
  CreditCard,
  Briefcase,
  GraduationCap,
  PawPrint,
  Baby,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'

/** Stable string key → lucide icon component. */
export const CATEGORY_ICONS: Record<string, LucideIcon> = {
  tag: Tag,
  utensils: Utensils,
  coffee: Coffee,
  'shopping-cart': ShoppingCart,
  'shopping-bag': ShoppingBag,
  car: Car,
  bus: Bus,
  fuel: Fuel,
  plane: Plane,
  home: Home,
  'heart-pulse': HeartPulse,
  pill: Pill,
  dumbbell: Dumbbell,
  gift: Gift,
  'gamepad-2': Gamepad2,
  music: Music,
  film: Film,
  scissors: Scissors,
  wifi: Wifi,
  droplet: Droplet,
  zap: Zap,
  landmark: Landmark,
  'piggy-bank': PiggyBank,
  'credit-card': CreditCard,
  briefcase: Briefcase,
  'graduation-cap': GraduationCap,
  'paw-print': PawPrint,
  baby: Baby,
  sparkles: Sparkles,
}

/** Fallback icon key used when a category has no icon or an unknown key. */
export const DEFAULT_ICON_KEY = 'tag'

/** Ordered list of selectable icon keys. */
export const CATEGORY_ICON_KEYS: string[] = Object.keys(CATEGORY_ICONS)

/** Resolve a string key to a lucide component, falling back to Tag. */
export function resolveCategoryIcon(name?: string | null): LucideIcon {
  if (name && CATEGORY_ICONS[name]) {
    return CATEGORY_ICONS[name]
  }
  return Tag
}
