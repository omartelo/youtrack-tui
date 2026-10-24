// Command youtrack-tui is a read-only terminal UI for browsing YouTrack issues.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/omartelo/youtrack-tui/internal/config"
	"github.com/omartelo/youtrack-tui/internal/ui"
	"github.com/omartelo/youtrack-tui/internal/update"
)

func main() {
	// `update` is the one subcommand, taken before the flag set so that it
	// does not have to be declared as a flag on the TUI.
	if len(os.Args) > 1 && os.Args[1] == "update" {
		if err := update.Run(context.Background(), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "youtrack-tui:", err)
			os.Exit(1)
		}
		return
	}

	path := flag.String("config", config.DefaultPath(), "path to config.yml")
	provider := flag.String("provider", "", "provider name (defaults to the first one)")
	scope := flag.String("q", "", "base query every filter and `s` query is narrowed by, e.g. \"#Logistica\"")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println("youtrack-tui", update.Version)
		return
	}

	if err := run(*path, *provider, *scope); err != nil {
		fmt.Fprintln(os.Stderr, "youtrack-tui:", err)
		os.Exit(1)
	}
}

func run(path, provider, scope string) error {
	// A missing config file is not an error: the TUI opens on its setup
	// screen and writes the file once the credentials check out. A file that
	// exists but is broken still fails loudly — overwriting someone's config
	// without asking is worse than an error message.
	cfg, err := config.Load(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	m, err := ui.New(cfg, provider, scope, path)
	if err != nil {
		return err
	}
	// No mouse options on purpose: with mouse tracking on, the terminal stops
	// handling Ctrl+Click on the OSC 8 links this app emits for attachments.
	_, err = tea.NewProgram(m).Run()
	return err
}
