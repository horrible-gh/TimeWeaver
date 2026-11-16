package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func runMigrate(args []string, out, diagnostics io.Writer) int {
	return runDatabaseCommand("migrate", args, out, diagnostics)
}

func runDatabaseCommand(command string, args []string, out, diagnostics io.Writer) int {
	writeAction := "up"
	if command == "schema" {
		writeAction = "repair"
	}
	if len(args) == 0 || (args[0] != "status" && args[0] != writeAction) {
		fmt.Fprintln(diagnostics, "Usage: timeweaver "+command+" status|"+writeAction+" --env FILE [writes: --allow-write]")
		return 2
	}
	flags := flag.NewFlagSet(command+" "+args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	filename := flags.String("env", "server/.env", "explicit legacy environment file")
	timeout := flags.Duration("timeout", 2*time.Minute, "database operation deadline")
	allowWrite := false
	if args[0] == writeAction {
		flags.BoolVar(&allowWrite, "allow-write", false, "confirm a backup exists and the old server/migrator is stopped")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *timeout <= 0 || *timeout > 30*time.Minute {
		fmt.Fprintln(diagnostics, "unexpected arguments or invalid timeout")
		return 2
	}
	if args[0] == writeAction && !allowWrite {
		fmt.Fprintln(diagnostics, "write operation requires --allow-write after backup and stopping the old server; no database was opened")
		return 2
	}
	settings, err := config.Load(*filename, nil)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	if args[0] == writeAction && settings.Database.Type != "mysql" {
		fmt.Fprintln(diagnostics, store.ErrSQLiteIncomplete)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	db, err := store.Open(ctx, settings.Database, args[0] == "status")
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer db.Close()
	var report any
	blocked := false
	if command == "schema" {
		var r store.SchemaReport
		if args[0] == writeAction {
			r, err = db.RepairCriticalSchema(ctx)
		} else {
			r, err = db.CriticalSchema(ctx)
		}
		report, blocked = r, len(r.Issues) > 0
	} else {
		var r store.MigrationReport
		if args[0] == writeAction {
			r, err = db.Migrate(ctx)
		} else {
			r, err = db.Status(ctx)
		}
		report, blocked = r, r.Blocked
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		fmt.Fprintln(diagnostics, "cannot write migration report")
		return 1
	}
	if blocked {
		return 1
	}
	return 0
}
