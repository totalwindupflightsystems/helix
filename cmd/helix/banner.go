// Command helix — banner.go
//
// `helix banner` prints the ASCII art HELIX banner. Two variants:
//
//	helix banner            full 7-line banner with version
//	helix banner --compact  3-line compact variant for tight terminals
//
// The banner is opt-in via the `banner` subcommand. A separate root-level
// `--banner` flag also exists (see main.go) so operators can prepend the
// banner to any other subcommand invocation:
//
//	helix --banner status
//	helix --banner version
//
// Output is plain ASCII (no box-drawing characters) so it survives
// copy-paste into chat / tickets / logs without rendering artifacts.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/totalwindupflightsystems/helix/pkg/banner"
)

type bannerFlags struct {
	compact bool
}

func parseBannerFlags(args []string, stdout, stderr io.Writer) (bannerFlags, bool, error) {
	fs := flag.NewFlagSet("helix-banner", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var f bannerFlags
	fs.BoolVar(&f.compact, "compact", false,
		"Print the compact 3-line banner instead of the full 7-line variant")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printBannerUsage(stdout)
			return f, true, nil
		}
		return f, false, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return f, false, fmt.Errorf("unexpected positional arguments: %v", rest)
	}
	return f, false, nil
}

// runBanner is the cmd/helix banner subcommand entry point.
func runBanner(args []string, stdout, stderr io.Writer) int {
	flags, showHelp, err := parseBannerFlags(args, stdout, stderr)
	if showHelp {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "helix banner: parse:", err)
		return 2
	}

	if flags.compact {
		fmt.Fprint(stdout, banner.RenderCompact(Version))
	} else {
		fmt.Fprint(stdout, banner.Render(Version))
	}
	return 0
}

// printBannerUsage renders the banner subcommand's help text.
func printBannerUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage: helix banner [flags]

Print the HELIX ASCII art startup banner.

Flags:
  --compact    Print the compact 3-line variant instead of the full 7-line banner

Examples:
  helix banner             # full banner
  helix banner --compact   # compact variant for tight terminals
  helix --banner version   # prepend banner to another subcommand`)
}
