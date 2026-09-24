/**
 * Single source of marketing prose (Issue #1666, Todo 14).
 *
 * The landing FAQ (`FAQSection.tsx`), the full `/faq` page (`FaqPage.tsx`), the
 * landing feature grid (`FeaturesSection.tsx`), and the generated agent-discovery
 * file `apps/web/public/llms-full.txt` all read their prose from HERE. Before this
 * module the same sentences were duplicated in three React files and had already
 * drifted once; one source means one place to edit and one place to drift-check.
 *
 * ⚠ This module holds PROSE ONLY. The lucide icon COMPONENTS stay in
 * `FeaturesSection.tsx` — a feature carries a stable `key`, and the component maps
 * that key to its icon. Keeping the icons out means
 * `scripts/generate-llms-full.ts` can import this file without pulling
 * `lucide-react` (and an entire icon set) into a Node-side generator.
 *
 * Location is deliberate: `features/landing/data/` matches NO `codeSplitting.groups`
 * entry, so these bytes stay in the entry chunk where the prose already lived.
 * Extraction RELOCATES them; it does not add them. Do not move this under `shared/`.
 */

/** A question/answer pair rendered as a disclosure, and printed to llms-full.txt. */
export interface FaqItem {
  question: string
  answer: string
  /** Optional inline link appended to the answer in the UI. The JSON-LD text — and llms-full.txt — stay the plain answer. */
  link?: { label: string; to: string }
}

/**
 * Stable keys for the six feature cards. The ICONS for these keys live in
 * `FeaturesSection.tsx`, never here (see the module doc).
 */
export type FeatureIconKey =
  | 'accounts'
  | 'dashboard'
  | 'categorization'
  | 'automation'
  | 'household'
  | 'agent'

export interface MarketingFeature {
  key: FeatureIconKey
  title: string
  description: string
}

/**
 * The ten public FAQ entries: the seven landing Q&As plus the three agentic Q&As
 * introduced by Todo 8. Consumed by `FAQSection` (landing), `FaqPage` (/faq), and
 * the llms-full.txt generator.
 */
export const MARKETING_FAQ: FaqItem[] = [
  {
    question: 'Is my bank data secure?',
    answer:
      'Yes, absolutely. We use Plaid, the same trusted service used by Venmo, Robinhood, and thousands of other apps, to securely connect to your bank. We never see or store your bank login credentials. All data is encrypted with 256-bit encryption, the same standard used by banks.',
  },
  {
    question: 'How does BoostedBudget connect to my bank?',
    answer:
      'We use Plaid to establish secure, read-only connections to over 12,000 financial institutions. When you link an account, you authenticate directly with your bank through Plaid - we never see your username or password.',
  },
  {
    question: 'Can I cancel anytime?',
    answer:
      'Yes, you can cancel your subscription at any time with no penalty. Your data remains accessible until the end of your billing period, and you can export all your data before canceling.',
  },
  {
    question: 'What happens to my data if I cancel?',
    answer:
      'Cancelling and deleting are two different things. When you cancel, your account switches to read-only: everything you have already recorded stays there, you can still view and export it, and we keep it that way for at least 6 months so you can pick up where you left off by resubscribing. We are not automatically deleting cancelled accounts today, so in practice your data is kept past that 6-month window rather than deleted at the end of it - see Section 8.4 of our Privacy Policy for exactly what an automatic deletion would destroy, and when we will start doing it. If you want your data gone, you can request deletion at any time: we hold it through a 30-day grace period in case you change your mind, then permanently delete it.',
  },
  {
    question: 'Is there a free trial?',
    answer:
      'Yes! All new accounts start with a 60-day free trial of our Premium features. No credit card is required to start. You can upgrade, downgrade, or cancel at any time during or after your trial.',
  },
  {
    question: 'Do you sell my financial data?',
    answer:
      'Never. Your financial data is yours and yours alone. We do not sell, share, or monetize your personal financial information. Our business model is simple: you pay for the service, and that is how we make money.',
  },
  {
    question: 'Can my family share one subscription?',
    answer:
      'Yes. Household plans cover 2 to 5+ members on one subscription. Each member chooses which accounts to share, and can hide any account at any time. Accepting a household invite is always a deliberate action you take in the app.',
  },
  {
    question: 'Can my AI agent help me manage my budget?',
    answer:
      'Yes. BoostedBudget runs a remote MCP server that lets an AI agent read across your accounts, transactions, budgets, and portfolio in plain language. It can recategorize, tag, and save repeatable reviews — but every proposed change waits for your explicit confirmation before it is applied, and each confirmation is single-use.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
  {
    question: 'Can an AI agent move my money?',
    answer:
      'No. BoostedBudget never moves money, with or without an agent. A connected agent can read your data and propose changes for you to review and confirm, but it has no ability to transfer funds, make payments, or act on your behalf without that confirmation step you control.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
  {
    question: 'Is it safe to let an AI agent see my finances?',
    answer:
      'Yes — and you stay in control. Your BoostedBudget credentials are injected server-side after you sign in, so they are never visible to the agent, and calls are recorded in an audit log on a best-effort basis. You can revoke the agent’s access at any time from your sign-in provider’s security dashboard; it can take about 30–60 seconds to take effect.',
    link: { label: 'See how the MCP server works', to: '/mcp' },
  },
]

/** The six feature cards' prose. Icons are attached by `FeaturesSection.tsx` via `key`. */
export const MARKETING_FEATURES: MarketingFeature[] = [
  {
    key: 'accounts',
    title: 'All Your Accounts in One Place',
    description:
      'Connect all your bank accounts, credit cards, and investments. Get a complete picture of your finances at a glance.',
  },
  {
    key: 'dashboard',
    title: 'Customizable Dashboard',
    description:
      'Personalize your financial overview with a diverse library of widgets. Visualize and analyze your data the way you want.',
  },
  {
    key: 'categorization',
    title: 'Categorization That Follows Your Rules',
    description:
      "Every transaction arrives with a category from Plaid. Write a rule for merchant, amount, or description, and it takes over for every match — unless you've already categorized or reviewed that transaction yourself.",
  },
  {
    key: 'automation',
    title: 'Automation You Can Audit',
    description:
      'Build rules that flag budget risks and suggest transfers. Every rule is one you wrote, so you always know why it fired.',
  },
  {
    key: 'household',
    title: 'Shared Household Budgeting',
    description:
      'One plan across 2 to 5+ members. Each person chooses which accounts to share, and can hide any account from the household at any time.',
  },
  {
    key: 'agent',
    title: 'Your Agent Can Do the Digging',
    description:
      'Connect BoostedBudget to Claude, Cursor, or VS Code through our MCP server. Your agent can read across your accounts, transactions, budgets, and portfolio — and every change it proposes waits for your confirmation.',
  },
]