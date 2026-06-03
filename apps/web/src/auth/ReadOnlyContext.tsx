import { createContext, useContext } from 'react'

interface ReadOnlyContextValue {
  isReadOnly: boolean
  subscriptionStatus: string
}

const ReadOnlyContext = createContext<ReadOnlyContextValue>({
  isReadOnly: false,
  subscriptionStatus: '',
})

interface ReadOnlyProviderProps {
  children: React.ReactNode
  isReadOnly: boolean
  subscriptionStatus?: string
}

export function ReadOnlyProvider({ children, isReadOnly, subscriptionStatus = '' }: ReadOnlyProviderProps) {
  return (
    <ReadOnlyContext.Provider value={{ isReadOnly, subscriptionStatus }}>
      {children}
    </ReadOnlyContext.Provider>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export function useReadOnly(): ReadOnlyContextValue {
  return useContext(ReadOnlyContext)
}
