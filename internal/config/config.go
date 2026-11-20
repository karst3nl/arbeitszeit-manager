// SPDX-License-Identifier: AGPL-3.0-only

// Package config holds the command line and environment configuration.
package config

import (
	"flag"
	"os"
)

// Config contains all runtime settings of the server.
type Config struct {
	Addr         string
	DBPath       string
	SecureCookie bool
}

// ParseArgs reads global flags and returns the remaining positional arguments
// so subcommands also work after global flags, e.g. "-db x admin create-user".
func ParseArgs(args []string) (Config, []string, error) {
	var cfg Config

	fs := flag.NewFlagSet("arbeitszeit-manager", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", env("ADDR", ":8080"), "listen address")
	fs.StringVar(&cfg.DBPath, "db", env("DB_PATH", "/var/lib/arbeitszeit-manager/app.db"), "path to SQLite database")
	fs.BoolVar(&cfg.SecureCookie, "secure-cookie", envBool("SECURE_COOKIE", false), "set the Secure attribute on cookies (HTTPS only)")

	if err := fs.Parse(args); err != nil {
		return Config{}, nil, err
	}
	return cfg, fs.Args(), nil
}

// Parse reads the configuration from flags with environment variable defaults.
func Parse(args []string) (Config, error) {
	cfg, _, err := ParseArgs(args)
	return cfg, err
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "TRUE" || v == "yes"
}
