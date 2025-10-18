import { useAuth0 } from '@auth0/auth0-react'

interface LogoutButtonProps {
  className?: string
  disabled?: boolean
}

export const LogoutButton: React.FC<LogoutButtonProps> = ({ className, disabled }) => {
  const { logout } = useAuth0()

  const handleLogout = () => {
    logout({
      logoutParams: {
        returnTo: window.location.origin,
      },
    })
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
