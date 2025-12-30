import { useClerk } from '@clerk/clerk-react'

interface LogoutButtonProps {
  className?: string
  disabled?: boolean
}

export const LogoutButton: React.FC<LogoutButtonProps> = ({ className, disabled }) => {
  const { signOut } = useClerk()

  const handleLogout = () => {
    signOut({ redirectUrl: window.location.origin })
  }

  return (
    <button
      onClick={handleLogout}
      className={className}
      disabled={disabled}
    >
      Log Out
    </button>
  )
}
