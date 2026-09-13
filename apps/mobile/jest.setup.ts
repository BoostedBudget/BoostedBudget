jest.mock('expo-router', () => {
  const Stack = ({ children }: { children?: React.ReactNode }) => children ?? null
  Stack.Screen = () => null
  const fonts = {
    regular: { fontFamily: 'System', fontWeight: '400' },
    medium: { fontFamily: 'System', fontWeight: '500' },
    bold: { fontFamily: 'System', fontWeight: '600' },
    heavy: { fontFamily: 'System', fontWeight: '700' },
  }
  return {
    DarkTheme: { dark: true, colors: {}, fonts },
    DefaultTheme: { dark: false, colors: {}, fonts },
    Link: ({ children }: { children: React.ReactNode }) => children,
    Stack,
    ThemeProvider: jest.fn(({ children }: { children: React.ReactNode }) => children),
  }
})

jest.mock('expo-status-bar', () => ({
  StatusBar: jest.fn(() => null),
}))
