<!-- DEVELOPER SETUP INSTRUCTIONS DIRECTIVE -->
<!--
IMPORTANT: When working with this project plan, Claude Code should automatically generate detailed, step-by-step instructions for ANY task that requires manual developer action. This includes but is not limited to:

1. External service setup (domains, DNS, Auth0, Stripe, Plaid, etc.)
2. Account creation and configuration
3. CLI tool installation and authentication
4. Environment variable configuration
5. Secret/API key generation and storage
6. Database creation and configuration
7. Payment and billing setup
8. Compliance and legal requirements
9. Third-party integrations
10. Production deployment prerequisites

For EACH manual setup task, provide:
- Clear numbered steps in order
- Exact commands to run (with explanations)
- Screenshots or UI navigation paths where applicable
- Cost implications if any
- Time estimates for completion
- Validation steps to confirm successful setup
- Troubleshooting tips for common issues
- Links to official documentation
- Security best practices
- What values to save for later steps

Format instructions as:
### 📋 MANUAL SETUP REQUIRED: [Task Name]
**Time Required:** X minutes
**Cost:** $X/month (if applicable)
**Prerequisites:** List any required prior steps

#### Steps:
1. **Step name**: Detailed instruction
```bash
   # Include any commands
```
   - Additional notes or warnings
   - Expected output or result

2. **Next step**: Continue...

#### Verification:
- How to verify this was completed successfully

#### Save These Values:
- List any IDs, keys, or values to record

GENERATE THESE INSTRUCTIONS IMMEDIATELY when encountering any setup task in the plan.
-->

# BoostedBudget - Development Plan & Project Tracker

## Project Overview

**Application Name:** BoostedBudget  
**Domain:** boostedbudget.com  
**Tagline:** "Supercharge your finances with intelligent automation"  
**Vision:** A global budgeting platform that helps users optimize their finances through customizable automation rules  
**Initial Market:** USA (with global accessibility and GDPR compliance from day one)  
**Expansion:** EU and other markets  

### Business Model
- **Free Tier:** 2-3 automation rules, basic budgeting, Google AdSense supported
- **Premium ($9.99/month):** Unlimited rules, advanced automation, priority execution, no ads
- **Premium Plus ($19.99/month):** Everything + AI insights, financial advisor tools, API access

### Compliance & Legal
- **GDPR Compliant:** From day one (global deployment)
- **CCPA Compliant:** California Consumer Privacy Act
- **Data Residency:** Multi-region with user choice
- **Transfer Suggestions:** Initially suggest transfers only (no money movement)
- **Future:** ACH transfers via Plaid Transfer API (requires MTL licenses)

---

## Technical Architecture

### Core Technology Stack
```yaml
Authentication:
  Provider: Auth0
  Implementation: Universal Login / Custom Forms
  Token: JWT with Auth0 validation
  
Backend:
  Language: Go 1.21+
  Framework: Echo/Gin with Auth0 middleware
  API Spec: OpenAPI 3.1
  Database: PostgreSQL 15 + Redis 7
  Message Queue: RabbitMQ (rule processing)
  
Frontend:
  Framework: React 18 + TypeScript 5
  Auth: Auth0 React SDK
  Build Tool: Vite
  State: Redux Toolkit + RTK Query
  UI: Tailwind CSS + Radix UI
  API Client: Auto-generated from OpenAPI
  
Infrastructure:
  Deployment: Global from day one
  Containers: Docker + Docker Compose
  Orchestration: Kubernetes (multi-region)
  CDN: CloudFlare (global)
  Cloud: AWS / GCP (multi-region)
  CI/CD: GitHub Actions
  Monitoring: OpenTelemetry → Grafana/Prometheus
  
Financial:
  Banking API: Plaid (Transactions, Accounts, Balance)
  Future Transfers: Plaid Transfer API
  Payments: Stripe
  
Advertising:
  Platform: Google AdSense
  Placement: Dashboard, between transaction lists
  
Development:
  API Mocking: Prism
  Testing: Jest, Testing Library, Testify
  Code Gen: oapi-codegen, openapi-typescript-codegen
```

### Architecture Patterns
- **Backend:** Clean Architecture with Hexagonal ports & adapters
- **Frontend:** Feature-based organization with barrel exports
- **API:** RESTful with consistent resource naming
- **Database:** Repository pattern with migrations
- **Authentication:** Auth0 token validation on every request
- **Data Privacy:** GDPR-compliant with encryption at rest

---

## Global Deployment & Compliance Strategy

### Multi-Region Architecture
```yaml
Regions:
  Primary: us-east-1 (Virginia)
  Secondary: eu-west-1 (Ireland)
  CDN: CloudFlare (200+ edge locations)
  
Data Residency:
  USA Users: Data stored in us-east-1
  EU Users: Data stored in eu-west-1
  User Choice: Allow users to select data region
  
Compliance:
  GDPR:
    - Right to deletion
    - Data portability
    - Privacy by design
    - Data minimization
    - 30-day deletion policy
  CCPA:
    - Opt-out of data sale
    - Access rights
    - Deletion rights
```

### Auth0 Configuration
```javascript
// Auth0 Tenant Configuration
{
  "tenant": "boostedbudget",
  "region": "US", // Primary tenant
  "custom_domain": "auth.boostedbudget.com",
  "universal_login": {
    "colors": {
      "primary": "#3B82F6"
    },
    "logo": "https://boostedbudget.com/logo.png"
  },
  "connections": [
    "Username-Password-Authentication",
    "google-oauth2",
    "apple"
  ],
  "rules": [
    "gdpr-consent-tracking",
    "geo-location-routing",
    "progressive-profiling"
  ]
}
```

---

## Phase 0: Project Bootstrap & Auth0 Setup (Week 1)
*Deliverable: Fully configured global development environment with Auth0*

### 0.1 Repository & Initial Setup
- [ ] Register boostedbudget.com domain
- [ ] Set up DNS with CloudFlare
- [ ] Create Git repository with branch protection
- [ ] Initialize project structure:
```bash
boostedbudget/
├── .github/
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── deploy-dev.yml
│   │   ├── deploy-staging.yml
│   │   └── deploy-prod.yml
│   └── CODEOWNERS
├── api/
│   ├── openapi.yaml
│   ├── schemas/
│   └── paths/
├── backend/
│   ├── cmd/
│   │   └── server/
│   ├── internal/
│   │   ├── auth/           # Auth0 integration
│   │   ├── generated/      # OpenAPI generated
│   │   ├── handlers/
│   │   ├── services/
│   │   ├── repository/
│   │   ├── domain/
│   │   ├── middleware/
│   │   └── privacy/        # GDPR compliance
│   ├── migrations/
│   ├── tests/
│   ├── Dockerfile
│   └── Dockerfile.test
├── frontend/
│   ├── src/
│   │   ├── api/
│   │   ├── auth/           # Auth0 React SDK
│   │   ├── features/
│   │   ├── shared/
│   │   ├── privacy/        # Cookie consent, GDPR
│   │   └── test-utils/
│   ├── public/
│   ├── Dockerfile
│   └── Dockerfile.test
├── infrastructure/
│   ├── docker/
│   ├── kubernetes/
│   │   ├── base/
│   │   ├── us-east-1/
│   │   └── eu-west-1/
│   ├── terraform/
│   └── auth0/
├── scripts/
├── legal/
│   ├── privacy-policy.md
│   ├── terms-of-service.md
│   └── gdpr-compliance.md
└── Makefile
```

### 0.2 Auth0 Setup
- [ ] Create Auth0 account and tenant
- [ ] Configure Auth0 applications:
```javascript
// Backend API Application (Machine-to-Machine)
{
  "name": "BoostedBudget API",
  "type": "non_interactive",
  "grant_types": ["client_credentials"],
  "jwt_configuration": {
    "alg": "RS256",
    "lifetime_in_seconds": 86400
  }
}

// Frontend Application (SPA)
{
  "name": "BoostedBudget Web",
  "type": "spa",
  "callbacks": [
    "http://localhost:3000/callback",
    "https://boostedbudget.com/callback"
  ],
  "allowed_logout_urls": [
    "http://localhost:3000",
    "https://boostedbudget.com"
  ],
  "allowed_origins": [
    "http://localhost:3000",
    "https://boostedbudget.com"
  ],
  "jwt_configuration": {
    "alg": "RS256",
    "lifetime_in_seconds": 3600
  }
}
```

