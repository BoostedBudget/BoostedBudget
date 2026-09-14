// Package disposabledb restricts destructive integration fixtures to known scratch databases.
package disposabledb

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errUnsafe = errors.New("integration fixture requires a known disposable loopback database without connection overrides")

// ParseURL accepts the local development scratch database and the CI scratch database.
// Validate both the explicit URL and pgx's effective configuration before connecting.
func ParseURL(raw string) (*pgxpool.Config, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.Fragment != "" || !loopback(u.Hostname()) {
		return nil, errUnsafe
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errUnsafe
	}
	for key, values := range query {
		if (key != "sslmode" && key != "connect_timeout") || len(values) != 1 {
			return nil, errUnsafe
		}
	}
	database := strings.TrimPrefix(u.Path, "/")
	if !knownIdentity(u.User.Username(), database) {
		return nil, errUnsafe
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		return nil, errUnsafe
	}
	if cfg.ConnConfig.Host != u.Hostname() || cfg.ConnConfig.User != u.User.Username() || cfg.ConnConfig.Database != database {
		return nil, errUnsafe
	}
	if err := validateConnection(cfg.ConnConfig); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateConnection(cfg *pgx.ConnConfig) error {
	if !loopback(cfg.Host) || !knownIdentity(cfg.User, cfg.Database) {
		return errUnsafe
	}
	for _, fallback := range cfg.Fallbacks {
		if fallback == nil || !loopback(fallback.Host) {
			return errUnsafe
		}
	}
	return nil
}

func knownIdentity(user, database string) bool {
	return (user == "devuser" && database == "freshcheck") || (user == "test" && database == "test")
}

func loopback(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}
