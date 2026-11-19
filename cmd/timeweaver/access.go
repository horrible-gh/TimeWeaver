package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/store"
)

func runAccess(args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 || (args[0] != "status" && args[0] != "init") {
		fmt.Fprintln(diagnostics, "Usage: timeweaver access status|init --env FILE [init: --allow-write]")
		return 2
	}
	flags := flag.NewFlagSet("access "+args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	filename := flags.String("env", "server/.env", "explicit legacy environment file")
	allow := flags.Bool("allow-write", false, "confirm a backup and stopped legacy server")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || args[0] == "init" && !*allow || args[0] == "status" && *allow {
		fmt.Fprintln(diagnostics, "invalid arguments or missing --allow-write; no database opened")
		return 2
	}
	settings, err := config.Load(*filename, nil)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, settings.Database, args[0] == "status")
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer db.Close()
	var report access.SchemaStatus
	if args[0] == "init" {
		report, err = access.Initialize(ctx, db)
	} else {
		report, err = access.Status(ctx, db)
	}
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	if json.NewEncoder(out).Encode(report) != nil {
		fmt.Fprintln(diagnostics, "cannot write access report")
		return 1
	}
	if !report.Ready {
		return 1
	}
	return 0
}
