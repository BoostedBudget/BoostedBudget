import { mock } from 'bun:test'
import * as RealSupportModule from '@boostedbudget/api-client/endpoints/support/support'

export const mockUseGetPostHogSupportIdentity = mock(() => ({
  data: undefined,
  isLoading: false,
  isError: false,
}))

mock.module('@boostedbudget/api-client/endpoints/support/support', () => ({
  ...RealSupportModule,
  useGetPostHogSupportIdentity: mockUseGetPostHogSupportIdentity,
}))
