import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Users, CreditCard, Activity, Webhook } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ADMIN_SIDEBAR_ITEMS } from '../constants'

const iconMap: Record<string, React.ComponentType<{ size?: number }>> = {
  LayoutDashboard,
  Users,
  CreditCard,
  Activity,
  Webhook,
}

export function AdminSidebar() {
  return (
    <nav
      className="w-60 min-h-screen bg-card border-r border-border py-4 flex flex-col hidden md:flex"
      aria-label="Admin navigation"
    >
      <ul className="list-none m-0 p-0">
        {ADMIN_SIDEBAR_ITEMS.map((item) => {
          const Icon = iconMap[item.icon]
          return (
            <li key={item.to}>
              <NavLink
                to={item.to}
                end={item.to === '/admin'}
                className={({ isActive }) =>
                  cn(
                    'flex items-center gap-3 px-6 py-3 text-muted-foreground font-medium no-underline transition-all duration-200',
                    'hover:bg-accent hover:text-foreground',
                    isActive &&
                      'bg-amber-50 text-amber-900 border-r-4 border-amber-500 dark:bg-amber-950 dark:text-amber-200 dark:border-amber-500 hover:bg-amber-50 hover:text-amber-900 dark:hover:bg-amber-950 dark:hover:text-amber-200'
                  )
                }
              >
                {Icon && <Icon size={20} />}
                <span>{item.label}</span>
              </NavLink>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
