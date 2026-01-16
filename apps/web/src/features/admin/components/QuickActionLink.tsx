import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import type { QuickActionLinkProps } from '../types'
import './QuickActionLink.css'

export function QuickActionLink({ to, label, description, icon }: QuickActionLinkProps) {
  return (
    <Link to={to} className="quick-action-link">
      <div className="quick-action-link__icon">{icon}</div>
      <div className="quick-action-link__content">
        <span className="quick-action-link__label">{label}</span>
        <span className="quick-action-link__description">{description}</span>
      </div>
      <ChevronRight className="quick-action-link__arrow" size={20} />
    </Link>
  )
}
