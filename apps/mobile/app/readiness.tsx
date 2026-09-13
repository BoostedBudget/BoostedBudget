import { useState } from 'react'
import { Platform, Pressable, Text, View } from 'react-native'
import { asBillingPlatform, selectBillingProvider } from '../src/billing/provider'
import { describePublicEnvironment, MobilePublicEnvironment } from '../src/config/public-env'

interface ReadinessScreenProps {
  environment?: MobilePublicEnvironment
  platform?: string
}

const billingNames = { revenuecat: 'RevenueCat', paddle: 'Paddle' } as const

export default function ReadinessScreen({
  environment = { EXPO_PUBLIC_API_URL: process.env.EXPO_PUBLIC_API_URL },
  platform = Platform.OS,
}: ReadinessScreenProps) {
  const [checked, setChecked] = useState(false)
  const status = describePublicEnvironment(environment)
  const billingPlatform = asBillingPlatform(platform)
  const billingProvider = billingPlatform ? selectBillingProvider(billingPlatform) : null
  const apiStatus = status.apiUrl.valid ? 'configured and valid' : 'missing or invalid'
  const billingStatus = billingProvider
    ? `${billingNames[billingProvider]} (contract only)`
    : 'unavailable on this platform'

  return (
    <View className="flex-1 gap-5 bg-background px-6 py-10">
      <Text accessibilityRole="header" className="text-2xl font-bold text-foreground">
        App readiness
      </Text>
      <View accessibilityRole="summary" className="gap-2 rounded-lg border border-border bg-card p-4">
        <Text className="text-card-foreground">API URL: {apiStatus}</Text>
        <Text className="text-card-foreground">Billing adapter: {billingStatus}</Text>
        <Text className="text-muted-foreground">Authentication: deferred</Text>
        <Text className="text-muted-foreground">Offline cache: deferred</Text>
      </View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Check configuration"
        className="rounded-lg bg-primary px-5 py-3"
        onPress={() => setChecked(true)}
      >
        <Text className="text-center font-semibold text-primary-foreground">Check configuration</Text>
      </Pressable>
      {checked ? (
        <Text accessibilityLiveRegion="polite" className="text-foreground">
          API URL: {apiStatus}
        </Text>
      ) : null}
    </View>
  )
}
