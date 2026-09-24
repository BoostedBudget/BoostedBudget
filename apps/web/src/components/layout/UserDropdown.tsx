import { Link } from 'react-router'
import { User, Settings, LogOut, CreditCard } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

interface UserDropdownProps {
  user: {
    name?: string
    email?: string
    picture?: string
  }
  onLogout: () => void
  className?: string
}

export function UserDropdown({ user, onLogout, className }: UserDropdownProps) {
  const initials =
    user.name
      ?.split(' ')
      .map((n) => n[0])
      .join('')
      .toUpperCase()
      .slice(0, 2) || '??'

  return (
    <div className={cn(className)}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="relative h-10 w-10 rounded-full"
            aria-label="User menu"
          >
            {/*
             * Issue #1514: PostHog session replay records DOM attributes verbatim;
             * data-ph-mask masks element TEXT only, never attribute values. `alt` stays
             * a generic, non-PII description ("User avatar") rather than the user's
             * name — the visible name/email in the open dropdown carry data-ph-mask
             * instead, so the accessible description and the masked text stay
             * independent (this isn't a decorative image: it's the user's photo, it
             * just doesn't need to repeat what's already announced by the adjacent
             * name text).
             *
             * `src={user.picture}` (Clerk's `imageUrl`) is NOT treated as PII here:
             * Clerk resource URLs/ids are opaque by design (same convention as
             * `user_`/`sess_`/`org_` ids), and for OAuth-sourced avatars (this app
             * enables Google/Microsoft/Apple) the URL points at the provider's own
             * opaque per-account photo resource — neither path embeds the user's name
             * or email as literal text. Investigated for #1514 fix round 2; revisit
             * only if Clerk's URL format changes.
             */}
            <Avatar className="h-10 w-10">
              <AvatarImage src={user.picture} alt="User avatar" />
              <AvatarFallback>{initials}</AvatarFallback>
            </Avatar>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="end" forceMount>
          <DropdownMenuLabel className="font-normal">
            <div className="flex flex-col space-y-1">
              <p className="text-sm font-medium leading-none" data-ph-mask>{user.name}</p>
              <p className="text-xs leading-none text-muted-foreground" data-ph-mask>{user.email}</p>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <Link to="/profile" className="cursor-pointer">
              <User className="mr-2 h-4 w-4" />
              <span>View Profile</span>
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/profile?tab=privacy" className="cursor-pointer">
              <Settings className="mr-2 h-4 w-4" />
              <span>Privacy Settings</span>
            </Link>
          </DropdownMenuItem>
          <DropdownMenuItem asChild>
            <Link to="/billing/manage" className="cursor-pointer">
              <CreditCard className="mr-2 h-4 w-4" />
              <span>Manage Subscription</span>
            </Link>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onLogout} className="cursor-pointer">
            <LogOut className="mr-2 h-4 w-4" />
            <span>Log out</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
