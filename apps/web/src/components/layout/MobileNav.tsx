import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Menu, LogOut, LogIn, User, CreditCard } from 'lucide-react'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Navigation } from './Navigation'
import { cn } from '@/lib/utils'

interface MobileNavProps {
  isAuthenticated: boolean
  user: {
    name?: string
    email?: string
    picture?: string
  } | null
  onLogout: () => void
  onLogin: () => void
  className?: string
}

export function MobileNav({
  isAuthenticated,
  user,
  onLogout,
  onLogin,
  className,
}: MobileNavProps) {
  const [open, setOpen] = useState(false)

  const handleLinkClick = () => {
    setOpen(false)
  }

  const initials =
    user?.name
      ?.split(' ')
      .map((n) => n[0])
      .join('')
      .toUpperCase()
      .slice(0, 2) || '??'

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className={cn('md:hidden', className)}
          aria-label="Open menu"
        >
          <Menu className="h-6 w-6" />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-[300px] sm:w-[350px]">
        <SheetHeader>
          <SheetTitle className="text-left">
            <Link to="/" className="flex items-center gap-2" onClick={handleLinkClick}>
              <span className="text-2xl">🚀</span>
              <span className="font-bold">BoostedBudget</span>
            </Link>
          </SheetTitle>
        </SheetHeader>

        <div className="mt-8 flex flex-col gap-4">
          {isAuthenticated && (
            <>
              <Navigation orientation="vertical" onLinkClick={handleLinkClick} />

              <div className="border-t pt-4">
                <div className="flex items-center gap-3 mb-4">
                  <Avatar className="h-10 w-10">
                    <AvatarImage src={user?.picture} alt={user?.name || 'User'} />
                    <AvatarFallback>{initials}</AvatarFallback>
                  </Avatar>
                  <div className="flex flex-col">
                    <span className="text-sm font-medium">{user?.name}</span>
                    <span className="text-xs text-muted-foreground">{user?.email}</span>
                  </div>
                </div>

                <Link
                  to="/profile"
                  onClick={handleLinkClick}
                  className="flex items-center gap-2 py-2 px-3 rounded-md text-sm hover:bg-accent w-full"
                >
                  <User className="h-4 w-4" />
                  View Profile
                </Link>

                <Link
                  to="/billing/manage"
                  onClick={handleLinkClick}
                  className="flex items-center gap-2 py-2 px-3 rounded-md text-sm hover:bg-accent w-full"
                >
                  <CreditCard className="h-4 w-4" />
                  Manage Subscription
                </Link>

                <Button
                  variant="ghost"
                  onClick={() => {
                    onLogout()
                    setOpen(false)
                  }}
                  className="w-full justify-start mt-2"
                >
                  <LogOut className="mr-2 h-4 w-4" />
                  Log out
                </Button>
              </div>
            </>
          )}

          {!isAuthenticated && (
            <Button
              onClick={() => {
                onLogin()
                setOpen(false)
              }}
              className="w-full"
            >
              <LogIn className="mr-2 h-4 w-4" />
              Log In
            </Button>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
