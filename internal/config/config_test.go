// SPDX-License-Identifier: AGPL-3.0-only

package config

import "testing"

func TestParseArgsKeepsSubcommands(t *testing.T) {
	cfg, rest, err := ParseArgs([]string{"-db", "/tmp/test.db", "admin", "create-user", "-username", "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Fatalf("db path: %q", cfg.DBPath)
	}
	if len(rest) == 0 || rest[0] != "admin" || rest[1] != "create-user" {
		t.Fatalf("rest: %q", rest)
	}

	_, rest, err = ParseArgs([]string{"admin", "create-user", "-db", "/tmp/other.db"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) == 0 || rest[0] != "admin" {
		t.Fatalf("rest with subcommand first: %q", rest)
	}
}