- [ ] Set up Auth0 Rules:
```javascript
// rules/add-user-metadata.js
function addUserMetadata(user, context, callback) {
  const namespace = 'https://boostedbudget.com/';
  
  // Add user metadata to tokens
  context.idToken[namespace + 'user_id'] = user.user_id;
  context.idToken[namespace + 'email'] = user.email;
  context.idToken[namespace + 'created_at'] = user.created_at;
  
  // Track GDPR consent
  if (context.request.body && context.request.body.gdpr_consent) {
    user.user_metadata = user.user_metadata || {};
    user.user_metadata.gdpr_consent = true;
    user.user_metadata.gdpr_consent_date = new Date().toISOString();
    
    auth0.users.updateUserMetadata(user.user_id, user.user_metadata)
      .then(() => callback(null, user, context))
      .catch((err) => callback(err));
  } else {
    callback(null, user, context);
  }
}

// rules/geo-location.js
function geoLocation(user, context, callback) {
  // Determine user's region for data residency
  const eu_countries = ['DE', 'FR', 'IT', 'ES', 'NL', /* ... */];
  
  user.app_metadata = user.app_metadata || {};
  
  if (eu_countries.includes(context.request.geoip.country_code)) {
    user.app_metadata.data_region = 'eu-west-1';
  } else {
    user.app_metadata.data_region = 'us-east-1';
  }
  
  auth0.users.updateAppMetadata(user.user_id, user.app_metadata)
    .then(() => callback(null, user, context))
    .catch((err) => callback(err));
}
```

### 0.3 OpenAPI Foundation with Auth0
- [ ] Create initial OpenAPI specification:
```yaml
# api/openapi.yaml
openapi: 3.1.0
info:
  title: BoostedBudget API
  version: 0.1.0
  description: Global budgeting platform with intelligent automation
  contact:
    email: api@boostedbudget.com
  license:
    name: Proprietary
    
servers:
  - url: http://localhost:8080/api/v1
    description: Local development
  - url: https://api.boostedbudget.com/v1
    description: Production (Global)
  - url: https://eu-api.boostedbudget.com/v1
    description: Production (EU)
    
security:
  - auth0: []

paths:
  /health:
    get:
      operationId: getHealth
      security: []  # Public endpoint
      tags: [System]
      summary: Health check endpoint
      responses:
        '200':
          description: Service is healthy
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/HealthResponse'
                
  /user/profile:
    get:
      operationId: getUserProfile
      tags: [User]
      summary: Get user profile
      responses:
        '200':
          description: User profile
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/UserProfile'
                
  /user/privacy:
    get:
      operationId: getPrivacySettings
      tags: [User]
      summary: Get user privacy settings (GDPR)
      responses:
        '200':
          description: Privacy settings
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/PrivacySettings'
    
    put:
      operationId: updatePrivacySettings
      tags: [User]
      summary: Update privacy settings
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/PrivacySettings'
              
  /user/data-export:
    post:
      operationId: requestDataExport
      tags: [User]
      summary: Request GDPR data export
      responses:
        '202':
          description: Export requested
          
  /user/delete:
    delete:
      operationId: deleteUserAccount
      tags: [User]
      summary: Delete user account (GDPR right to deletion)
      responses:
        '204':
          description: Account deletion scheduled

components:
  securitySchemes:
    auth0:
      type: oauth2
      flows:
        implicit:
          authorizationUrl: https://boostedbudget.auth0.com/authorize
          scopes:
            openid: OpenID
            profile: User profile
            email: User email
            
  schemas:
    UserProfile:
      type: object
      required: [id, email, created_at]
      properties:
        id:
          type: string
        email:
          type: string
          format: email
        first_name:
          type: string
        last_name:
          type: string
        data_region:
          type: string
          enum: [us-east-1, eu-west-1]
        subscription:
          $ref: '#/components/schemas/Subscription'
        created_at:
          type: string
          format: date-time
          
    PrivacySettings:
      type: object
      properties:
        data_region:
          type: string
          enum: [us-east-1, eu-west-1]
        analytics_consent:
          type: boolean
        marketing_consent:
          type: boolean
        data_sharing_consent:
          type: boolean
        ad_personalization_consent:
          type: boolean
```

### 0.4 Backend Auth0 Integration
- [ ] Implement Auth0 middleware:
```go
// backend/internal/middleware/auth0.go
package middleware

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    
    "github.com/auth0/go-jwt-middleware/v2"
    "github.com/auth0/go-jwt-middleware/v2/validator"
    "github.com/labstack/echo/v4"
)

type Auth0Config struct {
    Domain   string
    Audience string
    Issuer   string
}

func NewAuth0Middleware(config Auth0Config) echo.MiddlewareFunc {
    issuerURL := "https://" + config.Domain + "/"
    
    jwtValidator, err := validator.New(
        validator.WithIssuerURL(issuerURL),
        validator.WithAudience(config.Audience),
        validator.WithCustomClaims(func() validator.CustomClaims {
            return &CustomClaims{}
        }),
    )
    
    if err != nil {
        panic("Failed to create JWT validator: " + err.Error())
    }
    
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            token, err := extractToken(c.Request())
            if err != nil {
                return echo.NewHTTPError(http.StatusUnauthorized, "Missing or invalid token")
            }
            
            validatedToken, err := jwtValidator.ValidateToken(context.Background(), token)
            if err != nil {
                return echo.NewHTTPError(http.StatusUnauthorized, "Invalid token")
            }
            
            claims := validatedToken.(*validator.ValidatedClaims)
            
            // Set user context
            c.Set("user_id", claims.Subject)
            c.Set("user_claims", claims)
            
            return next(c)
        }
    }
}

type CustomClaims struct {
    Email      string `json:"email"`
    DataRegion string `json:"https://boostedbudget.com/data_region"`
    Scope      string `json:"scope"`
}

func (c CustomClaims) Validate(ctx context.Context) error {
    return nil
}
```

- [ ] Create user service with Auth0:
```go
// backend/internal/services/user_service.go
package services

import (
    "context"
    "github.com/auth0/go-auth0/management"
)

type UserService struct {
    auth0Client *management.Management
    userRepo    UserRepository
    privacyRepo PrivacyRepository
}

func (s *UserService) GetOrCreateUser(ctx context.Context, auth0ID string) (*User, error) {
    // Check if user exists in our database
    user, err := s.userRepo.GetByAuth0ID(ctx, auth0ID)
    if err == nil {
        return user, nil
    }
    
    // Get user from Auth0
    auth0User, err := s.auth0Client.User.Read(auth0ID)
    if err != nil {
        return nil, err
    }
    
    // Determine data region based on metadata
    dataRegion := "us-east-1" // default
    if auth0User.AppMetadata["data_region"] != nil {
        dataRegion = auth0User.AppMetadata["data_region"].(string)
    }
    
    // Create user in appropriate database region
    user = &User{
        ID:         generateUUID(),
        Auth0ID:    auth0ID,
        Email:      auth0User.Email,
        DataRegion: dataRegion,
        CreatedAt:  time.Now(),
    }
    
    if err := s.userRepo.CreateInRegion(ctx, user, dataRegion); err != nil {
        return nil, err
    }
    
    // Initialize privacy settings (GDPR)
    privacy := &PrivacySettings{
        UserID:                   user.ID,
        DataRegion:              dataRegion,
        AnalyticsConsent:        false, // Default opt-out
        MarketingConsent:        false,
        DataSharingConsent:      false,
        AdPersonalizationConsent: false,
    }
    
    s.privacyRepo.Create(ctx, privacy)
    
    return user, nil
}

func (s *UserService) DeleteUser(ctx context.Context, userID string) error {
    // GDPR compliance: Complete deletion
    user, err := s.userRepo.GetByID(ctx, userID)
    if err != nil {
        return err
    }
    
    // Schedule deletion (30-day grace period)
    deletionDate := time.Now().AddDate(0, 0, 30)
    
    if err := s.userRepo.ScheduleDeletion(ctx, userID, deletionDate); err != nil {
        return err
    }
    
    // Mark Auth0 user for deletion
    s.auth0Client.User.Update(user.Auth0ID, &management.User{
        AppMetadata: map[string]interface{}{
            "scheduled_deletion": deletionDate.Format(time.RFC3339),
        },
    })
    
    // Send confirmation email
    go s.emailService.SendDeletionConfirmation(user.Email)
    
    return nil
}

func (s *UserService) ExportUserData(ctx context.Context, userID string) (*UserDataExport, error) {
    // GDPR right to data portability
    export := &UserDataExport{
        UserID:    userID,
        Timestamp: time.Now(),
        Data:      make(map[string]interface{}),
    }
    
    // Collect all user data
    user, _ := s.userRepo.GetByID(ctx, userID)
    accounts, _ := s.accountRepo.GetByUserID(ctx, userID)
    transactions, _ := s.transactionRepo.GetByUserID(ctx, userID)
    budgets, _ := s.budgetRepo.GetByUserID(ctx, userID)
    rules, _ := s.ruleRepo.GetByUserID(ctx, userID)
    
    export.Data["user"] = user
    export.Data["accounts"] = accounts
    export.Data["transactions"] = transactions
    export.Data["budgets"] = budgets
    export.Data["rules"] = rules
    
    return export, nil
}
```

