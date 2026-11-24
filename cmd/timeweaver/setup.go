package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"golang.org/x/term"
)

func runSetup(args []string, out, diagnostics io.Writer) int {
	return runSetupInput(args, os.Stdin, out, diagnostics)
}

// Setup is an explicit offline operation for installation and recovery.
// Routine serve upgrades are limited to reviewed incremental migrations;
// serve never initializes, repairs or resets the configured database.
func runSetupInput(args []string, in io.Reader, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	filename := flags.String("env", "server/.env", "environment file")
	allow := flags.Bool("allow-write", false, "confirm a backup and stopped legacy server")
	stdin := flags.Bool("admin-password-stdin", false, "read the initial administrator password from standard input")
	timeout := flags.Duration("timeout", 2*time.Minute, "database operation deadline")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || !*allow || *timeout <= 0 || *timeout > 30*time.Minute {
		fmt.Fprintln(diagnostics, "setup requires --allow-write after backup and stopping the old server; no database was opened")
		return 2
	}
	settings, err := config.Load(*filename, nil)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	var password string
	if *stdin {
		password, err = readSetupPassword(in, diagnostics, true)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 2
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	db, err := store.Open(ctx, settings.Database, false)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer db.Close()
	migration, err := db.Migrate(ctx)
	if err != nil {
		fmt.Fprintln(diagnostics, "database migration:", err)
		return 1
	}
	if migration.Blocked || migration.Pending != 0 {
		fmt.Fprintln(diagnostics, "database migrations remain incomplete; inspect migrate status before retrying")
		return 1
	}
	schema, err := db.RepairCriticalSchema(ctx)
	if err != nil {
		fmt.Fprintln(diagnostics, "database schema:", err)
		return 1
	}
	if len(schema.Issues) != 0 {
		fmt.Fprintln(diagnostics, "critical schema requires review; inspect schema status before retrying")
		return 1
	}
	auth, err := access.Initialize(ctx, db)
	if err != nil {
		fmt.Fprintln(diagnostics, "access schema:", err)
		return 1
	}
	if !auth.Ready {
		fmt.Fprintln(diagnostics, "access schema remains incomplete; inspect access status before retrying")
		return 1
	}
	needsPassword, err := access.HasBootstrapPassword(ctx, db)
	if err != nil {
		fmt.Fprintln(diagnostics, "administrator readiness:", err)
		return 1
	}
	initialized := false
	if needsPassword {
		if !*stdin {
			password, err = readSetupPassword(in, diagnostics, false)
			if err != nil {
				fmt.Fprintln(diagnostics, err)
				return 1
			}
		}
		// Time spent entering a password is not part of the database deadline.
		passwordCtx, passwordCancel := context.WithTimeout(context.Background(), *timeout)
		defer passwordCancel()
		if err = access.BootstrapAdministrator(passwordCtx, db, password); err != nil {
			fmt.Fprintln(diagnostics, "administrator setup:", err)
			return 1
		}
		if needsPassword, err = access.HasBootstrapPassword(passwordCtx, db); err != nil || needsPassword {
			fmt.Fprintln(diagnostics, "active accounts still use a published seed password; review those accounts before serving")
			return 1
		}
		initialized = true
	}
	report := struct {
		Ready                    bool   `json:"ready"`
		ReadOnly                 bool   `json:"read_only"`
		DatabaseType             string `json:"database_type"`
		AccessVersion            int    `json:"access_version"`
		AdministratorInitialized bool   `json:"administrator_initialized"`
	}{true, false, settings.Database.Type, auth.Version, initialized}
	if err = json.NewEncoder(out).Encode(report); err != nil {
		fmt.Fprintln(diagnostics, "cannot write setup report")
		return 1
	}
	return 0
}

func readSetupPassword(in io.Reader, diagnostics io.Writer, fromStdin bool) (string, error) {
	var password []byte
	if fromStdin {
		var err error
		password, err = io.ReadAll(io.LimitReader(in, 1027))
		if err != nil {
			return "", errors.New("cannot read administrator password from standard input")
		}
		defer clear(password)
		password = bytes.TrimSuffix(bytes.TrimSuffix(password, []byte("\n")), []byte("\r"))
	} else {
		file, ok := in.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			return "", errors.New("initial administrator password is required; run setup in a terminal or pass --admin-password-stdin with a secure pipe")
		}
		fmt.Fprint(diagnostics, "New password for administrator timeweaver (12+ characters): ")
		var err error
		password, err = term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(diagnostics)
		if err != nil {
			return "", errors.New("cannot read administrator password")
		}
		defer clear(password)
		fmt.Fprint(diagnostics, "Confirm administrator password: ")
		confirmation, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(diagnostics)
		defer clear(confirmation)
		if err != nil || string(confirmation) != string(password) {
			return "", errors.New("administrator passwords did not match")
		}
	}
	if !utf8.Valid(password) || utf8.RuneCount(password) < 12 || len(password) > 1024 || strings.ContainsAny(string(password), "\r\n\x00") {
		return "", errors.New("administrator password must be one line containing at least 12 characters and at most 1024 UTF-8 bytes")
	}
	return string(password), nil
}
