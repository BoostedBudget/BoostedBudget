import { NavLink } from 'react-router'
import { cn } from '@/lib/utils'
// #1601: concrete modules, NOT the '@/features/admin' barrel — see App.tsx's note. Navigation
// is rendered by Header, which the landing route imports eagerly, so a barrel import here
// reaches the landing critical path too.
import { useIsAdmin } from '@/features/admin/hooks/useIsAdmin'
import { useAdminMode } from '@/features/admin/hooks/useAdminMode'
import { ADMIN_NAV_ITEMS } from '@/features/admin/constants'

interface NavigationProps {
  className?: string
  orientation?: 'horizontal' | 'vertical'
  onLinkClick?: () => void
}

const baseNavItems = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/accounts', label: 'Accounts' },
  { to: '/transactions', label: 'Transactions' },
  // Issue #1300: /budgets + /savings consolidated under /planning
  { to: '/planning', label: 'Planning' },
  // Issue #1300: /reports + /net-worth consolidated under /insights
  { to: '/insights', label: 'Insights' },
  { to: '/boosts', label: 'Boosts' },
  { to: '/auto-categorization', label: 'Auto-Cat' },
]

export function Navigation({ className, orientation = 'horizontal', onLinkClick }: NavigationProps) {
  const { isAdmin } = useIsAdmin()
  const { adminModeEnabled } = useAdminMode()

  const navItems = isAdmin && adminModeEnabled ? [...baseNavItems, ...ADMIN_NAV_ITEMS] : baseNavItems

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
              'flex items-center gap-1.5 text-sm font-medium transition-colors',
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