### 0.5 Frontend Auth0 Integration
- [ ] Configure Auth0 React SDK:
```typescript
// frontend/src/index.tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import { Auth0Provider } from '@auth0/auth0-react';
import { BrowserRouter } from 'react-router-dom';
import App from './App';

const auth0Config = {
  domain: import.meta.env.VITE_AUTH0_DOMAIN,
  clientId: import.meta.env.VITE_AUTH0_CLIENT_ID,
  authorizationParams: {
    redirect_uri: window.location.origin + '/callback',
    audience: import.meta.env.VITE_AUTH0_AUDIENCE,
    scope: 'openid profile email'
  },
  cacheLocation: 'localstorage' as const,
  useRefreshTokens: true
};

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Auth0Provider {...auth0Config}>
        <App />
      </Auth0Provider>
    </BrowserRouter>
  </React.StrictMode>
);
```

- [ ] Create authentication wrapper:
```typescript
// frontend/src/auth/AuthWrapper.tsx
import { useAuth0 } from '@auth0/auth0-react';
import { useEffect } from 'react';
import { useAppDispatch } from '@/app/hooks';
import { setUser, clearUser } from '@/features/auth/authSlice';
import { LoadingSpinner } from '@/shared/components/LoadingSpinner';

export const AuthWrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const { isLoading, isAuthenticated, user, getAccessTokenSilently } = useAuth0();
  const dispatch = useAppDispatch();
  
  useEffect(() => {
    if (isAuthenticated && user) {
      // Set user in Redux store
      dispatch(setUser({
        id: user.sub!,
        email: user.email!,
        name: user.name,
        picture: user.picture
      }));
      
      // Configure API client with token
      getAccessTokenSilently().then(token => {
        window.localStorage.setItem('auth0_token', token);
        // Configure axios defaults
        axios.defaults.headers.common['Authorization'] = `Bearer ${token}`;
      });
    } else {
      dispatch(clearUser());
    }
  }, [isAuthenticated, user]);
  
  if (isLoading) {
    return <LoadingSpinner fullScreen />;
  }
  
  return <>{children}</>;
};
```

- [ ] Create protected routes:
```typescript
// frontend/src/auth/ProtectedRoute.tsx
import { withAuthenticationRequired } from '@auth0/auth0-react';
import { LoadingSpinner } from '@/shared/components/LoadingSpinner';

export const ProtectedRoute: React.FC<{ component: React.ComponentType }> = ({ 
  component 
}) => {
  const Component = withAuthenticationRequired(component, {
    onRedirecting: () => <LoadingSpinner fullScreen />
  });
  
  return <Component />;
};
```

### 0.6 GDPR Compliance Components
- [ ] Create cookie consent banner:
```typescript
// frontend/src/privacy/CookieConsent.tsx
import { useState, useEffect } from 'react';
import { useCookies } from 'react-cookie';

export const CookieConsent: React.FC = () => {
  const [cookies, setCookie] = useCookies(['gdpr-consent']);
  const [showBanner, setShowBanner] = useState(false);
  
  useEffect(() => {
    if (!cookies['gdpr-consent']) {
      setShowBanner(true);
    }
  }, [cookies]);
  
  const handleAcceptAll = () => {
    const consent = {
      necessary: true,
      analytics: true,
      marketing: true,
      advertising: true,
      timestamp: new Date().toISOString()
    };
    
    setCookie('gdpr-consent', consent, {
      path: '/',
      maxAge: 365 * 24 * 60 * 60, // 1 year
      sameSite: 'strict',
      secure: true
    });
    
    // Initialize Google AdSense
    initializeAdSense(consent);
    
    setShowBanner(false);
  };
  
  const handleAcceptNecessary = () => {
    const consent = {
      necessary: true,
      analytics: false,
      marketing: false,
      advertising: false,
      timestamp: new Date().toISOString()
    };
    
    setCookie('gdpr-consent', consent, {
      path: '/',
      maxAge: 365 * 24 * 60 * 60,
      sameSite: 'strict',
      secure: true
    });
    
    setShowBanner(false);
  };
  
  if (!showBanner) return null;
  
  return (
    <div className="fixed bottom-0 left-0 right-0 bg-white shadow-lg border-t z-50">
      <div className="max-w-7xl mx-auto px-4 py-4">
        <div className="flex items-center justify-between">
          <div className="flex-1 pr-8">
            <h3 className="font-semibold text-lg">Cookie Preferences</h3>
            <p className="text-sm text-gray-600 mt-1">
              BoostedBudget uses cookies to improve your experience and show relevant ads. 
              You can choose to accept all cookies or only necessary ones. 
              <a href="/privacy" className="underline ml-1">Learn more</a>
            </p>
          </div>
          <div className="flex gap-2">
            <button
              onClick={handleAcceptNecessary}
              className="px-4 py-2 border border-gray-300 rounded hover:bg-gray-50"
            >
              Necessary Only
            </button>
            <button
              onClick={handleAcceptAll}
              className="px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700"
            >
              Accept All
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
```

### 0.7 Docker Development Environment with Auth0
- [ ] Create development Docker setup:
```yaml
# infrastructure/docker/docker-compose.yml
version: '3.9'

services:
  # PostgreSQL Database (Multi-region simulation)
  postgres-us:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: boostedbudget_us
      POSTGRES_USER: devuser
      POSTGRES_PASSWORD: devpass
    ports:
      - "5432:5432"
    volumes:
      - postgres_us_data:/var/lib/postgresql/data
      
  postgres-eu:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: boostedbudget_eu
      POSTGRES_USER: devuser
      POSTGRES_PASSWORD: devpass
    ports:
      - "5433:5432"
    volumes:
      - postgres_eu_data:/var/lib/postgresql/data

  # Redis Cache
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  # RabbitMQ Message Queue
  rabbitmq:
    image: rabbitmq:3-management-alpine
    ports:
      - "5672:5672"
      - "15672:15672"

  # Backend Development
  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile.dev
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL_US: postgres://devuser:devpass@postgres-us:5432/boostedbudget_us
      DATABASE_URL_EU: postgres://devuser:devpass@postgres-eu:5432/boostedbudget_eu
      REDIS_URL: redis://redis:6379
      RABBITMQ_URL: amqp://devuser:devpass@rabbitmq:5672/
      AUTH0_DOMAIN: ${AUTH0_DOMAIN}
      AUTH0_AUDIENCE: ${AUTH0_AUDIENCE}
      AUTH0_CLIENT_ID: ${AUTH0_CLIENT_ID}
      AUTH0_CLIENT_SECRET: ${AUTH0_CLIENT_SECRET}
      PLAID_CLIENT_ID: ${PLAID_CLIENT_ID}
      PLAID_SECRET: ${PLAID_SECRET}
      PLAID_ENV: sandbox
    depends_on:
      - postgres-us
      - postgres-eu
      - redis
      - rabbitmq
    volumes:
      - ./backend:/app
      - go_modules:/go/pkg/mod

  # Frontend Development
  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile.dev
    ports:
      - "3000:3000"
    environment:
      VITE_API_URL: http://localhost:8080/api/v1
      VITE_AUTH0_DOMAIN: ${AUTH0_DOMAIN}
      VITE_AUTH0_CLIENT_ID: ${AUTH0_CLIENT_ID}
      VITE_AUTH0_AUDIENCE: ${AUTH0_AUDIENCE}
      VITE_GOOGLE_ADSENSE_CLIENT: ${GOOGLE_ADSENSE_CLIENT}
      VITE_PLAID_PUBLIC_KEY: ${PLAID_PUBLIC_KEY}
    volumes:
      - ./frontend:/app
      - node_modules:/app/node_modules

volumes:
  postgres_us_data:
  postgres_eu_data:
  go_modules:
  node_modules:
```

