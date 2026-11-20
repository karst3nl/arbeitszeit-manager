// SPDX-License-Identifier: AGPL-3.0-only

// Command server starts the web application or manages users.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/karst3nl/arbeitszeit-manager/internal/api"
	"github.com/karst3nl/arbeitszeit-manager/internal/auth"
	"github.com/karst3nl/arbeitszeit-manager/internal/config"
	"github.com/karst3nl/arbeitszeit-manager/internal/store"
)

var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[arbeitszeit] ")

	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "-version" || args[0] == "--version") {
		fmt.Println(version)
		return
	}

	cfg, rest, err := config.ParseArgs(args)
	if err != nil {
		log.Fatal(err)
	}
	if len(rest) > 0 {
		switch rest[0] {
		case "admin":
			if err := runAdmin(rest[1:], cfg.DBPath); err != nil {
				log.Fatal(err)
			}
			return
		case "backup":
			if err := runBackup(rest[1:], cfg.DBPath); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	recovered, err := st.RecoverTimers()
	if err != nil {
		log.Fatal(err)
	}
	if recovered > 0 {
		log.Printf("recovered %d interrupted timer segment(s)", recovered)
	}
	if err := st.PurgeExpiredSessions(); err != nil {
		log.Printf("purge sessions: %v", err)
	}

	// Keep a server-side heartbeat so an interrupted timer can be recovered
	// even when no browser currently has the order page open.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := st.TouchRunningTimers(); err != nil {
				log.Printf("timer heartbeat: %v", err)
			}
		}
	}()

	a := auth.New(st, cfg.SecureCookie)
	srv, err := api.New(st, a, version, log.Default())
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// systemd sends SIGTERM on restart/shutdown; let in-flight requests finish.
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("listening on %s (db: %s, version: %s)", cfg.Addr, cfg.DBPath, version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Printf("stopped")
}

func runAdmin(args []string, defaultDB string) error {
	if len(args) == 0 || args[0] != "create-user" {
		return fmt.Errorf("usage: arbeitszeit-manager admin create-user -username NAME [options]")
	}

	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	username := fs.String("username", "", "login name (required)")
	displayName := fs.String("display-name", "", "display name")
	role := fs.String("role", "user", "role: user or admin")
	password := fs.String("password", "", "password (prompted if empty)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *username == "" {
		return fmt.Errorf("-username is required")
	}
	if *role != "user" && *role != "admin" {
		return fmt.Errorf("-role must be user or admin")
	}

	pw := *password
	if pw == "" {
		pw = os.Getenv("ADMIN_PASSWORD")
	}
	if pw == "" {
		fmt.Fprint(os.Stderr, "Passwort (Eingabe sichtbar): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err.Error() != "EOF" {
			return err
		}
		pw = strings.TrimRight(line, "\r\n")
		fmt.Fprintln(os.Stderr)
	}
	if len(pw) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	hash, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	id, err := st.CreateUser(*username, *displayName, hash, *role)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	fmt.Printf("Nutzer %q (ID %d, Rolle %s) angelegt.\n", *username, id, *role)
	return nil
}

func runBackup(args []string, defaultDB string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDB, "path to SQLite database")
	out := fs.String("out", "", "destination file for the snapshot (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-out is required")
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Backup(*out); err != nil {
		return err
	}
	fmt.Printf("Backup geschrieben: %s\n", *out)
	return nil
}
