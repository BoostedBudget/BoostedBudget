jest.mock('expo-router', () => {
  const Stack = ({ children }: { children?: React.ReactNode }) => children ?? null
  Stack.Screen = () => null
  return {
    Link: ({ children }: { children: React.ReactNode }) => children,
    Stack,
  }
})
