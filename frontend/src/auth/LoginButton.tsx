import { useClerk } from '@clerk/clerk-react'

interface LoginButtonProps {
  className?: string
  disabled?: boolean
}

export const LoginButton: React.FC<LoginButtonProps> = ({ className, disabled }) => {
  const { openSignIn } = useClerk()

  return (
    <button
      onClick={() => openSignIn()}
      className={className}
      disabled={disabled}
    >
      Log In
    </button>
  )
}
