import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth, useUser, useClerk } from '@clerk/react-router'
import { Inbox } from 'lucide-react'
import { Navigation } from './Navigation'
import { UserDropdown } from './UserDropdown'
import { MobileNav } from './MobileNav'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { NotificationBell, NotificationDrawer } from '@/features/notifications'
import { useLoginEnabled } from '@/shared/hooks'
import { useReviewSummary } from '@/features/transactions/hooks/useReviewSummary'

function ReviewInboxButton() {
  const { summary } = useReviewSummary()
  const unreviewedCount = summary?.unreviewed_count ?? 0

  return (
    <Button variant="ghost" size="icon" className="relative" asChild>
      <Link
        to="/review"
        aria-label={unreviewedCount > 0 ? `Review inbox, ${unreviewedCount} unreviewed` : 'Review inbox'}
      >
        <Inbox className="h-5 w-5" />
        {unreviewedCount > 0 && (
          <span className="absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-warning text-warning-foreground text-[10px] font-semibold px-0.5">
            {unreviewedCount > 99 ? '99+' : unreviewedCount}
          </span>
        )}
      </Link>
    </Button>
  )
}

interface HeaderProps {
  className?: string
}

export function Header({ className }: HeaderProps) {
  const { isSignedIn: isAuthenticated } = useAuth()
  const { user: clerkUser } = useUser()
  const { openSignIn, signOut } = useClerk()
  const [notifOpen, setNotifOpen] = useState(false)
  const { enabled: loginEnabled } = useLoginEnabled()

  // Map Clerk user to a format compatible with existing components
  const user = clerkUser ? {
    name: clerkUser.fullName || undefined,
    email: clerkUser.primaryEmailAddress?.emailAddress,
    picture: clerkUser.imageUrl,
  } : {}

  const handleLogout = () => {
    signOut({ redirectUrl: window.location.origin })
  }

  const handleLogin = () => {
    openSignIn()
  }

  return (
    <header
      role="banner"
      className={cn(
        'sticky top-0 z-50 bg-background/80 backdrop-blur-sm border-b border-border',
        'px-4 md:px-6 h-16 flex items-center justify-between',
        className
      )}
    >
      {/* Logo */}
      <Link
        to={isAuthenticated ? '/dashboard' : '/'}
        className="flex items-center gap-2"
        aria-label="BoostedBudget"
      >
        <span className="text-2xl">🚀</span>
        <span className="font-bold text-xl hidden sm:inline">BoostedBudget</span>
      </Link>

      {/* Desktop Navigation */}
      {isAuthenticated && <Navigation className="hidden md:flex" />}

      {/* Notification Drawer */}
      {isAuthenticated && (
        <NotificationDrawer open={notifOpen} onClose={() => setNotifOpen(false)} />
      )}

      {/* Right side actions */}
      <div className="flex items-center gap-4">
        {/* Review Inbox Button */}
        {isAuthenticated && <ReviewInboxButton />}

        {/* Notification Bell */}
        {isAuthenticated && <NotificationBell onOpen={() => setNotifOpen(true)} />}

        {/* Desktop: Login or User Dropdown */}
        {isAuthenticated ? (
          <UserDropdown user={user || {}} onLogout={handleLogout} className="hidden md:flex" />
        ) : (
          loginEnabled && (
            <Button onClick={handleLogin} className="hidden md:inline-flex">
              Log In
            </Button>
          )
        )}

        {/* Mobile Navigation */}
        <MobileNav
          isAuthenticated={isAuthenticated ?? false}
          user={user || null}
          onLogout={handleLogout}
          onLogin={handleLogin}
          loginEnabled={loginEnabled}
        />
      </div>
    </header>
  )
}
