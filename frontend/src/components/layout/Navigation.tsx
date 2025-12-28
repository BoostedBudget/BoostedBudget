import { NavLink } from 'react-router-dom'
import { cn } from '@/lib/utils'

interface NavigationProps {
  className?: string
  orientation?: 'horizontal' | 'vertical'
  onLinkClick?: () => void
}

const navItems = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/accounts', label: 'Accounts' },
  { to: '/transactions', label: 'Transactions' },
  { to: '/budgets', label: 'Budgets' },
  { to: '/rules', label: 'Rules' },
  { to: '/auto-categorization', label: 'Auto-Cat' },
]

export function Navigation({
  className,
  orientation = 'horizontal',
  onLinkClick,
}: NavigationProps) {
  return (
    <nav
      aria-label="Main navigation"
      className={cn(
        'flex',
        orientation === 'horizontal' ? 'items-center gap-6' : 'flex-col gap-2',
        className
      )}
    >
      {navItems.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          onClick={onLinkClick}
          className={({ isActive }) =>
            cn(
              'text-sm font-medium transition-colors',
              orientation === 'vertical' && 'py-2 px-3 rounded-md w-full',
              isActive
                ? 'text-foreground font-semibold'
                : 'text-muted-foreground hover:text-foreground'
            )
          }
        >
          {item.label}
        </NavLink>
      ))}
    </nav>
  )
}
