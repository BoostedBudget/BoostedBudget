# Tech Stack: BoostedBudget

## 1. Core Architecture
- **Monorepo:** Managed by **Turbo** and **Bun**.
- **Frontend Hosting:** **Cloudflare Pages** for high-performance static asset delivery.
- **Backend Hosting:** **Fly.io** for global application distribution.

## 2. Frontend
- **Language:** **TypeScript**.
- **Framework:** **React** with **Vite** for fast development and build times.
- **API Client:** **Orval** for auto-generating type-safe API hooks from the backend OpenAPI spec.
- **Styling:** (Inferred: Vanilla CSS or Tailwind - confirmation optional)

## 3. Backend
- **Language:** **Go**.
- **Framework:** **Huma** with **Chi/Echo** router.
- **Database Driver:** **Pgx** for high-performance PostgreSQL interaction.
- **Job Queue:** **River** for reliable background job processing (built on Postgres).

## 4. Data & Infrastructure
- **Database:** **PostgreSQL** hosted on **NeonDB**.
- **Containerization:** **Docker** for consistent development and deployment environments.

## 5. Key Integrations
- **Authentication:** **Clerk**.
- **Banking Data:** **Plaid**.
- **Payments/Subscriptions:** **Paddle**.
