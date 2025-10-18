import { useAuth0 } from '@auth0/auth0-react'

interface LoginButtonProps {
  className?: string
  disabled?: boolean
}

export const LoginButton: React.FC<LoginButtonProps> = ({ className, disabled }) => {
  const { loginWithRedirect } = useAuth0()

  return (
    <button
      onClick={() => loginWithRedirect()}
      className={className}
      disabled={disabled}
    >
      Log In
    </button>
  )
}
