import { Outlet } from 'react-router-dom'
import { Shield } from 'lucide-react'
import { AdminSidebar } from './AdminSidebar'

export function AdminLayout() {
  return (
    <div className="flex flex-col min-h-screen">
      <div className="flex items-center justify-center gap-2 px-4 py-2 text-sm bg-amber-500 text-amber-900 dark:bg-amber-800 dark:text-amber-100">
        <Shield size={18} />
        <span>
          <strong>Admin Mode</strong> — You are viewing as administrator
        </span>
      </div>

      <div className="flex flex-1">
        <AdminSidebar />
        <main className="flex-1 p-6 bg-background min-h-screen">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
