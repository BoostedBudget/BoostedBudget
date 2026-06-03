import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import type { QuickActionLinkProps } from '../types'

export function QuickActionLink({ to, label, description, icon }: QuickActionLinkProps) {
  return (
    <Link
      to={to}
      className="group flex items-center gap-4 p-4 bg-card border border-border rounded-lg no-underline hover:bg-accent hover:border-border/80 transition-all duration-200"
    >
      <div className="flex items-center justify-center w-10 h-10 bg-muted rounded-lg text-muted-foreground">
        {icon}
      </div>
      <div className="flex-1 flex flex-col gap-1">
        <span className="font-semibold text-foreground">{label}</span>
        <span className="text-sm text-muted-foreground">{description}</span>
      </div>
      <ChevronRight
        className="text-muted-foreground/60 transition-transform duration-200 group-hover:translate-x-1 group-hover:text-muted-foreground"
        size={20}
      />
    </Link>
  )
}
