import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Users, CreditCard, Activity, Webhook } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ADMIN_SIDEBAR_ITEMS } from '../constants'
import './AdminSidebar.css'

const iconMap: Record<string, React.ComponentType<{ size?: number }>> = {
  LayoutDashboard,
  Users,
  CreditCard,
  Activity,
  Webhook,
}

export function AdminSidebar() {
  return (
    <nav className="admin-sidebar" aria-label="Admin navigation">
      <ul className="admin-sidebar__list">
        {ADMIN_SIDEBAR_ITEMS.map((item) => {
          const Icon = iconMap[item.icon]
          return (
            <li key={item.to} className="admin-sidebar__item">
              <NavLink
                to={item.to}
                end={item.to === '/admin'}
                className={({ isActive }) =>
                  cn('admin-sidebar__link', isActive && 'admin-sidebar__link--active')
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
