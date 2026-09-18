/**
 * CookiePolicy Page
 *
 * Standalone Cookie Policy page at /legal/cookie-policy.
 * Fixes the dead link from the CookieConsent banner.
 *
 * Issue #858: Legal pages for Paddle domain verification
 * Route: /legal/cookie-policy (see App.tsx)
 */

import { Link } from 'react-router'

export function CookiePolicy() {
  return (
    <div className="privacy-policy">
      <div className="privacy-policy-container">
        <h1>Cookie Policy</h1>
        <p className="last-updated">Last Updated: September 13, 2026</p>

        <section>
          <h2>1. What Are Cookies?</h2>
          <p>
            Cookies are small text files placed on your device by websites you visit. They are
            widely used to make websites work more efficiently, remember your preferences, and
            provide information to website owners.
          </p>
          <p>
            BoostedBudget uses cookies and similar tracking technologies (such as local storage)
            to keep you logged in, protect your account, and optionally improve our platform.
          </p>
        </section>

        <section>
          <h2>2. Types of Cookies We Use</h2>

          <h3>2.1 Necessary Cookies (Always Active)</h3>
          <p>
            These cookies are essential for the Service to function. They cannot be disabled
            because the Service would not work properly without them. They do not store any
            personally identifiable information beyond what is required for functionality.
          </p>
          <p>Necessary cookies are used for:</p>
          <ul>
            <li><strong>Authentication:</strong> Keeping you logged in securely via Clerk</li>
            <li><strong>Session management:</strong> Maintaining your session state</li>
            <li><strong>Security:</strong> CSRF protection and fraud prevention</li>
            <li><strong>Preferences:</strong> Remembering your cookie consent settings and UI preferences (e.g., dark mode)</li>
          </ul>

          <h3>2.2 Analytics Cookies (Optional — Requires Consent)</h3>
          <p>
            We use <strong>PostHog</strong> to collect pseudonymous analytics data about how users
            interact with BoostedBudget. This helps us identify usability issues, understand which
            features are most valuable, and improve the platform.
          </p>
          <p>
            No PostHog analytics persistence or analytics event capture occurs before the applicable
            consent choice resolves.
            Signed-out visitors choose through our consent banner; signed-in users choose in
            Profile → Privacy Settings. Signed-out visitors can reopen Cookie Settings and explicitly
            withdraw; signed-in users can withdraw in Privacy Settings.
          </p>
          <p>PostHog analytics cookies collect:</p>
          <ul>
            <li>Sanitized route templates and features used (pseudonymous)</li>
            <li>General device and browser information</li>
            <li>Session duration and navigation patterns</li>
            <li>Session recording and heatmaps, only if you separately grant interaction-capture
              consent; recorded text and element attributes are masked</li>
          </ul>
          <p>
            After consent, locally persisted PostHog SDK state is limited to the current tab session.
            The anonymous consent cookie remains the cross-session authority. Signed-in tabs identify
            as the same account after server consent resolves. Duplicating a tab may copy sessionStorage,
            so BoostedBudget clears inherited SDK identity before enabling persistence for that tab.
          </p>
          <p>
            Do Not Track is a browser-only hard stop: when your browser sends DNT, browser analytics
            stay off even if your saved account preference permits them. Your server consent record
            remains authoritative for the account, so aggregate browser analytics may undercount DNT users.
          </p>
          <p className="important-note">
            <strong>Privacy note:</strong> PostHog analytics are configured to respect GDPR requirements.
            Analytics properties and URLs are sanitized before capture. Session replay and heatmaps are
            separately consent-gated and merge disabled; when you grant interaction-capture consent,
            recorded text and element attributes are masked, and financial amounts, account numbers, and
            other sensitive displayed data are protected. We do not sell analytics data to third parties.
          </p>
          <p>
            Authenticated Support chat is a separate, user-requested service and is not an analytics
            cookie. It loads only inside the signed-in application after our server verifies your
            account identity. PostHog Surveys load only for signed-in users who have granted analytics
            consent; BoostedBudget permits rating and closed-choice questions and does not display
            free-text surveys.
          </p>
        </section>

        <section>
          <h2>3. Managing Your Cookie Preferences</h2>
          <p>
            You have control over optional cookies through the controls available for your session:
          </p>
          <ul>
            <li>
              <strong>Cookie Consent Banner:</strong> When you first visit BoostedBudget, a consent
              banner allows you to accept or decline analytics cookies.
            </li>
            <li>
              <strong>Privacy Settings:</strong> Once logged in, you can update your cookie preferences
              anytime in <strong>Profile → Privacy Settings</strong>.
            </li>
            <li>
              <strong>Browser Settings:</strong> You can also control cookies through your browser
              settings. Use BoostedBudget&apos;s Cookie Settings control for an immediate signed-out
              analytics withdrawal broadcast; blocking necessary cookies will prevent the Service
              from working.
            </li>
          </ul>
          <p>
            Declining analytics cookies has no effect on your access to any features of BoostedBudget.
          </p>
        </section>

        <section>
          <h2>4. Third-Party Cookies</h2>
          <p>
            Some cookies are set by third-party services we use. We only allow third-party cookies
            from the following trusted providers:
          </p>
          <ul>
            <li>
              <strong>Clerk</strong> — authentication cookies required for login functionality.
              See <a href="https://clerk.com/privacy" target="_blank" rel="noopener noreferrer">Clerk's Privacy Policy</a>.
            </li>
            <li>
              <strong>PostHog</strong> (analytics only, with consent) — usage analytics.
              See <a href="https://posthog.com/privacy" target="_blank" rel="noopener noreferrer">PostHog's Privacy Policy</a>.
            </li>
            <li>
              <strong>Sentry</strong> — error tracking. Sentry may use local storage to capture
              error context and session information for debugging purposes.
              See <a href="https://sentry.io/privacy/" target="_blank" rel="noopener noreferrer">Sentry's Privacy Policy</a>.
            </li>
          </ul>
          <p>
            We do not permit advertising cookies or third-party tracking for advertising purposes.
          </p>
        </section>

        <section>
          <h2>5. Cookie Retention</h2>
          <p>Cookies are retained for the following periods:</p>
          <ul>
            <li><strong>Session cookies:</strong> Deleted when you close your browser</li>
            <li><strong>Authentication cookies:</strong> Up to 30 days (or until you log out)</li>
            <li><strong>Cookie consent preferences:</strong> 365 days</li>
            <li>
              <strong>PostHog analytics state:</strong> No longer than the current tab session;
              cleared sooner on reload or an authority change
            </li>
          </ul>
        </section>

        <section>
          <h2>6. Changes to This Cookie Policy</h2>
          <p>
            We may update this Cookie Policy when we change the cookies we use or to reflect
            changes in applicable law. We will update the "Last Updated" date when we make changes.
            Material changes will be notified via the cookie consent banner.
          </p>
        </section>

        <section>
          <h2>7. Contact Us</h2>
          <p>
            If you have questions about our use of cookies or this Cookie Policy, please contact:
          </p>
          <ul>
            <li><strong>Privacy Team:</strong> privacy@boostedbudget.com</li>
          </ul>
        </section>

        <hr />

        <div className="privacy-summary">
          <h2>Quick Summary</h2>
          <ul>
            <li><strong>Necessary cookies:</strong> Always on — required for login and security</li>
            <li><strong>Analytics cookies (PostHog):</strong> Optional — requires your consent</li>
            <li><strong>Advertising cookies:</strong> None — we don't use them</li>
            <li><strong>Change preferences:</strong> Profile → Privacy Settings, anytime</li>
          </ul>
          <p className="contact-cta">
            For full details on how we handle your personal data, see our{' '}
            <Link to="/legal/privacy-policy">Privacy Policy</Link>. For the complete terms
            governing your use of BoostedBudget, see our{' '}
            <Link to="/legal/terms">Terms of Service</Link>.
          </p>
        </div>
      </div>
    </div>
  )
}
