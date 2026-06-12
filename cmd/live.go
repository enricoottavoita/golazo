package cmd

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/0xjuanma/golazo/internal/api"
	"github.com/0xjuanma/golazo/internal/data"
	"github.com/0xjuanma/golazo/internal/fotmob"
	"github.com/spf13/cobra"
)

// Common per-subcommand flag values. Each subcommand declares its own copies
// so the TUI's rootCmd flag set stays untouched.
type cliFlags struct {
	mock    bool
	debug   bool
	timeout time.Duration
	pretty  bool
}

// addCommonCLIFlags registers --mock, --debug, --timeout, --pretty on a subcmd.
func addCommonCLIFlags(cmd *cobra.Command, f *cliFlags) {
	cmd.Flags().BoolVar(&f.mock, "mock", false, "Use mock data instead of real API")
	cmd.Flags().BoolVar(&f.debug, "debug", false, "Emit debug logs to stderr")
	cmd.Flags().DurationVar(&f.timeout, "timeout", 15*time.Second, "Overall request timeout")
	cmd.Flags().BoolVar(&f.pretty, "pretty", false, "Indent JSON output")
}

// applyPretty syncs the package-level Pretty toggle. Subcommands call this
// from RunE before emitting output. GOLAZO_AGENT forces compact regardless.
func applyPretty(f cliFlags) {
	if agentMode() {
		Pretty = false
		return
	}
	Pretty = f.pretty
}

// liveFetcher abstracts the live-matches data source so runLive can be tested
// without spinning up an HTTP client. The default implementation calls into
// fotmob.Client; mock-mode callers bypass it entirely.
type liveFetcher func(ctx context.Context) ([]api.Match, error)

func defaultLiveFetcher(c *fotmob.Client) liveFetcher {
	return c.LiveMatches
}

// runLive is the testable core of the `live` subcommand. It writes the JSON
// envelope to stdout and any error envelope to stderr; returns the exit code.
func runLive(stdout, stderr io.Writer, flags cliFlags) int {
	applyPretty(flags)

	client, ctx, cancel, err := newHeadlessClient(runtimeOpts{
		mock:    flags.mock,
		debug:   flags.debug,
		timeout: flags.timeout,
	})
	defer cancel()
	if err == ErrOffline {
		return WriteError(stderr, ErrCodeOffline, err)
	}
	if err != nil {
		return WriteError(stderr, ErrCodeUpstreamError, err)
	}

	var matches []api.Match
	if flags.mock {
		matches = data.MockLiveMatches()
	} else {
		matches, err = defaultLiveFetcher(client)(ctx)
		if err != nil {
			return WriteError(stderr, ClassifyClientError(err, isTimeout(ctx)), err)
		}
	}

	SortMatches(matches)
	if err := WriteJSON(stdout, matches); err != nil {
		return WriteError(stderr, ErrCodeUpstreamError, err)
	}
	return ExitOK
}

var liveFlags cliFlags

var liveCmd = &cobra.Command{
	Use:           "live",
	Short:         "List live matches as JSON",
	Long:          "Fetches today's live matches for the active leagues and prints a JSON envelope to stdout.",
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		code := runLive(os.Stdout, os.Stderr, liveFlags)
		if code != ExitOK {
			os.Exit(code)
		}
	},
}

func init() {
	addCommonCLIFlags(liveCmd, &liveFlags)
	rootCmd.AddCommand(liveCmd)
}
