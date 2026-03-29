/**
 * TermsOfService Page
 *
 * Full Terms of Service for BoostedBudget, including Refund & Cancellation Policy
 * (satisfies Paddle domain verification requirement).
 *
 * Issue #858: Legal pages for Paddle domain verification
 * Route: /legal/terms (see App.tsx)
 */

import './PrivacyPolicy.css'
import { Link } from 'react-router-dom'

export function TermsOfService() {
  return (
    <div className="privacy-policy">
      <div className="privacy-policy-container">
        <h1>Terms of Service</h1>
        <p className="last-updated">Last Updated: March 29, 2026</p>

        <section>
          <h2>1. Acceptance of Terms</h2>
          <p>
            Welcome to BoostedBudget ("we," "our," or "us"). By accessing or using the BoostedBudget
            platform, website, or any related services (collectively, the "Service"), you agree to be
            bound by these Terms of Service ("Terms"). If you do not agree to these Terms, do not use
            the Service.
          </p>
          <p>
            These Terms constitute a legally binding agreement between you and BoostedBudget. We may
            update these Terms from time to time as described in Section 19.
          </p>
        </section>

        <section>
          <h2>2. Service Description</h2>
          <p>
            BoostedBudget is a budgeting and financial automation platform that helps you track
            spending, create budgets, and automate financial rules. We connect to your financial
            institutions via Plaid to read account balances and transaction data.
          </p>
          <p className="important-note">
            <strong>Important:</strong> BoostedBudget provides financial suggestions and automation
            only. We do <strong>not</strong> move money on your behalf. All automation rules create
            suggestions that you review and execute manually. Nothing in this Service constitutes
            financial, investment, legal, or tax advice.
          </p>
        </section>

        <section>
          <h2>3. Eligibility</h2>
          <p>
            You must be at least <strong>18 years of age</strong> to use the Service. By using
            BoostedBudget, you represent and warrant that you meet this requirement. We do not
            knowingly collect information from anyone under 18.
          </p>
        </section>

        <section>
          <h2>4. Account Registration and Free Trial</h2>
          <h3>4.1 Account Creation</h3>
          <p>
            To use BoostedBudget, you must create an account and provide accurate information. You
            are responsible for maintaining the security of your account credentials and for all
            activity that occurs under your account.
          </p>

          <h3>4.2 Free Trial</h3>
          <p>
            All new accounts receive a <strong>60-day free trial</strong> of BoostedBudget Premium
            features. During your trial:
          </p>
          <ul>
            <li>No credit card is required</li>
            <li>All Premium features are fully available</li>
            <li>No advertisements are displayed</li>
            <li>One trial per person — trials may not be shared or transferred</li>
          </ul>
          <p>
            After your trial ends, you will have read-only access to your data (see Section 7).
            To restore full access, subscribe to a paid plan at any time.
          </p>
        </section>

        <section>
          <h2>5. Subscription Plans and Billing</h2>
          <h3>5.1 Payment Processing</h3>
          <p>
            Payments are processed by <strong>Paddle</strong>, our authorized reseller and payment
            processor. When you purchase a subscription, you are purchasing from Paddle, who then
            licenses the Service to you. Paddle's terms of service and privacy policy apply to
            your payment transaction.
          </p>

          <h3>5.2 Current Subscription Plans</h3>
          <p>BoostedBudget offers the following paid subscription plans:</p>
          <ul>
            <li><strong>Premium:</strong> $8.99/month or $79.00/year — for individual users</li>
            <li><strong>Household (2):</strong> $12.99/month or $119.00/year — for 2 users</li>
            <li><strong>Household (3):</strong> $14.99/month or $139.00/year — for 3 users</li>
            <li><strong>Household (4):</strong> $16.99/month or $159.00/year — for 4 users</li>
            <li><strong>Household (5+):</strong> $18.99/month or $179.00/year — for 5 users</li>
          </ul>
          <p>
            All paid plans include all Premium features with no feature restrictions between tiers.
            Household plans differ only in the number of users included.
          </p>

          <h3>5.3 Billing</h3>
          <p>
            Subscriptions are billed monthly or annually (your choice at signup). Your subscription
            will <strong>automatically renew</strong> at the end of each billing period unless you
            cancel before the renewal date.
          </p>
          <p>
            All prices are in USD and exclude applicable taxes. Paddle may collect and remit sales
            tax or VAT as required by your jurisdiction.
          </p>
        </section>

        <section>
          <h2>6. Refund and Cancellation Policy</h2>
          <h3>6.1 No Refunds</h3>
          <p>
            <strong>All subscription payments are non-refundable.</strong> We do not provide refunds
            or credits for any partial billing periods, unused time, or unused features.
          </p>

          <h3>6.2 Cancellation</h3>
          <p>
            You may cancel your subscription at any time from your account settings. Upon
            cancellation:
          </p>
          <ul>
            <li>Your access to Premium features continues until the end of your current paid billing period</li>
            <li>You will not be charged for the next billing period</li>
            <li>After your billing period ends, your account transitions to read-only mode (see Section 7)</li>
            <li>Your data is preserved and you may resubscribe at any time to restore full access</li>
          </ul>

          <h3>6.3 Exceptions</h3>
          <p>
            We may issue refunds at our sole discretion in cases of documented technical failures
            or billing errors caused by BoostedBudget. To request a refund under these circumstances,
            contact us at <strong>support@boostedbudget.com</strong> within 14 days of the charge.
          </p>
        </section>

        <section>
          <h2>7. Trial Expiration and Read-Only Access</h2>
          <p>
            When your free trial ends or your subscription is cancelled and your billing period
            expires, your account enters <strong>read-only mode</strong>:
          </p>
          <ul>
            <li>You can log in and view all your existing data: transactions, budgets, reports, and accounts</li>
            <li>No new transaction syncing will occur</li>
            <li>You cannot create, edit, or run automation rules</li>
            <li>You cannot connect new financial accounts</li>
            <li>Premium features (AI insights, advanced reports, API access) are unavailable</li>
          </ul>
          <p>
            Your data is <strong>preserved indefinitely</strong> in read-only mode. You can subscribe
            or resubscribe at any time to immediately restore full access to all features.
          </p>
        </section>

        <section>
          <h2>8. Price Changes</h2>
          <p>
            We reserve the right to change subscription prices. If we change the price of your
            current plan:
          </p>
          <ul>
            <li>We will notify you by email at least <strong>30 days before</strong> the change takes effect</li>
            <li>Price changes apply at your next billing cycle after the 30-day notice period</li>
            <li>You may cancel your subscription before the new price takes effect</li>
            <li>Continued use of the Service after a price change constitutes acceptance of the new price</li>
          </ul>
        </section>

        <section>
          <h2>9. Household Plans and Member Management</h2>
          <h3>9.1 Household Members</h3>
          <p>
            Household plans allow multiple users to share a single subscription. The account owner
            manages household membership within the limits of their plan tier.
          </p>

          <h3>9.2 Downgrading Household Plans</h3>
          <p>
            If you downgrade to a lower household tier or individual plan that supports fewer users:
          </p>
          <ul>
            <li>You (the account owner) must select which members to keep within the new limit</li>
            <li>Removed members lose access to the shared subscription immediately</li>
            <li>Removed members' data (linked accounts, rules, transaction history) is preserved for
              <strong> 30 days</strong> before permanent deletion</li>
            <li>Removed members may create their own independent BoostedBudget account</li>
          </ul>
        </section>

        <section>
          <h2>10. Acceptable Use</h2>
          <p>You agree not to:</p>
          <ul>
            <li>Use the Service for any illegal purpose or in violation of any laws</li>
            <li>Attempt to gain unauthorized access to the Service or its related systems</li>
            <li>Reverse engineer, decompile, or disassemble any part of the Service</li>
            <li>Scrape, data-mine, or use automated tools to access the Service without our written permission</li>
            <li>Impersonate any person or entity or misrepresent your affiliation</li>
            <li>Use the Service to process or store another person's financial data without their consent</li>
            <li>Attempt to interfere with the proper working of the Service</li>
          </ul>
          <p>
            We reserve the right to terminate or suspend accounts that violate these terms.
          </p>
        </section>

        <section>
          <h2>11. Intellectual Property</h2>
          <p>
            <strong>Our property:</strong> BoostedBudget and its original content, features, and
            functionality are and will remain the exclusive property of BoostedBudget and its
            licensors. Our trademarks and trade dress may not be used without our prior written consent.
          </p>
          <p>
            <strong>Your data:</strong> You retain full ownership of your financial data. By using
            the Service, you grant us a limited license to process and store your data solely to
            provide the Service to you. We do not claim ownership of your financial information.
          </p>
        </section>

        <section>
          <h2>12. Third-Party Services</h2>
          <p>
            BoostedBudget integrates with the following third-party services to provide the Service:
          </p>
          <ul>
            <li><strong>Plaid:</strong> Secure bank account connectivity and transaction data — subject to
              Plaid's <a href="https://plaid.com/legal/" target="_blank" rel="noopener noreferrer">Terms of Service</a> and
              End User Privacy Policy</li>
            <li><strong>Paddle:</strong> Payment processing and subscription management — subject to
              Paddle's <a href="https://www.paddle.com/legal/terms" target="_blank" rel="noopener noreferrer">Terms of Service</a></li>
            <li><strong>Clerk:</strong> Authentication and user management — subject to
              Clerk's <a href="https://clerk.com/terms" target="_blank" rel="noopener noreferrer">Terms of Service</a></li>
          </ul>
          <p>
            We are not responsible for the practices or content of these third-party services. Your
            use of these integrations is governed by their respective terms.
          </p>
        </section>

        <section>
          <h2>13. Limitation of Liability</h2>
          <p>
            To the maximum extent permitted by applicable law, BoostedBudget shall not be liable for:
          </p>
          <ul>
            <li>Any indirect, incidental, special, consequential, or punitive damages</li>
            <li>Loss of profits, data, use, goodwill, or other intangible losses</li>
            <li>Damages resulting from your reliance on financial information or suggestions provided by the Service</li>
            <li>Unauthorized access to or use of our servers and your personal or financial data stored therein</li>
            <li>Interruptions or cessation of transmission to or from the Service</li>
          </ul>
          <p>
            Our aggregate liability to you for all claims arising from or related to the Service
            shall not exceed the greater of (a) the amounts you paid to us in the 12 months prior
            to the claim, or (b) $100 USD.
          </p>
        </section>

        <section>
          <h2>14. Disclaimer of Warranties</h2>
          <p>
            The Service is provided on an <strong>"as is"</strong> and <strong>"as available"</strong>{' '}
            basis without any warranties of any kind, either express or implied, including but not
            limited to implied warranties of merchantability, fitness for a particular purpose, or
            non-infringement.
          </p>
          <p>
            We do not warrant that the Service will be uninterrupted, error-free, or free of viruses
            or other harmful components. We do not warrant the accuracy, completeness, or usefulness
            of any financial data provided through the Service.
          </p>
        </section>

        <section>
          <h2>15. Dispute Resolution</h2>
          <h3>15.1 Informal Resolution</h3>
          <p>
            Before initiating any formal dispute process, please contact us at{' '}
            <strong>support@boostedbudget.com</strong> and give us 30 days to resolve the issue
            informally.
          </p>

          <h3>15.2 Binding Arbitration</h3>
          <p>
            If informal resolution fails, any dispute arising from or relating to these Terms or the
            Service shall be resolved by <strong>binding individual arbitration</strong> under the
            rules of the American Arbitration Association (AAA), rather than in court.
          </p>
          <ul>
            <li>Arbitration will be conducted in English</li>
            <li>You may bring claims only in your individual capacity, not as a plaintiff or class member
              in any purported class or representative action</li>
            <li><strong>Class actions and class arbitrations are not permitted</strong></li>
          </ul>

          <h3>15.3 Small Claims Exception</h3>
          <p>
            Either party may bring qualifying claims in small claims court rather than arbitration,
            provided the claim remains in small claims court and is not removed or appealed to a court
            of general jurisdiction.
          </p>

          <h3>15.4 Opt-Out</h3>
          <p>
            You may opt out of binding arbitration by sending written notice to{' '}
            <strong>support@boostedbudget.com</strong> within 30 days of first accepting these Terms.
            Your notice must include your name, address, and a clear statement that you wish to opt
            out of arbitration.
          </p>
        </section>

        <section>
          <h2>16. Governing Law</h2>
          <p>
            These Terms shall be governed by and construed in accordance with the laws of the{' '}
            <strong>State of Delaware</strong>, without regard to its conflict of law provisions.
            For any disputes not subject to arbitration, you consent to the exclusive jurisdiction
            of the state and federal courts located in Delaware.
          </p>
        </section>

        <section>
          <h2>17. Termination</h2>
          <h3>17.1 Termination by You</h3>
          <p>
            You may terminate your account at any time by cancelling your subscription and
            requesting account deletion in your account settings. Account deletion is subject to
            a 30-day grace period as described in our{' '}
            <Link to="/legal/privacy-policy">Privacy Policy</Link>.
          </p>

          <h3>17.2 Termination by Us</h3>
          <p>
            We may suspend or terminate your access to the Service, with or without notice, if:
          </p>
          <ul>
            <li>You violate these Terms or our Acceptable Use policy (Section 10)</li>
            <li>We are required to do so by law</li>
            <li>We discontinue the Service</li>
          </ul>
          <p>
            Upon termination for cause, we may retain your data for the period required by law or
            legitimate business interests before deletion.
          </p>
        </section>

        <section>
          <h2>18. Privacy</h2>
          <p>
            Your privacy is important to us. Our{' '}
            <Link to="/legal/privacy-policy">Privacy Policy</Link> explains how we collect, use,
            and protect your personal and financial data. By using the Service, you agree to our
            Privacy Policy, which is incorporated into these Terms by reference.
          </p>
        </section>

        <section>
          <h2>19. Changes to These Terms</h2>
          <p>
            We may update these Terms to reflect changes in our practices, legal requirements, or
            Service features. For material changes, we will:
          </p>
          <ul>
            <li>Notify you by email at least <strong>30 days before</strong> the changes take effect</li>
            <li>Update the "Last Updated" date at the top of this page</li>
            <li>Post the updated Terms at this URL</li>
          </ul>
          <p>
            Your continued use of the Service after changes are posted and any notice period has
            passed constitutes your acceptance of the updated Terms. If you do not agree to the
            revised Terms, you must stop using the Service before the effective date.
          </p>
        </section>

        <section>
          <h2>20. Contact Us</h2>
          <p>
            If you have questions about these Terms or need support, please contact us:
          </p>
          <ul>
            <li><strong>General Support:</strong> support@boostedbudget.com</li>
            <li><strong>Privacy Inquiries:</strong> privacy@boostedbudget.com</li>
          </ul>
          <p>
            We aim to respond to all inquiries within 2 business days.
          </p>
        </section>

        <hr />

        <div className="privacy-summary">
          <h2>Key Points Summary</h2>
          <ul>
            <li><strong>Free trial:</strong> 60 days, no credit card required, no ads</li>
            <li><strong>After trial:</strong> Read-only access — your data is always preserved</li>
            <li><strong>Refunds:</strong> No refunds for any billing period; access continues until period ends</li>
            <li><strong>Price changes:</strong> 30 days' email notice before any price increase</li>
            <li><strong>No money movement:</strong> We never move money on your behalf</li>
            <li><strong>Disputes:</strong> Resolved by individual binding arbitration under Delaware law</li>
          </ul>
          <p className="contact-cta">
            <strong>Questions?</strong> Contact us at support@boostedbudget.com
          </p>
        </div>
      </div>
    </div>
  )
}
