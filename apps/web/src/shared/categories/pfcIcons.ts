import {
  Banknote,
  Clapperboard,
  UtensilsCrossed,
  ShoppingCart,
  Wrench,
  Building2,
  Hammer,
  TrendingUp,
  CreditCard,
  HeartPulse,
  Sparkles,
  Home,
  ArrowDownCircle,
  ArrowUpCircle,
  Car,
  Plane,
  CircleHelp,
  type LucideIcon,
} from 'lucide-react'

const PFC_ICONS: Record<string, LucideIcon> = {
  BANK_FEES: Banknote,
  ENTERTAINMENT: Clapperboard,
  FOOD_AND_DRINK: UtensilsCrossed,
  GENERAL_MERCHANDISE: ShoppingCart,
  GENERAL_SERVICES: Wrench,
  GOVERNMENT_AND_NON_PROFIT: Building2,
  HOME_IMPROVEMENT: Hammer,
  INCOME: TrendingUp,
  LOAN_DISBURSEMENTS: CreditCard,
  LOAN_PAYMENTS: CreditCard,
  MEDICAL: HeartPulse,
  PERSONAL_CARE: Sparkles,
  RENT_AND_UTILITIES: Home,
  TRANSFER_IN: ArrowDownCircle,
  TRANSFER_OUT: ArrowUpCircle,
  TRANSPORTATION: Car,
  TRAVEL: Plane,
  OTHER: CircleHelp,
}

export function getCategoryIcon(primary: string): LucideIcon | null {
  return PFC_ICONS[primary] ?? null
}
