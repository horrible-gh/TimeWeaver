// TimeWeaver serves its browser UI, dashboard and agent API in one process.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/horrible-gh/TimeWeaver/internal/config"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

var version = "dev"
var commit = "unknown"

func run(args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(diagnostics, "Usage: timeweaver serve --env FILE | setup --env FILE --allow-write | version | check [--database] --env FILE | assets-check [--context PATH] | migrate status|up --env FILE | schema status|repair --env FILE | access status|init --env FILE")
		return 2
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(diagnostics, "version takes no arguments")
			return 2
		}
		fmt.Fprintf(out, "TimeWeaver %s (%s); unified server\n", version, commit)
		return 0
	case "access":
		return runAccess(args[1:], out, diagnostics)
	case "schema":
		return runDatabaseCommand("schema", args[1:], out, diagnostics)
	case "migrate":
		return runMigrate(args[1:], out, diagnostics)
	case "setup":
		return runSetup(args[1:], out, diagnostics)
	case "serve":
		return runServe(args[1:], out, diagnostics)
	case "check", "assets-check":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(diagnostics)
		var filename, context string
		var inspectDatabase bool
		if args[0] == "check" {
			flags.StringVar(&filename, "env", "server/.env", "explicit legacy environment file")
			flags.BoolVar(&inspectDatabase, "database", false, "also inspect database and server readiness without changing schema or data")
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
		return writeCheckReport(settings, inspectDatabase, out, diagnostics)
	default:
		fmt.Fprintln(diagnostics, "unknown command")
		return 2
	}
}
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