### 0.8 Database Schema with GDPR
- [ ] Create initial database migrations:
```sql
-- migrations/001_create_users_table.up.sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    auth0_id VARCHAR(255) UNIQUE NOT NULL,
    email VARCHAR(255) NOT NULL,
    data_region VARCHAR(20) NOT NULL DEFAULT 'us-east-1',
    subscription_tier VARCHAR(20) DEFAULT 'free',
    subscription_expires_at TIMESTAMP,
    
    -- GDPR fields
    gdpr_consent BOOLEAN DEFAULT FALSE,
    gdpr_consent_date TIMESTAMP,
    data_export_requested_at TIMESTAMP,
    deletion_requested_at TIMESTAMP,
    deletion_scheduled_for TIMESTAMP,
    
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_users_auth0_id ON users(auth0_id);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_deletion_scheduled ON users(deletion_scheduled_for);

-- Privacy settings table (GDPR)
CREATE TABLE privacy_settings (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    analytics_consent BOOLEAN DEFAULT FALSE,
    marketing_consent BOOLEAN DEFAULT FALSE,
    data_sharing_consent BOOLEAN DEFAULT FALSE,
    ad_personalization_consent BOOLEAN DEFAULT FALSE,
    consent_updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Audit log for GDPR compliance
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id VARCHAR(255),
    ip_address INET,
    user_agent TEXT,
    metadata JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
```

### Phase 0 Deliverables
✅ **Deployable Application:**
- Health check endpoint (no auth required)
- Auth0 authentication configured
- GDPR cookie consent banner
- Multi-region database setup
- Global CDN via CloudFlare

✅ **Compliance:**
- GDPR consent tracking
- Privacy settings management
- Data export endpoint
- Account deletion endpoint
- Audit logging

✅ **Development Environment:**
- Fully Dockerized
- Auth0 integrated
- Multi-region database simulation
- OpenAPI specification with Auth0 security

### Phase 0 Deployment Checklist
- [ ] Auth0 tenant configured
- [ ] Health endpoint responds with 200 OK
- [ ] Auth0 login flow works
- [ ] GDPR consent banner appears
- [ ] Docker images build successfully
- [ ] Multi-region database connections work
- [ ] CloudFlare CDN configured
- [ ] Deployment to production successful

---

## Phase 1: User Profile & Subscription Management (Week 2)
*Deliverable: Complete user profile system with subscription tiers*

### 1.1 User Profile API
- [ ] Extend OpenAPI with profile endpoints:
```yaml
# api/paths/profile.yaml
/profile:
  get:
    operationId: getProfile
    tags: [Profile]
    summary: Get user profile
    responses:
      '200':
        description: User profile
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Profile'
              
  put:
    operationId: updateProfile
    tags: [Profile]
    summary: Update user profile
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/UpdateProfileRequest'
            
/profile/subscription:
  get:
    operationId: getSubscription
    tags: [Profile]
    summary: Get subscription details
    responses:
      '200':
        description: Subscription details
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/SubscriptionDetails'
```

### 1.2 Subscription Tiers Implementation
- [ ] Create subscription service:
```go
// backend/internal/services/subscription_service.go
package services

type SubscriptionTier string

const (
    TierFree        SubscriptionTier = "free"
    TierPremium     SubscriptionTier = "premium"
    TierPremiumPlus SubscriptionTier = "premium_plus"
)

type SubscriptionLimits struct {
    MaxRules           int
    MaxAccountsLinked  int
    RuleExecutionsPerDay int
    HasAds             bool
    HasAPIAccess       bool
    HasAIInsights      bool
}

var TierLimits = map[SubscriptionTier]SubscriptionLimits{
    TierFree: {
        MaxRules:           3,
        MaxAccountsLinked:  2,
        RuleExecutionsPerDay: 10,
        HasAds:             true,
        HasAPIAccess:       false,
        HasAIInsights:      false,
    },
    TierPremium: {
        MaxRules:           -1, // unlimited
        MaxAccountsLinked:  10,
        RuleExecutionsPerDay: -1,
        HasAds:             false,
        HasAPIAccess:       true,
        HasAIInsights:      false,
    },
    TierPremiumPlus: {
        MaxRules:           -1,
        MaxAccountsLinked:  -1,
        RuleExecutionsPerDay: -1,
        HasAds:             false,
        HasAPIAccess:       true,
        HasAIInsights:      true,
    },
}

func (s *SubscriptionService) CheckLimit(ctx context.Context, userID string, limitType string) (bool, error) {
    user, err := s.userRepo.GetByID(ctx, userID)
    if err != nil {
        return false, err
    }
    
    limits := TierLimits[user.SubscriptionTier]
    
    switch limitType {
    case "rules":
        if limits.MaxRules == -1 {
            return true, nil
        }
        count, _ := s.ruleRepo.CountByUserID(ctx, userID)
        return count < limits.MaxRules, nil
        
    case "accounts":
        if limits.MaxAccountsLinked == -1 {
            return true, nil
        }
        count, _ := s.accountRepo.CountByUserID(ctx, userID)
        return count < limits.MaxAccountsLinked, nil
    }
    
    return true, nil
}
```

### 1.3 Frontend Profile Components
- [ ] Create profile management UI:
```typescript
// frontend/src/features/profile/ProfilePage.tsx
import { useAuth0 } from '@auth0/auth0-react';
import { useQuery } from '@tanstack/react-query';

export const ProfilePage: React.FC = () => {
  const { user } = useAuth0();
  const { data: profile } = useQuery({
    queryKey: ['profile'],
    queryFn: () => api.getProfile()
  });
  
  return (
    <div className="max-w-4xl mx-auto p-6">
      <h1 className="text-2xl font-bold mb-6">Profile Settings</h1>
      
      <div className="bg-white rounded-lg shadow p-6 mb-6">
        <h2 className="text-lg font-semibold mb-4">Personal Information</h2>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700">Email</label>
            <p className="mt-1">{user?.email}</p>
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700">Data Region</label>
            <select 
              value={profile?.dataRegion}
              onChange={handleRegionChange}
              className="mt-1 block w-full border-gray-300 rounded-md"
            >
              <option value="us-east-1">United States</option>
              <option value="eu-west-1">European Union</option>
            </select>
            <p className="text-xs text-gray-500 mt-1">
              Your data will be stored in the selected region for compliance
            </p>
          </div>
        </div>
      </div>
      
      <SubscriptionCard subscription={profile?.subscription} />
      <PrivacySettings />
      <DangerZone />
    </div>
  );
};
```

### Phase 1 Deliverables
✅ **User Management:**
- Profile viewing and editing
- Subscription tier display
- Data region selection
- Privacy settings (GDPR)

---

## Phase 2: Plaid Integration & Financial Accounts (Weeks 3-4)
*Deliverable: Bank account linking with transaction import*

