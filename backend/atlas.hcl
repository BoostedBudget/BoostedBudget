// Atlas configuration for BoostedBudget database migrations
// Documentation: https://atlasgo.io/versioned/intro

// Local development environment
env "local" {
  src = "file://migrations"
  dev = "docker://postgres/15/test?search_path=public"

  migration {
    dir = "file://migrations"
  }

  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}

// Production environment (Fly.io)
env "production" {
  src = "file://migrations"

  migration {
    dir = "file://migrations"
  }
}

// Linting rules to catch errors before deployment
lint {
  // Warn about potentially risky changes
  review = WARNING

  // Error on destructive changes (DROP TABLE, DROP COLUMN, etc.)
  destructive {
    error = true
  }

  // Error on changes that depend on data (e.g., adding NOT NULL without default)
  data_depend {
    error = true
  }

  // Warn about backward incompatible changes
  incompatible {
    error = false
  }
}
