// The migration CLI intentionally cannot start an incomplete API server.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

var version = "dev-migration"
var commit = "unknown"

func run(args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(diagnostics, "Usage: timeweaver version | check --env FILE | assets-check [--context PATH] | migrate status|up --env FILE | schema status|repair --env FILE | access status|init --env FILE")
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(diagnostics, "version takes no arguments")
			return 2
		}
		fmt.Fprintf(out, "TimeWeaver %s (%s); migration build, not a production server\n", version, commit)
		return 0
	case "access":
		return runAccess(args[1:], out, diagnostics)
	case "schema":
		return runDatabaseCommand("schema", args[1:], out, diagnostics)
	case "migrate":
		return runMigrate(args[1:], out, diagnostics)
	case "serve":
		fmt.Fprintln(diagnostics, "serve is disabled: database, dashboard and agent API migration are not complete")
		return 2
	case "check", "assets-check":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(diagnostics)
		var filename, context string
		if args[0] == "check" {
			flags.StringVar(&filename, "env", "server/.env", "explicit legacy environment file")
		} else {
			flags.StringVar(&context, "context", "/time_weaver", "API context injected into UI")
		}
		if err := flags.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if flags.NArg() != 0 {
			fmt.Fprintln(diagnostics, "unexpected positional arguments")
			return 2
		}
		if args[0] == "assets-check" {
			if _, err := webui.Bundled(context); err != nil {
				fmt.Fprintln(diagnostics, err)
				return 1
			}
			fmt.Fprintln(out, "Embedded Vue entries and referenced local assets are valid; API readiness was not checked.")
			return 0
		}
		settings, err := config.Load(filename, nil)
		if err != nil {
			fmt.Fprintln(diagnostics, err)
			return 2
		}
		_, uiError := webui.Bundled(settings.Context)
		report := struct {
			Scope             string          `json:"scope"`
			ReadOnly          bool            `json:"read_only"`
			DatabaseConnected bool            `json:"database_connected"`
			ServerImplemented bool            `json:"server_implemented"`
			EmbeddedUIReady   bool            `json:"embedded_ui_ready"`
			Settings          config.Settings `json:"settings"`
		}{"configuration-only", true, false, false, uiError == nil, settings}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err = encoder.Encode(report); err != nil {
			fmt.Fprintln(diagnostics, "cannot write configuration report")
			return 1
		}
		return 0
	default:
		fmt.Fprintln(diagnostics, "unknown command")
		return 2
	}
}
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
