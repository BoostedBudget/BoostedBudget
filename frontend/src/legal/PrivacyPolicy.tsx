import './PrivacyPolicy.css'

export function PrivacyPolicy() {
  return (
    <div className="privacy-policy">
      <div className="privacy-policy-container">
        <h1>Privacy Policy</h1>
        <p className="last-updated">Last Updated: January 20, 2025</p>

        <section>
          <h2>1. Introduction</h2>
          <p>
            Welcome to BoostedBudget ("we," "our," or "us"). We are committed to protecting your personal
            information and your right to privacy. This Privacy Policy explains how we collect, use, disclose,
            and safeguard your information when you use our budgeting and financial automation platform.
          </p>
          <p>
            BoostedBudget is headquartered in the United States, and all user data is currently stored in the
            <strong> us-east-1 (Virginia, USA) region</strong> as part of our single-region MVP deployment.
          </p>
        </section>

        <section>
          <h2>2. Information We Collect</h2>
          <h3>2.1 Personal Information</h3>
          <p>We collect the following types of personal information:</p>
          <ul>
            <li><strong>Account Information:</strong> Email address, name, Auth0 user ID</li>
            <li><strong>Profile Data:</strong> User preferences, subscription tier, primary data region selection</li>
            <li><strong>Financial Data:</strong> Bank account information, transaction data, balances (via Plaid integration)</li>
            <li><strong>Authentication Tokens:</strong> Encrypted access tokens for third-party financial institutions</li>
            <li><strong>Usage Data:</strong> Application usage statistics, feature interactions, error logs</li>
          </ul>

          <h3>2.2 Automatically Collected Information</h3>
          <ul>
            <li><strong>Cookies:</strong> Necessary, analytics, and advertising cookies (with your consent)</li>
            <li><strong>Device Information:</strong> Browser type, IP address, device identifiers</li>
            <li><strong>Log Data:</strong> Access times, pages viewed, actions performed</li>
          </ul>
        </section>

        <section>
          <h2>3. How We Use Your Information</h2>
          <p>We use your personal information for the following purposes:</p>
          <ul>
            <li><strong>Service Delivery:</strong> Provide budgeting tools, financial insights, and automation rules</li>
            <li><strong>Account Management:</strong> Create and maintain your account, process subscriptions</li>
            <li><strong>Financial Operations:</strong> Connect to financial institutions, sync transactions, execute automation rules</li>
            <li><strong>Communication:</strong> Send service updates, security alerts, marketing emails (with consent)</li>
            <li><strong>Improvement:</strong> Analyze usage patterns to improve our platform</li>
            <li><strong>Security:</strong> Detect fraud, prevent abuse, ensure platform security</li>
            <li><strong>Compliance:</strong> Meet legal obligations, respond to lawful requests</li>
          </ul>
          <p className="important-note">
            <strong>Important:</strong> BoostedBudget provides financial suggestions and automation only.
            We do not move money on your behalf. All automation rules create suggestions that you review and execute manually.
          </p>
        </section>

        <section>
          <h2>4. Data Storage and Security</h2>
          <h3>4.1 Storage Location</h3>
          <p>
            All user data is currently stored in <strong>AWS us-east-1 (Virginia, USA)</strong> region.
            Our platform is designed with future multi-region support in mind (Phase 7+), allowing users
            to choose their preferred data region in compliance with data residency requirements.
          </p>

          <h3>4.2 Security Measures</h3>
          <ul>
            <li><strong>Encryption:</strong> Data encrypted in transit (TLS 1.3) and at rest (AES-256)</li>
            <li><strong>Access Controls:</strong> Role-based access, principle of least privilege</li>
            <li><strong>Authentication:</strong> Auth0-powered authentication with industry-standard security</li>
            <li><strong>Token Protection:</strong> Financial institution tokens encrypted with dedicated encryption keys</li>
            <li><strong>Audit Logging:</strong> All data access and modifications are logged for security monitoring</li>
            <li><strong>Regular Security Audits:</strong> Periodic security assessments and vulnerability scanning</li>
          </ul>
        </section>

        <section>
          <h2>5. Third-Party Services</h2>
          <p>We work with the following trusted third-party service providers:</p>
          <ul>
            <li><strong>Auth0:</strong> Authentication and user management</li>
            <li><strong>Plaid:</strong> Secure bank account connectivity and transaction data</li>
            <li><strong>Stripe:</strong> Payment processing for subscriptions</li>
            <li><strong>Google AdSense:</strong> Advertising for free-tier users (with consent)</li>
            <li><strong>Cloud Infrastructure:</strong> AWS (data hosting), Fly.io (application hosting)</li>
          </ul>
          <p>
            These providers may have access to your information only to perform specific tasks on our behalf
            and are obligated to protect your information in accordance with their privacy policies.
          </p>
        </section>

        <section>
          <h2>6. Cookie Policy</h2>
          <p>We use three types of cookies, in compliance with GDPR requirements:</p>
          <ul>
            <li>
              <strong>Necessary Cookies (Required):</strong> Essential for app functionality, authentication,
              and security. Cannot be disabled.
            </li>
            <li>
              <strong>Analytics Cookies (Optional):</strong> Help us understand how users interact with our platform
              to improve features and user experience. Requires your consent.
            </li>
            <li>
              <strong>Advertising Cookies (Optional):</strong> Enable personalized ads for free-tier users.
              Supports the free version of our platform. Requires your consent.
            </li>
          </ul>
          <p>
            You can manage your cookie preferences through our Cookie Consent banner or in your Privacy Settings.
          </p>
        </section>

        <section>
          <h2>7. Your Privacy Rights (GDPR)</h2>
          <p>
            All BoostedBudget users, regardless of location, have the following GDPR-equivalent rights:
          </p>
          <ul>
            <li><strong>Right to Access:</strong> Request a copy of your personal data</li>
            <li><strong>Right to Rectification:</strong> Correct inaccurate or incomplete data</li>
            <li><strong>Right to Erasure ("Right to be Forgotten"):</strong> Request deletion of your data</li>
            <li><strong>Right to Data Portability:</strong> Export your data in a machine-readable format</li>
            <li><strong>Right to Restrict Processing:</strong> Limit how we use your data</li>
            <li><strong>Right to Object:</strong> Object to certain data processing activities</li>
            <li><strong>Right to Withdraw Consent:</strong> Withdraw consent for analytics and advertising cookies</li>
          </ul>
          <p>
            To exercise any of these rights, please contact us at <strong>privacy@boostedbudget.com</strong>.
            We will respond to your request within 30 days.
          </p>
        </section>

        <section>
          <h2>8. Data Retention and Deletion</h2>
          <h3>8.1 Active Accounts</h3>
          <p>
            We retain your personal information for as long as your account is active or as needed to provide services.
          </p>

          <h3>8.2 Account Deletion</h3>
          <p>
            When you request account deletion, we implement a <strong>30-day grace period</strong> before permanent deletion.
            During this period, you can reactivate your account. After 30 days:
          </p>
          <ul>
            <li>All personal data is permanently deleted</li>
            <li>Financial institution connections are revoked</li>
            <li>Subscription is cancelled (no refunds for partial periods)</li>
            <li>Automation rules and transaction history are erased</li>
          </ul>
          <p>
            Some data may be retained in anonymized form for analytics or as required by law (e.g., financial records).
          </p>

          <h3>8.3 Backup Retention</h3>
          <p>
            Deleted data may persist in encrypted backups for up to 90 days before being permanently purged.
          </p>
        </section>

        <section>
          <h2>9. Data Sharing and Disclosure</h2>
          <p>We do not sell your personal information. We may share your data in the following circumstances:</p>
          <ul>
            <li><strong>With Your Consent:</strong> When you explicitly authorize data sharing</li>
            <li><strong>Service Providers:</strong> Third-party partners performing services on our behalf (Auth0, Plaid, Stripe)</li>
            <li><strong>Legal Compliance:</strong> When required by law, court order, or government request</li>
            <li><strong>Business Transfers:</strong> In the event of a merger, acquisition, or sale of assets</li>
            <li><strong>Fraud Prevention:</strong> To protect against fraud, security threats, or illegal activity</li>
          </ul>
          <p>
            You can control data sharing preferences in your Privacy Settings under "Third-Party Data Sharing."
          </p>
        </section>

        <section>
          <h2>10. Children's Privacy</h2>
          <p>
            BoostedBudget is not intended for users under the age of 18. We do not knowingly collect personal
            information from children. If you believe a child has provided us with personal information, please
            contact us immediately at <strong>privacy@boostedbudget.com</strong>.
          </p>
        </section>

        <section>
          <h2>11. International Data Transfers</h2>
          <p>
            As our data is currently stored in the United States (us-east-1 region), international users should
            be aware that their data may be transferred to and processed in the United States. We implement
            appropriate safeguards to protect your data in accordance with applicable data protection laws.
          </p>
          <p>
            <strong>Future Multi-Region Support:</strong> We are planning to expand to additional regions
            (e.g., EU, Asia-Pacific) in future phases, allowing users to choose their preferred data region.
          </p>
        </section>

        <section>
          <h2>12. Changes to This Privacy Policy</h2>
          <p>
            We may update this Privacy Policy from time to time to reflect changes in our practices or legal requirements.
            We will notify you of any material changes by:
          </p>
          <ul>
            <li>Posting the new Privacy Policy on this page</li>
            <li>Updating the "Last Updated" date</li>
            <li>Sending an email notification for significant changes</li>
          </ul>
          <p>
            Your continued use of BoostedBudget after changes are posted constitutes acceptance of the updated policy.
          </p>
        </section>

        <section>
          <h2>13. Contact Us</h2>
          <p>If you have questions, concerns, or requests regarding this Privacy Policy or your personal data, please contact us:</p>
          <ul>
            <li><strong>Email:</strong> privacy@boostedbudget.com</li>
            <li><strong>Data Protection Inquiries:</strong> dpo@boostedbudget.com</li>
            <li><strong>Mail:</strong> BoostedBudget Privacy Team, [Address], United States</li>
          </ul>
          <p>
            We are committed to resolving privacy concerns promptly and transparently.
          </p>
        </section>

        <section>
          <h2>14. Subscription Tiers and Data Usage</h2>
          <p>Different subscription tiers have varying features and data processing needs:</p>
          <ul>
            <li><strong>Free Tier:</strong> Basic budgeting, 2 automation rules, ads enabled (with consent)</li>
            <li><strong>Premium Lite ($4.99/mo):</strong> Enhanced limits, no ads</li>
            <li><strong>Premium ($9.99/mo):</strong> Advanced features, priority support</li>
            <li><strong>Premium+ ($19.99/mo):</strong> Unlimited automation, AI insights (future), API access (future)</li>
          </ul>
          <p>
            Your subscription tier does not affect your privacy rights or our data protection commitments.
          </p>
        </section>

        <hr />

        <div className="privacy-summary">
          <h2>Summary</h2>
          <p>
            BoostedBudget is committed to protecting your privacy and providing transparency about how we
            handle your financial data. We provide GDPR-equivalent rights to all users, regardless of location.
            Your data is securely stored in the United States with plans for multi-region expansion. You have
            full control over your privacy settings, cookie preferences, and can request data export or deletion at any time.
          </p>
          <p className="contact-cta">
            <strong>Questions?</strong> Contact us at privacy@boostedbudget.com
          </p>
        </div>
      </div>
    </div>
  )
}