### 2.1 Plaid Configuration
- [ ] Set up Plaid for multi-region:
```go
// backend/internal/services/plaid_service.go
package services

import (
    "github.com/plaid/plaid-go/v3/plaid"
)

type PlaidService struct {
    clients map[string]*plaid.APIClient // Region-specific clients
    config  PlaidConfig
}

func NewPlaidService(config PlaidConfig) *PlaidService {
    clients := make(map[string]*plaid.APIClient)
    
    // US configuration
    usConfig := plaid.NewConfiguration()
    usConfig.AddDefaultHeader("PLAID-CLIENT-ID", config.ClientID)
    usConfig.AddDefaultHeader("PLAID-SECRET", config.SecretUS)
    usConfig.UseEnvironment(plaid.Sandbox) // Change to Production later
    clients["us-east-1"] = plaid.NewAPIClient(usConfig)
    
    // EU configuration (when available)
    // Plaid is expanding to EU, prepare for it
    euConfig := plaid.NewConfiguration()
    euConfig.AddDefaultHeader("PLAID-CLIENT-ID", config.ClientID)
    euConfig.AddDefaultHeader("PLAID-SECRET", config.SecretEU)
    euConfig.UseEnvironment(plaid.Sandbox)
    clients["eu-west-1"] = plaid.NewAPIClient(euConfig)
    
    return &PlaidService{
        clients: clients,
        config:  config,
    }
}

func (s *PlaidService) CreateLinkToken(ctx context.Context, userID string, region string) (*LinkTokenResponse, error) {
    client := s.clients[region]
    
    request := plaid.NewLinkTokenCreateRequest(
        "BoostedBudget",
        "en",
        []plaid.CountryCode{plaid.COUNTRYCODE_US}, // Add EU countries when available
        plaid.LinkTokenCreateRequestUser{
            ClientUserId: userID,
        },
    )
    
    request.SetProducts([]plaid.Products{
        plaid.PRODUCTS_TRANSACTIONS,
        plaid.PRODUCTS_ACCOUNTS,
        plaid.PRODUCTS_BALANCE,
    })
    
    // Enable Transfer product for future ACH capabilities
    // request.SetProducts(append(request.GetProducts(), plaid.PRODUCTS_TRANSFER))
    
    request.SetWebhook(s.config.WebhookURL)
    
    response, _, err := client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
    if err != nil {
        return nil, err
    }
    
    return &LinkTokenResponse{
        LinkToken: response.GetLinkToken(),
        Expiration: response.GetExpiration(),
    }, nil
}

// Prepare for future transfer suggestions
func (s *PlaidService) SuggestTransfer(ctx context.Context, userID string, amount float64, fromAccount, toAccount string) (*TransferSuggestion, error) {
    // For MVP: Just create a suggestion, don't actually move money
    suggestion := &TransferSuggestion{
        ID:          generateUUID(),
        UserID:      userID,
        Amount:      amount,
        FromAccount: fromAccount,
        ToAccount:   toAccount,
        Reason:      "Budget optimization",
        CreatedAt:   time.Now(),
        Status:      "suggested",
    }
    
    // In future phases, this will use Plaid Transfer API
    // to actually move money with proper compliance
    
    return suggestion, nil
}
```

### 2.2 Account Management
- [ ] Create account service:
```go
// backend/internal/services/account_service.go
package services

func (s *AccountService) LinkAccount(ctx context.Context, userID string, publicToken string) error {
    // Check account limit based on subscription
    canLink, err := s.subscriptionService.CheckLimit(ctx, userID, "accounts")
    if err != nil {
        return err
    }
    
    if !canLink {
        return ErrAccountLimitReached
    }
    
    // Exchange public token
    accessToken, itemID, err := s.plaidService.ExchangePublicToken(ctx, publicToken)
    if err != nil {
        return err
    }
    
    // Get accounts from Plaid
    accounts, err := s.plaidService.GetAccounts(ctx, accessToken)
    if err != nil {
        return err
    }
    
    // Store encrypted access token
    encryptedToken := s.encryption.Encrypt(accessToken)
    
    // Save linked item
    linkedItem := &LinkedItem{
        ID:              generateUUID(),
        UserID:          userID,
        PlaidItemID:     itemID,
        PlaidAccessToken: encryptedToken,
        LastSynced:      time.Now(),
    }
    
    // Store in user's data region
    user, _ := s.userRepo.GetByID(ctx, userID)
    if err := s.itemRepo.CreateInRegion(ctx, linkedItem, user.DataRegion); err != nil {
        return err
    }
    
    // Store accounts
    for _, account := range accounts {
        acc := &Account{
            ID:              generateUUID(),
            LinkedItemID:    linkedItem.ID,
            PlaidAccountID:  account.ID,
            Name:           account.Name,
            Type:           account.Type,
            Subtype:        account.Subtype,
            CurrentBalance: account.Balance.Current,
        }
        
        s.accountRepo.CreateInRegion(ctx, acc, user.DataRegion)
    }
    
    // Start transaction sync
    go s.transactionSync.SyncAccount(context.Background(), linkedItem.ID)
    
    return nil
}
```

### 2.3 Transaction Sync with GDPR Compliance
- [ ] Implement transaction synchronization:
```go
// backend/internal/workers/transaction_sync.go
package workers

func (w *TransactionSyncWorker) SyncAccount(ctx context.Context, linkedItemID string) error {
    linkedItem, err := w.itemRepo.GetByID(ctx, linkedItemID)
    if err != nil {
        return err
    }
    
    // Decrypt access token
    accessToken := w.encryption.Decrypt(linkedItem.PlaidAccessToken)
    
    // Use transactions sync for incremental updates
    request := plaid.NewTransactionsSyncRequest(accessToken)
    if linkedItem.Cursor != "" {
        request.SetCursor(linkedItem.Cursor)
    }
    
    for hasMore := true; hasMore; {
        response, _, err := w.plaidClient.TransactionsSync(ctx, request)
        if err != nil {
            return err
        }
        
        // Process transactions
        for _, txn := range response.GetAdded() {
            transaction := &Transaction{
                ID:              generateUUID(),
                AccountID:       w.getAccountID(txn.AccountId),
                PlaidID:         txn.TransactionId,
                Amount:         txn.Amount,
                Date:           txn.Date,
                Name:           txn.Name,
                Category:       txn.Category,
                Pending:        txn.Pending,
                
                // GDPR: Track data lineage
                DataSource:     "plaid",
                DataImportedAt: time.Now(),
            }
            
            // Store in user's data region
            user, _ := w.userRepo.GetByLinkedItemID(ctx, linkedItemID)
            w.transactionRepo.CreateInRegion(ctx, transaction, user.DataRegion)
            
            // Trigger budget tracking
            w.eventBus.Publish(TransactionAddedEvent{
                UserID:        user.ID,
                TransactionID: transaction.ID,
                Amount:       transaction.Amount,
                Category:     transaction.Category,
            })
        }
        
        // Update cursor
        linkedItem.Cursor = response.GetNextCursor()
        w.itemRepo.UpdateCursor(ctx, linkedItemID, response.GetNextCursor())
        
        hasMore = response.GetHasMore()
        if hasMore {
            request.SetCursor(response.GetNextCursor())
        }
    }
    
    // Update last synced
    linkedItem.LastSynced = time.Now()
    w.itemRepo.Update(ctx, linkedItem)
    
    return nil
}
```

### Phase 2 Deliverables
✅ **Plaid Integration:**
- Account linking via Plaid Link
- Transaction import and sync
- Balance updates
- Multi-region data storage
- GDPR-compliant data handling

---

## Phase 3: Core Budgeting System (Weeks 5-6)
*Deliverable: Complete budgeting functionality*

