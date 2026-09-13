import { Link } from 'react-router'

export function PrivacyPolicy() {
  return (
    <div className="min-h-screen bg-muted py-8 px-4 sm:px-2">
      <div className="max-w-[900px] mx-auto bg-card px-12 py-12 rounded-lg shadow-[0_2px_8px_rgba(0,0,0,0.1)] sm:px-6 sm:py-8 xs:px-4 xs:py-6
        [&_h2]:text-foreground [&_h2]:text-[1.75rem] [&_h2]:mt-8 [&_h2]:mb-4 [&_h2]:border-l-4 [&_h2]:border-primary [&_h2]:pl-4
        [&_h3]:text-foreground [&_h3]:text-xl [&_h3]:mt-6 [&_h3]:mb-3
        [&_h4]:text-foreground [&_h4]:text-base [&_h4]:mt-4 [&_h4]:mb-2 [&_h4]:font-semibold
        [&_p]:leading-[1.7] [&_p]:text-foreground [&_p]:mb-4
        [&_ul]:list-disc [&_ul]:ml-8 [&_ul]:mb-4
        [&_li]:leading-[1.7] [&_li]:text-foreground [&_li]:mb-2
        [&_li_strong]:text-foreground
        [&_strong]:text-foreground [&_strong]:font-semibold
        [&_hr]:border-none [&_hr]:border-t-2 [&_hr]:border-border [&_hr]:my-12
        [&_section]:mb-10
        sm:[&_h2]:text-2xl sm:[&_h3]:text-[1.15rem] sm:[&_ul]:ml-6">
        <h1 className="text-foreground text-4xl mb-2 border-b-[3px] border-primary pb-4 sm:text-[2rem] xs:text-[1.75rem]">
          Privacy Policy
        </h1>
        <p className="text-muted-foreground italic mb-8 text-[0.95rem]">Last Updated: August 25, 2026</p>

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
            <li><strong>Account Information:</strong> Email address, name, Clerk user ID</li>
            <li><strong>Profile Data:</strong> User preferences, subscription tier, primary data region selection</li>
            <li><strong>Financial Data:</strong> Bank account information, transaction data, balances, investment holdings, and liability/loan details (via Plaid integration)</li>
            <li><strong>Authentication Tokens:</strong> Encrypted access tokens for third-party financial institutions</li>
            <li><strong>Usage Data:</strong> Application usage statistics, feature interactions, error logs</li>
          </ul>
          <p>
            Sensitive personal and financial fields stored in our application database are encrypted at the
            application level before storage. For technical details, see Section 4.2. Note: third-party
            processors (such as Plaid, Clerk, and Paddle) handle data under their own security practices
            as described in Section 5.
          </p>

          <h3>2.2 Automatically Collected Information</h3>
          <ul>
            <li><strong>Cookies:</strong> Necessary cookies (required) and analytics cookies (with your consent)</li>
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
            <li><strong>Financial Operations:</strong> Connect to financial institutions, sync transactions, and generate rule-based suggestions for your review</li>
            <li><strong>Communication:</strong> Send service updates, security alerts, marketing emails (with consent)</li>
            <li><strong>Improvement:</strong> Analyze usage patterns to improve our platform</li>
            <li><strong>Security:</strong> Detect fraud, prevent abuse, ensure platform security</li>
            <li><strong>Compliance:</strong> Meet legal obligations, respond to lawful requests</li>
          </ul>
          <p className="bg-warning/10 border-l-4 border-warning rounded px-4 py-4 my-6 [&_strong]:text-foreground">
            <strong>Important:</strong> BoostedBudget provides financial suggestions and automation only.
            We do not move money on your behalf. All automation rules create suggestions that you review and execute manually.
          </p>

          <h3>3.1 Legal Basis for Processing (GDPR)</h3>
          <p>Where GDPR applies, we rely on the following lawful bases:</p>
          <ul>
            <li><strong>Contract performance (Art. 6(1)(b)):</strong> Account management, service
              delivery, financial operations, subscription processing</li>
            <li><strong>Legitimate interests (Art. 6(1)(f)):</strong> Security and fraud prevention,
              service improvement, error tracking and performance monitoring</li>
            <li><strong>Consent (Art. 6(1)(a)):</strong> Analytics cookies (PostHog), marketing emails,
              household data sharing (see Section 9)</li>
            <li><strong>Legal obligation (Art. 6(1)(c)):</strong> Tax records, regulatory compliance,
              responding to lawful requests</li>
          </ul>
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

          <h4>Application-Level Encryption</h4>
          <p>
            Sensitive personal and financial fields stored in our application database are encrypted using{' '}
            <strong>AES-256-GCM</strong> (Advanced Encryption Standard with Galois/Counter Mode) before
            being written to disk. This provides defense-in-depth: even in the unlikely event of
            unauthorized database access, those fields remain unreadable without the encryption key. Each
            encryption operation uses a unique random value, so identical data produces different encrypted
            outputs — preventing pattern analysis. The encrypted categories are listed in "What We Encrypt"
            below; not every attribute in our system falls under this layer.
          </p>

          <h4>What We Encrypt</h4>
          <p>The following categories of data are protected by application-level encryption:</p>
          <ul>
            <li>Personal identifiers (email addresses, user IDs, customer IDs)</li>
            <li>Financial account information (account numbers, institution names, account masks)</li>
            <li>Physical addresses (property addresses on mortgages, servicer addresses on student loans)</li>
            <li>Transaction metadata and external identifiers</li>
            <li>Webhook payloads from financial and payment integrations</li>
            <li>Audit log details (change records, IP addresses)</li>
          </ul>

          <h4>Display Masking</h4>
          <p>
            When sensitive data is shown in the app, we apply display masking to limit exposure.
            Examples: email addresses appear as <code>u***@example.com</code>, account numbers as{' '}
            <code>****9012</code>, and street numbers in physical addresses are hidden.
          </p>

          <h4>Encrypted Lookups (Blind Indexes)</h4>
          <p>
            Because encrypted values cannot be searched directly, we use deterministic HMAC-SHA256 blind
            indexes stored in separate columns for fields that require equality lookups (such as email and
            account identifiers). This allows us to find your records without storing plaintext in queryable columns.
          </p>

          <h4>Encryption in Transit</h4>
          <p>
            Data in transit between your browser and our service is protected by HTTPS/TLS.
          </p>

          <h4>Key Management</h4>
          <p>
            Encryption keys are stored separately from encrypted data. The key used to derive lookup
            indexes is cryptographically derived from — but distinct from — the key used for field
            encryption, providing separation between the two operations.
          </p>

          <h4>Additional Controls</h4>
          <ul>
            <li><strong>Access Controls:</strong> Role-based access, principle of least privilege</li>
            <li><strong>Authentication:</strong> Clerk-powered authentication with industry-standard security</li>
            <li><strong>Audit Logging:</strong> Security-relevant administrative actions and certain data modifications are logged for monitoring</li>
            <li><strong>Regular Security Audits:</strong> Periodic security assessments and vulnerability scanning</li>
          </ul>
        </section>

        <section>
          <h2>5. Third-Party Services</h2>
          <p>We work with the following trusted third-party service providers:</p>
          <ul>
            <li><strong>Clerk:</strong> Authentication and user management</li>
            <li><strong>Plaid:</strong> Secure bank account connectivity and transaction data. When
              you connect a financial account, you direct us and Plaid to access account and
              transaction data from your financial institution. Data shared with Plaid may include
              account identifiers, balances, transactions, investment holdings, liabilities, and
              access credentials necessary to maintain the connection. Plaid processes this data
              under its own End User Privacy Policy. You can disconnect Plaid-linked accounts at
              any time in your account settings.</li>
            <li><strong>Paddle:</strong> Payment processing and subscription management</li>
            <li><strong>PostHog:</strong> Product analytics and usage insights (analytics cookies only, with your consent)</li>
            <li><strong>Cloud Infrastructure:</strong> AWS (data hosting), Fly.io (application hosting)</li>
            <li><strong>Sentry:</strong> Error tracking and performance monitoring. Sentry may receive
              error context, device and browser information, IP address, and application state at the
              time of an error.</li>
            <li><strong>SendGrid:</strong> Transactional email delivery</li>
          </ul>
          <p>
            These providers may have access to your information only to perform specific tasks on our behalf
            and are obligated to protect your information in accordance with their privacy policies.
          </p>
        </section>

        <section>
          <h2>6. Cookie Policy</h2>
          <p>We use two types of cookies, in compliance with GDPR requirements:</p>
          <ul>
            <li>
              <strong>Necessary Cookies (Required):</strong> Essential for app functionality, authentication,
              and security. Cannot be disabled.
            </li>
            <li>
              <strong>Analytics Cookies (Optional):</strong> Help us understand how users interact with our
              platform to improve features and user experience. We use PostHog for analytics. Requires your consent.
            </li>
          </ul>
          <p>
            You can manage your cookie preferences through our Cookie Consent banner or in your Privacy Settings.
            For full details on the cookies we use, see our <Link to="/legal/cookie-policy">Cookie Policy</Link>.
          </p>
          <p>
            PostHog provides pseudonymous product analytics only after the applicable consent choice
            resolves. No PostHog persistence or capture occurs before that point. Production session
            replay is disabled. Do Not Track is enforced in the browser only; it does not rewrite your
            server-authoritative consent setting, so browser analytics can undercount consenting DNT users.
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
            <li><strong>Right to Withdraw Consent:</strong> Withdraw consent for optional data processing, including analytics cookies and marketing communications</li>
          </ul>
          <p>
            To exercise any of these rights, please contact us at <strong>privacy@boostedbudget.com</strong>.
            We will respond to your request within 30 days.
          </p>
          <p>
            If you are in the EEA or UK, you also have the right to lodge a complaint with your
            local data protection supervisory authority.
          </p>

          <h3>7.1 Additional California Rights (CCPA)</h3>
          <p>If you are a California resident, you have the right to:</p>
          <ul>
            <li>Know what personal information we collect, use, and disclose</li>
            <li>Request deletion of your personal information</li>
            <li>Correct inaccurate personal information</li>
            <li>Not be discriminated against for exercising your privacy rights</li>
            <li>Limit the use of your sensitive personal information (including financial account data)
              to purposes necessary to provide the Service</li>
          </ul>
          <p>
            We do not sell or share personal information for cross-context behavioral advertising.
            To exercise your California privacy rights, contact us at privacy@boostedbudget.com.
          </p>
          <p>
            We use sensitive personal information (such as financial account numbers, balances, and
            transaction data) only as necessary to provide and improve the Service. We do not use or
            share sensitive personal information for purposes beyond what is necessary to perform the
            Service.
          </p>

          <h3>7.2 Do Not Track Signals</h3>
          <p>
            When a browser transmits a "Do Not Track" (DNT) signal, browser analytics remain off
            even if the saved account preference permits analytics. DNT does not rewrite the
            server-authoritative consent setting, so aggregate browser analytics may undercount
            consenting users who enable DNT.
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
            <li>
              Everything in your BoostedBudget account is permanently deleted, except where the
              law requires us to keep a record. Section 8.4 lists in full what this destroys
            </li>
            <li>Financial institution connections are revoked</li>
            <li>Subscription is cancelled (no refunds for partial periods)</li>
            <li>Your entries in our internal activity log are stripped, not erased — see below</li>
          </ul>
          <p>
            Your entries in our internal activity log are <strong>stripped of their contents rather
            than erased</strong>. We blank the details each entry recorded and unlink it from your
            user account. What we keep is what the action was, what kind of record it touched, an
            internal identifier for that record, the IP address and browser user agent the request
            came from, the request identifier, and when it happened. We keep these as our security
            and compliance record of what happened on the Service.
          </p>
          <p>
            Some data may also be retained in anonymized form for analytics or as required by law
            (e.g., financial records). Deleted data may persist in encrypted backups as described
            in Section 8.3.
          </p>

          <h3>8.2a Removing a Linked Bank Account (Not Account Deletion)</h3>
          <p>
            Disconnecting a bank connection or removing a single linked account is different from
            the account deletion described in Section 8.2 above, which deletes your entire
            BoostedBudget account. When you remove a linked bank account or disconnect a bank
            connection, that account's transaction, balance, and holdings history <strong>is
            retained</strong> — it is not erased — so you can still view it. Open the account
            from "Closed accounts" on your Accounts page to see its retained day-by-day balances
            and, for an investment account, its retained holdings; its transactions stay in your
            Transactions list, where you can filter by that account. The account is excluded from
            your active balances and reports going forward. If you want that history erased as
            well, you must delete your entire BoostedBudget account per Section 8.2.
          </p>

          <h3>8.3 Backup Retention</h3>
          <p>
            Deleted data may persist in encrypted backups for up to 90 days before being permanently purged.
          </p>

          <h3>8.4 Cancelled Accounts (No Deletion Request)</h3>
          <p>
            If you cancel your subscription but do not request account deletion, we retain your
            account data in read-only mode for at least <strong>6 months</strong> to allow
            reactivation — and, as explained below, longer than that today, because we are not yet
            automatically deleting cancelled accounts. Six months is the minimum we commit to, not a
            deadline. You may request deletion sooner at any time per Section 8.2.
          </p>
          <p>
            Before we automatically delete a cancelled account, we email you twice — 30 days
            beforehand, and again 7 days beforehand. Both notices are a precondition, not a
            courtesy: <strong>we do not automatically delete a cancelled account unless both of
            those notices have been sent.</strong>
          </p>
          <p>
            <strong>We are not sending those notices yet, so no cancelled account is being
            automatically deleted today.</strong> Cancelled accounts are retained past the 6-month
            window instead. We will begin automatic deletion only once the notices are in place, and
            you will receive both of them before yours is deleted. None of this applies to a
            deletion you request yourself under Section 8.2 — that is not conditioned on these
            notices, and it proceeds on the 30-day grace period described there.
          </p>
          <p>
            A deletion — whether it runs automatically at the end of this window, or because you
            requested one under Section 8.2 — permanently destroys everything in your BoostedBudget
            account, except where the law requires us to keep a record. In full, it deletes:
          </p>
          {/*
            ⚠ THESE THIRTEEN ITEMS ARE `retentionDeletionItems`, VERBATIM
            (backend/internal/queue/jobs/retention_cleanup_check.go). That list is the source of
            truth: it is the enumeration the 30-day and 7-day pre-deletion emails are rendered
            from, and it is the one covered by a guard that checks every table DeleteUserCascade
            destroys is honestly named by some phrase
            (TestRetentionNoticeEnumerationCoversEverythingDeleted). Nothing checks these list
            items against it — the Go/TS boundary has no mechanical link — so the pairing is a
            convention, held by this comment and by PrivacyPolicy.test.tsx's
            RETENTION_DELETION_ITEMS, which quotes the same strings.

            #1592 is the defect that convention exists to prevent: this clause named THREE
            categories where the email named thirteen, describing the same deletion. If you
            reword an item, reword it in the Go source and copy it back here in the same change.
            Do not paraphrase — a paraphrase is how the two drifted apart the first time.
          */}
          <ul>
            <li>your bank connections and every account on them, including accounts you have already closed or disconnected, the record of when each one was connected, disconnected or closed, and any reconnection you had started</li>
            <li>your day-by-day balance history for every account</li>
            <li>your transactions, together with every category, note, tag and merchant name you set on them, and the record of those edits</li>
            <li>your investment holdings and their day-by-day snapshots, every buy and sell recorded against them, and your portfolio value history</li>
            <li>your credit card, mortgage and student loan records, including their statements and APR schedules</li>
            <li>your budgets, their categories, their period history and every rollover</li>
            <li>your savings goals and every contribution you logged against them by hand</li>
            <li>your boosts and the record of every boost that ran</li>
            <li>your automation rules, the record of every rule that ran, and your transfer suggestions</li>
            <li>your membership of a household, its invitations, its sharing consents, and the accounts shared into it (and the household itself, if you are its only member)</li>
            <li>your subscription, billing and payment records, and your referrals</li>
            <li>your profile and sign-in identities, notifications, dashboard layout, privacy settings and saved automation recipes</li>
            <li>the internal job and activity records tied to your account — deleted, or stripped of their contents and disconnected from you where we are required to keep a record</li>
          </ul>
          <p>
            This cannot be undone, and there is no way to restore it afterwards. The carve-outs
            elsewhere in this section apply here too: the activity-log entries described in
            Section 8.2, data retained in anonymized form, records we are required by law to keep,
            and encrypted backups for up to 90 days (Section 8.3).
          </p>

          <h3>8.5 Removed Household Members</h3>
          <p>
            When a household member is removed from a shared plan, their data is preserved for
            30 days before permanent deletion (see our{' '}
            <Link to="/legal/terms">Terms of Service</Link>, Section 9.2). Other household members'
            access to that member's shared data ends immediately upon removal, and the data-sharing
            grants themselves are purged along with the rest of the removed member's data at the end
            of the 30-day window. The backup retention period in Section 8.3 applies after permanent
            deletion.
          </p>
        </section>

        <section>
          <h2>9. Data Sharing and Disclosure</h2>
          <p>We do not sell your personal information. We may share your data in the following circumstances:</p>
          <ul>
            <li><strong>With Your Consent:</strong> When you explicitly authorize data sharing</li>
            <li><strong>Service Providers:</strong> Third-party partners performing services on our behalf (as listed in Section 5)</li>
            <li><strong>Legal Compliance:</strong> When required by law, court order, or government request</li>
            <li><strong>Business Transfers:</strong> In the event of a merger, acquisition, or sale of assets</li>
            <li><strong>Fraud Prevention:</strong> To protect against fraud, security threats, or illegal activity</li>
            <li><strong>Household Members:</strong> For accounts you choose to share, other members of
              your household (see below)</li>
          </ul>
          <p>
            You may control optional data processing (analytics and marketing) in your Privacy
            Settings. Essential service-provider processing required to operate the Service cannot
            be disabled while your account is active.
          </p>

          <h3>9.1 Household Data Sharing</h3>
          <p>
            If you belong to a household plan, you can choose which of your linked accounts to share
            with the rest of your household. Sharing is <strong>per-account</strong> and, for any
            account you choose to share, other household members can see:
          </p>
          <ul>
            <li>Account names</li>
            <li>Account balances</li>
            <li>Net worth contribution</li>
            <li>Spending activity, including merchant names</li>
            <li>Income sources and savings-rate figures</li>
          </ul>
          <p>Household members can <strong>never</strong> see:</p>
          <ul>
            <li>Individual transaction line items</li>
            <li>Full account numbers or masks</li>
            <li>Login credentials</li>
          </ul>
          <p>
            Before joining a household, you will be shown your currently linked accounts and asked to
            choose which to share; all accounts are shared by default and you may opt out of sharing
            any individual account before joining. Your explicit consent is recorded, along with a
            version identifier and timestamp, for compliance purposes.
          </p>
          <p>
            Accounts you link to BoostedBudget after joining a household are likewise offered for
            sharing and default to shared, and you choose whether to share each one before it enters
            the household view.
          </p>
          <p>
            You may hide any of your linked accounts from the household view at any time from your
            Account Settings. Once you save this change, the account is removed from other members'
            view. See
            our <Link to="/legal/terms">Terms of Service</Link>, Section 9, for full details on
            household plans and data visibility between members.
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
            be aware that their data may be transferred to and processed in the United States. Where required,
            we rely on the European Commission's Standard Contractual Clauses (SCCs) and equivalent UK transfer
            mechanisms. You may request a copy of the relevant transfer safeguards by contacting
            privacy@boostedbudget.com.
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
            Where required by law, we will obtain your affirmative consent before processing your data under
            materially changed purposes.
          </p>

          <h3>12.1 Data Breach Notification</h3>
          <p>
            In the event of a personal data breach that is likely to result in a risk to your rights,
            we will notify affected users without unreasonable delay, and in any case within the
            timeframe required by applicable law. We will also notify relevant supervisory authorities
            where required by GDPR or other applicable data protection laws.
          </p>
        </section>

        <section>
          <h2>13. Contact Us</h2>
          <p>If you have questions, concerns, or requests regarding this Privacy Policy or your personal data, please contact us:</p>
          <ul>
            <li><strong>Email:</strong> privacy@boostedbudget.com</li>
            <li><strong>Data Protection Inquiries:</strong> dpo@boostedbudget.com</li>
          </ul>
          <p>
            We are committed to resolving privacy concerns promptly and transparently.
          </p>
        </section>

        <section>
          <h2>14. Subscription Plans and Data Usage</h2>
          <p>
            BoostedBudget offers a <strong>60-day free trial</strong> (no credit card required, no ads)
            followed by paid subscription plans. For current plan details and pricing, visit our{' '}
            <Link to="/pricing">Pricing page</Link>.
          </p>
          <p>
            After trial expiry or subscription cancellation, accounts enter read-only mode — data is
            preserved and accessible but no new syncing or premium features are available. See our{' '}
            <Link to="/legal/terms">Terms of Service</Link> for full details.
          </p>
          <p>
            Your subscription plan does not affect your privacy rights or our data protection commitments.
          </p>
        </section>

        <hr />

        <div className="bg-info/10 border-2 border-info rounded-lg p-8 mt-8
          [&_h2]:border-none [&_h2]:mt-0 [&_h2]:text-primary [&_h2]:pl-0
          [&_p]:mb-4">
          <h2>Summary</h2>
          <p>
            BoostedBudget is committed to protecting your privacy and providing transparency about how we
            handle your financial data. We provide GDPR-equivalent rights to all users, regardless of location.
            Your data is securely stored in the United States with plans for multi-region expansion. You have
            full control over your privacy settings, cookie preferences, and can request data export or deletion at any time.
          </p>
          <p className="text-[1.1rem] text-center mt-6 pt-4 border-t border-info/30 [&_strong]:text-primary">
            <strong>Questions?</strong> Contact us at privacy@boostedbudget.com
          </p>
        </div>
      </div>
    </div>
  )
}
