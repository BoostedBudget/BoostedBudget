jest.mock('expo-router', () => {
  const React = require('react')
  const { Text } = require('react-native')
  const Stack = ({ children }: { children?: React.ReactNode }) => children ?? null
  Stack.Screen = ({ name }: { name: string }) => React.createElement(Text, null, name)
  return {
    Link: ({ children }: { children: React.ReactNode }) => children,
    Stack,
  }
})
