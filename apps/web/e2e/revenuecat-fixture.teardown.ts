import { fixtureDatabase, verifyRevenueCatFixtureCompletion } from '../src/test/support/revenuecat-e2e-fixture'

export default function verifyRevenueCatFixture(): void {
  fixtureDatabase()
  verifyRevenueCatFixtureCompletion(process.env.E2E_REVENUECAT_FIXTURE_PID)
}
