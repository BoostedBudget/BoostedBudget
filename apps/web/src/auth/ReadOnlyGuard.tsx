import { useReadOnly } from './ReadOnlyContext'

interface ReadOnlyGuardProps {
  children: React.ReactNode
  message?: string
}

export function ReadOnlyGuard({ children, message = 'Subscribe to use this feature' }: ReadOnlyGuardProps) {
  const { isReadOnly } = useReadOnly()

  if (!isReadOnly) {
    return <>{children}</>
  }

  return (
    // inert prevents all interaction — mouse, keyboard, and assistive technology
    <div
      className="relative inline-block"
      title={message}
      aria-label={message}
      inert={true}
    >
      <div className="pointer-events-none opacity-50 select-none" aria-disabled="true">
        {children}
      </div>
    </div>
  )
}
