package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/server"
	"github.com/horrible-gh/TimeWeaver/internal/store"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

const startupTimeout = 30 * time.Second

type readinessReport struct {
	Ready             bool                   `json:"ready"`
	Migrations        *store.MigrationReport `json:"migrations,omitempty"`
	Schema            *store.SchemaReport    `json:"schema,omitempty"`
	Access            *access.SchemaStatus   `json:"access,omitempty"`
	BootstrapPassword bool                   `json:"bootstrap_password"`
}

// inspectReadiness never changes data. The same checks run before binding the
// real listener and for check --database, so a configuration-only check cannot
// accidentally masquerade as a successful startup check.
func inspectReadiness(ctx context.Context, db *store.Store) (readinessReport, error) {
	var r readinessReport
	migrations, err := db.Status(ctx)
	r.Migrations = &migrations
	if err != nil {
		return r, err
	}
	if migrations.Blocked {
		return r, errors.New("database migration history is blocked; inspect 'timeweaver migrate status --env FILE' before changing the database")
	}
	if !migrations.HistoryExists || migrations.Pending != 0 {
		return r, errors.New("database migrations are incomplete; run 'timeweaver setup --env FILE --allow-write' after backing up and stopping the previous server")
	}
	schema, err := db.CriticalSchema(ctx)
	r.Schema = &schema
	if err != nil {
		return r, err
	}
	if len(schema.Issues) != 0 {
		return r, errors.New("critical database schema needs repair; inspect 'timeweaver schema status --env FILE' and run setup before serving")
	}
	human, err := access.Status(ctx, db)
	r.Access = &human
	if err != nil {
		return r, err
	}
	if !human.Ready {
		return r, errors.New("human access schema is not ready; run 'timeweaver setup --env FILE --allow-write' before serving")
	}
	r.BootstrapPassword, err = access.HasBootstrapPassword(ctx, db)
	if err != nil {
		return r, err
	}
	if r.BootstrapPassword {
		return r, errors.New("a known default password remains active; run 'timeweaver setup --env FILE --allow-write' to set the initial administrator password, and review any other default-password users")
	}
	r.Ready = true
	return r, nil
}

func writeCheckReport(settings config.Settings, inspectDatabase bool, out, diagnostics io.Writer) int {
	_, uiError := webui.Bundled(settings.Context)
	report := struct {
		Scope             string           `json:"scope"`
		ReadOnly          bool             `json:"read_only"`
		DatabaseConnected bool             `json:"database_connected"`
		ServerImplemented bool             `json:"server_implemented"`
		EmbeddedUIReady   bool             `json:"embedded_ui_ready"`
		Ready             *bool            `json:"ready,omitempty"`
		Database          *readinessReport `json:"database,omitempty"`
		Settings          config.Settings  `json:"settings"`
	}{Scope: "configuration-only", ReadOnly: true, ServerImplemented: true, EmbeddedUIReady: uiError == nil, Settings: settings}
	var readinessError error
	if inspectDatabase {
		report.Scope = "runtime-readiness"
		ready := false
		report.Ready = &ready
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()
		db, err := store.Open(ctx, settings.Database, true)
		if err != nil {
			readinessError = err
		} else {
			defer db.Close()
			report.DatabaseConnected = true
			state, err := inspectReadiness(ctx, db)
			report.Database = &state
			readinessError = err
			ready = state.Ready && uiError == nil
		}
		readinessError = errors.Join(readinessError, uiError)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(diagnostics, "cannot write configuration report")
		return 1
	}
	if readinessError != nil {
		fmt.Fprintln(diagnostics, readinessError)
		return 1
	}
	return 0
}

func runServe(args []string, out, diagnostics io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runServeContext(ctx, args, out, diagnostics)
}

func runServeContext(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	filename := flags.String("env", "server/.env", "environment file containing database, listen and public-origin settings")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(diagnostics, "unexpected positional arguments")
		return 2
	}
	settings, err := config.Load(*filename, nil)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	ui, err := webui.Bundled(settings.Context)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	startup, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	// The first pool is read-only and refuses a missing SQLite file. Serving
	// never initializes or migrates a database as a side effect of startup.
	probe, err := store.Open(startup, settings.Database, true)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer probe.Close()
	if _, err = inspectReadiness(startup, probe); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	db, err := store.OpenExisting(startup, settings.Database)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer db.Close()
	logger := slog.New(slog.NewTextHandler(diagnostics, nil))
	app, err := server.NewBrowserSurface(startup, db, probe, settings, ui, logger, access.HTTPOptions{Origin: settings.PublicOrigin})
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	address := net.JoinHostPort(settings.ListenHost, strconv.Itoa(settings.ListenPort))
	if ctx.Err() != nil {
		return 0
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(startup, "tcp", address)
	if err != nil {
		fmt.Fprintln(diagnostics, "cannot bind HTTP listener; check LISTEN_HOST, LISTEN_PORT and whether another server is running")
		return 1
	}
	cancel() // The request and worker lifetimes are owned by app.Serve below.
	if _, err = fmt.Fprintf(out, "TimeWeaver %s (%s) listening on %s; dashboard %s/; API %s\n", version, commit, listener.Addr(), settings.PublicOrigin, settings.Context); err != nil {
		listener.Close()
		fmt.Fprintln(diagnostics, "cannot write server startup report")
		return 1
	}
	if err = app.Serve(ctx, listener); err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return 0
		}
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	fmt.Fprintln(out, "TimeWeaver stopped")
	return 0
}
