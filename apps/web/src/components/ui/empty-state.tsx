/* eslint-disable react-refresh/only-export-components */
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"
import type React from "react"

const emptyStateVariants = cva(
  "flex flex-col items-center justify-center text-center",
  {
    variants: {
      size: {
        default:
          "rounded-xl border border-dashed border-border bg-muted/50 px-6 py-12",
        compact: "px-4 py-6",
      },
    },
    defaultVariants: {
      size: "default",
    },
  }
)

const iconVariants = cva("text-muted-foreground mb-3", {
  variants: {
    size: {
      default: "[&>svg]:h-12 [&>svg]:w-12",
      compact: "[&>svg]:h-8 [&>svg]:w-8",
    },
  },
  defaultVariants: { size: "default" },
})

interface EmptyStateProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof emptyStateVariants> {
  icon: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
}

function EmptyState({
  icon,
  title,
  description,
  action,
  size,
  className,
  ...props
}: EmptyStateProps) {
  return (
    <div
      data-slot="empty-state"
      role="status"
      className={cn(emptyStateVariants({ size }), className)}
      {...props}
    >
      <div className={cn(iconVariants({ size }))} aria-hidden="true">
        {icon}
      </div>
      <h3 className="text-base font-medium text-foreground">{title}</h3>
      {description && (
        <p className="mt-1 text-sm text-muted-foreground max-w-sm">{description}</p>
      )}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

export { EmptyState, emptyStateVariants }