### 3.1 Budget Creation and Management
- [ ] Implement budget service:
```go
// backend/internal/services/budget_service.go
package services

func (s *BudgetService) CreateBudget(ctx context.Context, userID string, req CreateBudgetRequest) (*Budget, error) {
    // Get user for data region
    user, err := s.userRepo.GetByID(ctx, userID)
    if err != nil {
        return nil, err
    }
    
    budget := &Budget{
        ID:        generateUUID(),
        UserID:    userID,
        Name:      req.Name,
        Period:    req.Period,
        StartDate: req.StartDate,
        EndDate:   req.EndDate,
        Amount:    req.Amount,
        CreatedAt: time.Now(),
    }
    
    // Store in user's data region
    if err := s.budgetRepo.CreateInRegion(ctx, budget, user.DataRegion); err != nil {
        return nil, err
    }
    
    // Create default categories
    s.createDefaultCategories(ctx, budget.ID, user.DataRegion)
    
    // Audit log for GDPR
    s.auditLog.Log(ctx, AuditEntry{
        UserID:     userID,
        Action:     "budget_created",
        ResourceID: budget.ID,
        IPAddress:  getIPFromContext(ctx),
    })
    
    return budget, nil
}
```

### 3.2 Budget Progress Tracking
- [ ] Real-time budget tracking:
```typescript
// frontend/src/features/budgets/BudgetDashboard.tsx
import { useQuery, useSubscription } from '@tanstack/react-query';

export const BudgetDashboard: React.FC = () => {
  const { data: budget } = useQuery({
    queryKey: ['current-budget'],
    queryFn: () => api.getCurrentBudget()
  });
  
  // Real-time updates via WebSocket
  useSubscription({
    channel: `budget-${budget?.id}`,
    onMessage: (update) => {
      queryClient.setQueryData(['current-budget'], (old) => ({
        ...old,
        ...update
      }));
    }
  });
  
  return (
    <div className="space-y-6">
      <BudgetOverview budget={budget} />
      <CategoryBreakdown categories={budget?.categories} />
      <SpendingTrends />
      <BudgetAlerts alerts={budget?.alerts} />
    </div>
  );
};
```

### Phase 3 Deliverables
✅ **Budgeting Features:**
- Budget creation with categories
- Real-time spending tracking
- Budget vs. actual analysis
- Spending alerts
- Category management

---

## Phase 4: Rules Engine MVP (Weeks 7-9)
*Deliverable: Automation rules with free tier limits*

### 4.1 Rules Engine Core
- [ ] Implement rules engine with tier limits:
```go
// backend/internal/rules/engine.go
package rules

type RulesEngine struct {
    ruleRepo        RuleRepository
    subscriptionSvc SubscriptionService
    executorSvc     ExecutorService
    plaidSvc        PlaidService
}

func (e *RulesEngine) CreateRule(ctx context.Context, userID string, req CreateRuleRequest) (*Rule, error) {
    // Check subscription limits
    canCreate, err := e.subscriptionSvc.CheckLimit(ctx, userID, "rules")
    if err != nil {
        return nil, err
    }
    
    if !canCreate {
        return nil, ErrRuleLimitReached
    }
    
    rule := &Rule{
        ID:          generateUUID(),
        UserID:      userID,
        Name:        req.Name,
        Description: req.Description,
        Trigger:     req.Trigger,
        Conditions:  req.Conditions,
        Actions:     req.Actions,
        Enabled:     true,
        CreatedAt:   time.Now(),
    }
    
    // Get user for data region
    user, _ := e.userRepo.GetByID(ctx, userID)
    
    // Store in user's data region
    if err := e.ruleRepo.CreateInRegion(ctx, rule, user.DataRegion); err != nil {
        return nil, err
    }
    
    // Schedule if needed
    if rule.Trigger.Type == "schedule" {
        e.scheduler.Schedule(rule)
    }
    
    return rule, nil
}

func (e *RulesEngine) ExecuteRule(ctx context.Context, rule *Rule) error {
    // Check daily execution limits for free tier
    user, _ := e.userRepo.GetByID(ctx, rule.UserID)
    limits := TierLimits[user.SubscriptionTier]
    
    if limits.RuleExecutionsPerDay > 0 {
        count, _ := e.executionRepo.CountTodayByUserID(ctx, rule.UserID)
        if count >= limits.RuleExecutionsPerDay {
            return ErrDailyExecutionLimitReached
        }
    }
    
    execution := &RuleExecution{
        ID:        generateUUID(),
        RuleID:    rule.ID,
        StartTime: time.Now(),
        Status:    "running",
    }
    
    // Execute actions
    for _, action := range rule.Actions {
        switch action.Type {
        case "transfer_suggestion":
            // For MVP: Create transfer suggestion, don't move money
            e.suggestTransfer(ctx, rule.UserID, action.Parameters)
            
        case "category_rollover":
            e.executeCategoryRollover(ctx, rule.UserID, action.Parameters)
            
        case "savings_goal":
            e.updateSavingsGoal(ctx, rule.UserID, action.Parameters)
            
        case "alert":
            e.sendAlert(ctx, rule.UserID, action.Parameters)
        }
    }
    
    execution.Status = "completed"
    execution.EndTime = time.Now()
    e.executionRepo.Create(ctx, execution)
    
    return nil
}

func (e *RulesEngine) suggestTransfer(ctx context.Context, userID string, params map[string]interface{}) error {
    // Create transfer suggestion using Plaid
    amount := params["amount"].(float64)
    fromAccount := params["from_account"].(string)
    toAccount := params["to_account"].(string)
    
    suggestion, err := e.plaidSvc.SuggestTransfer(ctx, userID, amount, fromAccount, toAccount)
    if err != nil {
        return err
    }
    
    // Store suggestion for user review
    e.suggestionRepo.Create(ctx, suggestion)
    
    // Send notification
    e.notificationSvc.Send(ctx, userID, Notification{
        Type:    "transfer_suggestion",
        Title:   "Transfer Suggestion",
        Message: fmt.Sprintf("Consider transferring $%.2f from %s to %s", amount, fromAccount, toAccount),
        ActionURL: fmt.Sprintf("/suggestions/%s", suggestion.ID),
    })
    
    return nil
}
```

### 4.2 Pre-built Rule Templates
- [ ] Create common rule templates:
```typescript
// frontend/src/features/rules/RuleTemplates.tsx
export const ruleTemplates = [
  {
    id: 'savings-sweep',
    name: 'Automatic Savings Sweep',
    description: 'Transfer leftover budget to savings at end of month',
    icon: '💰',
    isPremium: false,
    template: {
      trigger: {
        type: 'schedule',
        schedule: { frequency: 'monthly', dayOfMonth: -1 }
      },
      conditions: [
        { field: 'budget.remaining', operator: 'greater_than', value: 0 }
      ],
      actions: [
        { type: 'transfer_suggestion', parameters: {
          from: 'checking',
          to: 'savings',
          amount: '${budget.remaining}'
        }}
      ]
    }
  },
  {
    id: 'spending-alert',
    name: 'Spending Limit Alert',
    description: 'Alert when category spending exceeds threshold',
    icon: '🚨',
    isPremium: false,
    template: {
      trigger: { type: 'transaction' },
      conditions: [
        { field: 'category.spending_percentage', operator: 'greater_than', value: 80 }
      ],
      actions: [
        { type: 'alert', parameters: {
          severity: 'warning',
          message: 'You\'ve spent ${percentage}% of your ${category} budget'
        }}
      ]
    }
  },
  {
    id: 'round-up-savings',
    name: 'Round-Up Savings',
    description: 'Round up transactions and save the difference',
    icon: '🪙',
    isPremium: true,
    template: {
      trigger: { type: 'transaction' },
      conditions: [
        { field: 'transaction.amount_decimal', operator: 'greater_than', value: 0 }
      ],
      actions: [
        { type: 'transfer_suggestion', parameters: {
          amount: 'ROUNDUP(${transaction.amount}) - ${transaction.amount}',
          to: 'savings'
        }}
      ]
    }
  }
];
```

