export function privacySettingsQueryKey(userId: string | undefined) {
  return ['/api/v1/user/privacy', userId ?? 'identity-pending'] as const
}
