/**
 * Banner Variants
 *
 * Shared CVA for full-width, dismissible top-of-page banners (e.g.
 * `SubscriptionStatusBanner`, `HouseholdOverCapacityBanner`). Mirrors
 * `components/ui/alert.tsx`'s pattern with semantic design tokens — no raw
 * color literals (tailwind consolidation policy).
 *
 * Extracted from `SubscriptionStatusBanner` (originally module-local) so
 * multiple features can share identical banner styling instead of hand-typing
 * duplicate class strings.
 */
import { cva, type VariantProps } from 'class-variance-authority'

export const bannerVariants = cva(
  'border-b px-4 py-3 flex items-center justify-between gap-4',
  {
    variants: {
      variant: {
        warning: 'bg-warning/10 border-warning/30 text-warning-foreground',
        destructive: 'bg-destructive/10 border-destructive/30 text-destructive-foreground',
        // text-foreground instead of text-info: bg-info/10 yields ~3.1:1 with text-info (WCAG AA requires 4.5:1)
        info: 'bg-info/10 border-info/30 text-foreground',
      },
    },
    defaultVariants: {
      variant: 'info',
    },
  }
)

export type BannerVariants = VariantProps<typeof bannerVariants>

/**
 * Shared CTA styling for the "warning" banner's action link/button — used
 * identically by `SubscriptionStatusBanner` (past_due "Update payment
 * method") and `HouseholdOverCapacityBanner` ("Manage household"). Only a
 * `warning` variant exists today because that's the one genuinely
 * duplicated across features; add more variants only when another banner
 * needs an identical (not merely similar) CTA treatment.
 */
export const bannerCtaVariants = cva(
  'shrink-0 px-3 py-1.5 text-sm font-medium rounded-lg transition-colors',
  {
    variants: {
      variant: {
        warning: 'bg-warning text-warning-foreground hover:bg-warning/90',
      },
    },
  }
)

export type BannerCtaVariants = VariantProps<typeof bannerCtaVariants>
