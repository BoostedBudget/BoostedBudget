import { Link } from 'expo-router'
import { Pressable, Text, View } from 'react-native'
import { useTheme } from '../src/theme/ThemeProvider'

export default function HomeScreen() {
  const { resolvedTheme } = useTheme()

  return (
    <View className="flex-1 items-center justify-center gap-6 bg-background px-6">
      <Text accessibilityRole="header" className="text-3xl font-bold text-foreground">
        BoostedBudget
      </Text>
      <Text className="text-center text-base text-muted-foreground">
        Universal app foundation for iOS, Android, and mobile web.
      </Text>
      <Text className="text-sm text-muted-foreground">Resolved theme: {resolvedTheme}</Text>
      <Link href="/readiness" asChild>
        <Pressable accessibilityRole="link" accessibilityLabel="View app readiness" className="rounded-lg bg-primary px-5 py-3">
          <Text className="font-semibold text-primary-foreground">View app readiness</Text>
        </Pressable>
      </Link>
    </View>
  )
}