### 4.3 Rule Builder UI
- [ ] Visual rule builder:
```typescript
// frontend/src/features/rules/RuleBuilder.tsx
import { useState } from 'react';
import { useAuth0 } from '@auth0/auth0-react';

export const RuleBuilder: React.FC = () => {
  const { user } = useAuth0();
  const [selectedTemplate, setSelectedTemplate] = useState(null);
  const { data: subscription } = useQuery(['subscription'], api.getSubscription);
  
  const maxRules = subscription?.tier === 'free' ? 3 : -1;
  const { data: currentRules } = useQuery(['rules'], api.getRules);
  
  const canCreateRule = maxRules === -1 || currentRules?.length < maxRules;
  
  if (!canCreateRule) {
    return (
      <UpgradePrompt
        message={`You've reached your limit of ${maxRules} rules`}
        ctaText="Upgrade for Unlimited Rules"
        ctaLink="/pricing"
      />
    );
  }
  
  return (
    <div className="space-y-6">
      <RuleTemplateSelector
        templates={ruleTemplates}
        onSelect={setSelectedTemplate}
        isPremium={subscription?.tier !== 'free'}
      />
      
      {selectedTemplate && (
        <RuleCustomizer
          template={selectedTemplate}
          onSave={handleSave}
          onTest={handleTest}
        />
      )}
    </div>
  );
};
```

### Phase 4 Deliverables
✅ **Rules Engine:**
- 2-3 free rules (tier-based limits)
- Transfer suggestions (not actual transfers)
- Budget automation
- Visual rule builder
- Pre-built templates
- Execution history

---

## Phase 5: Monetization & Premium Features (Weeks 10-11)
*Deliverable: Stripe payments and Google AdSense integration*

### 5.1 Stripe Payment Integration
- [ ] Implement Stripe subscription:
```go
// backend/internal/services/payment_service.go
package services

import (
    "github.com/stripe/stripe-go/v74"
    "github.com/stripe/stripe-go/v74/subscription"
)

type PaymentService struct {
    stripe      *stripe.Client
    userRepo    UserRepository
}

func (s *PaymentService) CreateCheckoutSession(ctx context.Context, userID string, priceID string) (*CheckoutSession, error) {
    user, err := s.userRepo.GetByID(ctx, userID)
    if err != nil {
        return nil, err
    }
    
    // Create or retrieve Stripe customer
    customerID, err := s.getOrCreateCustomer(user)
    if err != nil {
        return nil, err
    }
    
    params := &stripe.CheckoutSessionParams{
        Customer: stripe.String(customerID),
        Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
        LineItems: []*stripe.CheckoutSessionLineItemParams{
            {
                Price:    stripe.String(priceID),
                Quantity: stripe.Int64(1),
            },
        },
        SuccessURL: stripe.String("https://boostedbudget.com/subscription/success?session_id={CHECKOUT_SESSION_ID}"),
        CancelURL:  stripe.String("https://boostedbudget.com/pricing"),
        
        // Enable 14-day trial
        SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
            TrialPeriodDays: stripe.Int64(14),
        },
        
        // Collect consent for data processing
        ConsentCollection: &stripe.CheckoutSessionConsentCollectionParams{
            Terms: stripe.String("required"),
        },
    }
    
    session, err := s.stripe.CheckoutSessions.New(params)
    if err != nil {
        return nil, err
    }
    
    return &CheckoutSession{
        ID:  session.ID,
        URL: session.URL,
    }, nil
}

func (s *PaymentService) HandleWebhook(ctx context.Context, payload []byte, signature string) error {
    event, err := webhook.ConstructEvent(payload, signature, s.webhookSecret)
    if err != nil {
        return err
    }
    
    switch event.Type {
    case "customer.subscription.created", "customer.subscription.updated":
        var subscription stripe.Subscription
        if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
            return err
        }
        
        // Update user subscription
        tier := s.mapStripePriceToTier(subscription.Items.Data[0].Price.ID)
        s.userRepo.UpdateSubscription(ctx, subscription.Customer.ID, tier, subscription.CurrentPeriodEnd)
        
        // Update Auth0 metadata
        s.auth0Service.UpdateUserMetadata(subscription.Customer.ID, map[string]interface{}{
            "subscription_tier": tier,
            "subscription_active": subscription.Status == "active",
        })
        
    case "customer.subscription.deleted":
        var subscription stripe.Subscription
        if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
            return err
        }
        
        // Downgrade to free tier
        s.userRepo.UpdateSubscription(ctx, subscription.Customer.ID, TierFree, nil)
    }
    
    return nil
}
```

### 5.2 Google AdSense Integration
- [ ] Implement ad components:
```typescript
// frontend/src/features/ads/AdBanner.tsx
import { useEffect } from 'react';
import { useAuth0 } from '@auth0/auth0-react';
import { useCookies } from 'react-cookie';

interface AdBannerProps {
  slot: string;
  format?: 'auto' | 'fluid' | 'rectangle';
  style?: React.CSSProperties;
}

export const AdBanner: React.FC<AdBannerProps> = ({ 
  slot, 
  format = 'auto',
  style 
}) => {
  const { user } = useAuth0();
  const [cookies] = useCookies(['gdpr-consent']);
  const { data: subscription } = useQuery(['subscription'], api.getSubscription);
  
  // Don't show ads for premium users
  if (subscription?.tier !== 'free') {
    return null;
  }
  
  // Check GDPR consent for advertising
  const hasAdConsent = cookies['gdpr-consent']?.advertising;
  
  useEffect(() => {
    if (!hasAdConsent) return;
    
    try {
      (window.adsbygoogle = window.adsbygoogle || []).push({});
    } catch (err) {
      console.error('AdSense error:', err);
    }
  }, [hasAdConsent]);
  
  if (!hasAdConsent) {
    return (
      <div className="bg-gray-100 p-4 rounded text-center">
        <p className="text-sm text-gray-600">
          Enable advertising cookies to support free BoostedBudget
        </p>
        <button 
          onClick={() => window.location.href = '/privacy-settings'}
          className="text-blue-600 underline text-sm mt-2"
        >
          Update Privacy Settings
        </button>
      </div>
    );
  }
  
  return (
    <ins
      className="adsbygoogle"
      style={{ display: 'block', ...style }}
      data-ad-client={import.meta.env.VITE_GOOGLE_ADSENSE_CLIENT}
      data-ad-slot={slot}
      data-ad-format={format}
      data-full-width-responsive="true"
    />
  );
};

// Strategic ad placements
export const DashboardAd: React.FC = () => (
  <AdBanner 
    slot="1234567890" 
    style={{ minHeight: '90px', marginBottom: '20px' }}
  />
);

export const TransactionListAd: React.FC = () => (
  <AdBanner 
    slot="0987654321"
    format="fluid"
    style={{ marginTop: '20px', marginBottom: '20px' }}
  />
);
```

### 5.3 Pricing Page
- [ ] Create pricing page:
```typescript
// frontend/src/pages/PricingPage.tsx
export const PricingPage: React.FC = () => {
  const plans = [
    {
      name: 'Free',
      price: 0,
      priceId: null,
      features: [
        '2-3 automation rules',
        '2 linked accounts',
        'Basic budgeting',
        'Transaction tracking',
        'Community support',
        'Ads supported'
      ],
      limitations: [
        'Limited rule executions',
        'No API access',
        'No AI insights'
      ]
    },
    {
      name: 'Premium',
      price: 9.99,
      priceId: 'price_premium_monthly',
      features: [
        'Unlimited automation rules',
        '10 linked accounts',
        'Advanced budgeting',
        'Priority rule execution',
        'Email support',
        'No advertisements',
        'API access',
        'Data export'
      ],
      popular: true
    },
    {
      name: 'Premium Plus',
      price: 19.99,
      priceId: 'price_premium_plus_monthly',
      features: [
        'Everything in Premium',
        'Unlimited linked accounts',
        'AI-powered insights',
        'Financial advisor tools',
        'Priority support',
        'Custom integrations',
        'White-label options'
      ]
    }
  ];
  
  return (
    <div className="max-w-7xl mx-auto px-4 py-12">
      <div className="text-center mb-12">
        <h1 className="text-4xl font-bold mb-4">
          Choose Your BoostedBudget Plan
        </h1>
        <p className="text-lg text-gray-600">
          Start free, upgrade anytime. 14-day trial on all paid plans.
        </p>
      </div>
      
      <div className="grid md:grid-cols-3 gap-8">
        {plans.map(plan => (
          <PricingCard 
            key={plan.name}
            plan={plan}
            onSelect={() => handleSelectPlan(plan)}
          />
        ))}
      </div>
      
      <TrustBadges />
      <FAQSection />
    </div>
  );
};
```

### Phase 5 Deliverables
✅ **Monetization:**
- Stripe subscription billing
- 14-day free trial
- Google AdSense for free tier
- GDPR-compliant ad consent
- Subscription management portal

---

## Phase 6: Advanced Features & Optimization (Weeks 12-14)
*Deliverable: AI insights, mobile preparation, and performance optimization*

### 6.1 AI-Powered Insights
- [ ] Implement spending predictions:
```python
# backend/ml/insights_service.py
from fastapi import FastAPI
from sklearn.ensemble import RandomForestRegressor
import pandas as pd

