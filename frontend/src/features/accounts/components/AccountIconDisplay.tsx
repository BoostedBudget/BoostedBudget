/**
 * AccountIconDisplay Component
 *
 * Displays the account icon based on the icon identifier.
 * Extracts icon rendering logic to avoid dynamic component creation during render.
 *
 * Issue #307: Account Customization
 */

import {
  Wallet,
  PiggyBank,
  CreditCard,
  Building,
  Building2,
  Briefcase,
  DollarSign,
  Euro,
  PoundSterling,
  JapaneseYen,
  BarChart3,
  TrendingUp,
  TrendingDown,
  Lock,
  Unlock,
  Star,
  Heart,
  Home,
  Car,
  Plane,
  ShoppingCart,
  Gift,
  Coffee,
  Utensils,
  GraduationCap,
} from 'lucide-react'
import type { AccountIcon } from '../types'

export interface AccountIconDisplayProps {
  /** Icon identifier */
  icon: AccountIcon
  /** CSS class name for the icon */
  className?: string
  /** Inline style for the icon */
  style?: React.CSSProperties
}

/**
 * AccountIconDisplay component for rendering account icons
 *
 * @example
 * ```tsx
 * <AccountIconDisplay icon="wallet" className="h-5 w-5" />
 * ```
 */
export function AccountIconDisplay({ icon, className, style }: AccountIconDisplayProps) {
  switch (icon) {
    case 'wallet':
      return <Wallet className={className} style={style} />
    case 'piggy-bank':
      return <PiggyBank className={className} style={style} />
    case 'credit-card':
      return <CreditCard className={className} style={style} />
    case 'bank':
      return <Building className={className} style={style} />
    case 'building':
      return <Building2 className={className} style={style} />
    case 'briefcase':
      return <Briefcase className={className} style={style} />
    case 'dollar':
      return <DollarSign className={className} style={style} />
    case 'euro':
      return <Euro className={className} style={style} />
    case 'pound':
      return <PoundSterling className={className} style={style} />
    case 'yen':
      return <JapaneseYen className={className} style={style} />
    case 'chart':
      return <BarChart3 className={className} style={style} />
    case 'trending-up':
      return <TrendingUp className={className} style={style} />
    case 'trending-down':
      return <TrendingDown className={className} style={style} />
    case 'lock':
      return <Lock className={className} style={style} />
    case 'unlock':
      return <Unlock className={className} style={style} />
    case 'star':
      return <Star className={className} style={style} />
    case 'heart':
      return <Heart className={className} style={style} />
    case 'home':
      return <Home className={className} style={style} />
    case 'car':
      return <Car className={className} style={style} />
    case 'plane':
      return <Plane className={className} style={style} />
    case 'shopping-cart':
      return <ShoppingCart className={className} style={style} />
    case 'gift':
      return <Gift className={className} style={style} />
    case 'coffee':
      return <Coffee className={className} style={style} />
    case 'utensils':
      return <Utensils className={className} style={style} />
    case 'graduation-cap':
      return <GraduationCap className={className} style={style} />
    default:
      return null
  }
}
