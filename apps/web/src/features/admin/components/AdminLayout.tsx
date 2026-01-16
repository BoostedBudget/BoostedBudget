import { Outlet } from 'react-router-dom'
import { Shield } from 'lucide-react'
import { AdminSidebar } from './AdminSidebar'
import './AdminLayout.css'

export function AdminLayout() {
  return (
    <div className="admin-layout">
      <div className="admin-layout__header">
        <Shield size={18} />
        <span>
          <strong>Admin Mode</strong> — You are viewing as administrator
        </span>
      </div>

      <div className="admin-layout__container">
        <AdminSidebar />
        <main className="admin-layout__content">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