app = FastAPI()

@app.post("/predict/spending")
async def predict_spending(user_id: str, month: str):
    # Load user transaction history
    transactions = load_user_transactions(user_id)
    
    # Feature engineering
    features = extract_spending_features(transactions)
    
    # Predict next month's spending by category
    predictions = model.predict(features)
    
    # Generate insights
    insights = {
        "predicted_total": float(predictions.sum()),
        "category_predictions": category_breakdown(predictions),
        "saving_opportunities": identify_savings(transactions, predictions),
        "anomalies": detect_anomalies(transactions),
        "recommendations": generate_recommendations(user_id, predictions)
    }
    
    return insights
```

### 6.2 Performance Optimization
- [ ] Implement caching strategy:
```go
// backend/internal/services/cache_service.go
package services

type CacheService struct {
    redis *redis.Client
    ttl   time.Duration
}

func (s *CacheService) GetOrSet(ctx context.Context, key string, 
    fetcher func() (interface{}, error)) (interface{}, error) {
    
    // Try cache first
    cached, err := s.redis.Get(ctx, key).Result()
    if err == nil {
        var result interface{}
        json.Unmarshal([]byte(cached), &result)
        return result, nil
    }
    
    // Fetch from source
    result, err := fetcher()
    if err != nil {
        return nil, err
    }
    
    // Cache result
    data, _ := json.Marshal(result)
    s.redis.Set(ctx, key, data, s.ttl)
    
    return result, nil
}
```

### 6.3 Mobile App Foundation
- [ ] Create React Native app structure:
```typescript
// mobile/src/App.tsx
import React from 'react';
import { NavigationContainer } from '@react-navigation/native';
import { Auth0Provider } from 'react-native-auth0';
import { Provider } from 'react-redux';
import { store } from './store';
import { AppNavigator } from './navigation';

export default function App() {
  return (
    <Auth0Provider
      domain={Config.AUTH0_DOMAIN}
      clientId={Config.AUTH0_CLIENT_ID}
    >
      <Provider store={store}>
        <NavigationContainer>
          <AppNavigator />
        </NavigationContainer>
      </Provider>
    </Auth0Provider>
  );
}
```

### Phase 6 Deliverables
✅ **Advanced Features:**
- AI spending predictions (Premium Plus)
- Performance optimizations
- Mobile app foundation
- Advanced analytics
- Export capabilities

---

## Deployment & Infrastructure

### Multi-Region Kubernetes Deployment
```yaml
# infrastructure/kubernetes/base/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: boostedbudget-backend
spec:
  replicas: 3
  selector:
    matchLabels:
      app: boostedbudget-backend
  template:
    metadata:
      labels:
        app: boostedbudget-backend
    spec:
      containers:
      - name: backend
        image: gcr.io/boostedbudget/backend:latest
        env:
        - name: AUTH0_DOMAIN
          valueFrom:
            secretKeyRef:
              name: auth0-secrets
              key: domain
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: db-secrets
              key: url
        - name: DATA_REGION
          valueFrom:
            fieldRef:
              fieldPath: metadata.annotations['node.kubernetes.io/region']
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

### GitHub Actions CI/CD
```yaml
# .github/workflows/deploy.yml
name: Deploy to Production

on:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - name: Run tests
      run: |
        docker-compose -f docker-compose.test.yml up --exit-code-from test
    
  deploy-us:
    needs: test
    runs-on: ubuntu-latest
    steps:
    - name: Deploy to US region
      run: |
        gcloud container clusters get-credentials boostedbudget-us --region us-east1
        kubectl apply -f infrastructure/kubernetes/us-east-1/
        kubectl rollout status deployment/boostedbudget-backend
  
  deploy-eu:
    needs: test
    runs-on: ubuntu-latest
    steps:
    - name: Deploy to EU region
      run: |
        gcloud container clusters get-credentials boostedbudget-eu --region europe-west1
        kubectl apply -f infrastructure/kubernetes/eu-west-1/
        kubectl rollout status deployment/boostedbudget-backend
```

---

## Success Metrics & KPIs

### Technical Metrics
- **Global Uptime:** >99.9%
- **API Response Time:** p50 < 100ms globally
- **Auth0 Login Success:** >99%
- **Plaid Sync Success:** >95%
- **Rule Execution Success:** >99%

### Business Metrics
- **Free to Paid Conversion:** 5-7%
- **Monthly Recurring Revenue:** $10K by month 6
- **User Retention:** >80% monthly
- **Global Users:** 60% US, 30% EU, 10% other
- **Ad Revenue per Free User:** $0.50-1.00/month

### Compliance Metrics
- **GDPR Requests Handled:** <72 hours
- **Data Deletion Completed:** <30 days
- **Consent Rate:** >70%
- **Security Incidents:** 0 critical

---

## Risk Mitigation

### Technical Risks
1. **Auth0 Outage**
   - Implement Auth0 tenant failover
   - Cache JWT validation
   - Graceful degradation

2. **Multi-Region Data Sync**
   - Use eventual consistency
   - Implement conflict resolution
   - Regular consistency checks

3. **Plaid API Changes**
   - Abstract Plaid integration
   - Version API clients
   - Monitor deprecations

### Compliance Risks
1. **GDPR Violations**
   - Regular compliance audits
   - Automated data retention
   - Clear consent tracking

2. **Financial Regulations**
   - Don't move money initially
   - Partner with compliant providers
   - Regular legal review

---

## Future Roadmap

### Phase 7: ACH Transfers (Requires Compliance)
- Money Transmitter Licenses
- Banking partnerships
- Enhanced security
- Plaid Transfer API implementation

### Phase 8: International Expansion
- Multi-currency support
- Local payment methods
- Regional compliance
- Localization

### Phase 9: Business Accounts
- Team budgets
- Expense management
- Corporate cards integration
- Invoice processing

### Phase 10: Open Banking
- Direct bank APIs
- PSD2 compliance (EU)
- Reduced Plaid dependency
- Lower costs

---

## Documentation & Support

### User Documentation
- Getting Started (by region)
- Video tutorials
- Rule templates library
- Privacy guide
- API documentation (Premium)

### Developer Documentation
- API reference (OpenAPI)
- Webhook guide
- Rate limits
- SDKs and examples

### Support Channels
- Help center (all users)
- Email support (Premium)
- Priority support (Premium Plus)
- Community forum

---

## Final Notes

This plan provides a complete roadmap for building BoostedBudget as a global, GDPR-compliant budgeting platform with intelligent automation. Key advantages of this approach:

1. **Auth0 Integration** significantly reduces authentication complexity and provides enterprise-grade security
2. **Global deployment from day one** with proper data residency
3. **GDPR compliance built-in** rather than bolted on
4. **Phased approach to financial features** - start with suggestions, add transfers later
5. **Clear monetization path** with free tier limitations and premium features

Each phase produces a deployable, valuable product increment. The architecture supports global scale while maintaining compliance with regional regulations.

**Remember:** 
- Always deploy after each phase
- Maintain GDPR compliance
- Start with transfer suggestions, not actual money movement
- Use Auth0's features to simplify authentication
- Monitor metrics from day one

---

*Document Version: 2.0.0*  
*Last Updated: [Current Date]*  
*Project: BoostedBudget - Global Financial Automation Platform*
