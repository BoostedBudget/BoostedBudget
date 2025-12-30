import { Link } from 'react-router-dom'
import { useAuth, useUser, useClerk } from '@clerk/clerk-react'
import { Navigation } from './Navigation'
import { UserDropdown } from './UserDropdown'
import { MobileNav } from './MobileNav'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface HeaderProps {
  className?: string
}

export function Header({ className }: HeaderProps) {
  const { isSignedIn: isAuthenticated } = useAuth()
  const { user: clerkUser } = useUser()
  const { openSignIn, signOut } = useClerk()

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
        'sticky top-0 z-50',
        'bg-background/80 backdrop-blur-sm',
        'border-b border-border',
        'px-4 md:px-6 h-16',
        'flex items-center justify-between',
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

      {/* Right side actions */}
      <div className="flex items-center gap-4">
        {/* Desktop: Login or User Dropdown */}
        {isAuthenticated ? (
          <UserDropdown user={user || {}} onLogout={handleLogout} className="hidden md:flex" />
        ) : (
          <Button onClick={handleLogin} className="hidden md:inline-flex">
            Log In
          </Button>
        )}

        {/* Mobile Navigation */}
        <MobileNav
          isAuthenticated={isAuthenticated ?? false}
          user={user || null}
          onLogout={handleLogout}
          onLogin={handleLogin}
        />
      </div>
    </header>
  )
}
