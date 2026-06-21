// Command helix-prompt is the Helix prompt registry CLI.
//
// It implements the prompt lifecycle system from specs/prompt-registry.md:
// prompt storage, SHA-256 content hashing, lifecycle state machine, commit
// attestation enforcement, PromptFoo CI integration, and provenance chain
// verification.
//
// Four subcommands:
//
//	helix-prompt register <component> <version>   register a new prompt
//	helix-prompt attest   <component> <version>   attest a prompt to a commit
//	helix-prompt verify   <commit-sha>            verify a commit's attestation
//	helix-prompt list                             list registered prompts
//
// Import constraint: stdlib + github.com/spf13/cobra + gopkg.in/yaml.v3 only.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/helix/pkg/prompt"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

type globalOptions struct {
	verbose bool
}

func newRootCmd() *cobra.Command {
	gOpts := &globalOptions{}
	root := &cobra.Command{
		Use:   "helix-prompt",
		Short: "Helix prompt registry",
		Long: `helix-prompt — Prompt Registry CLI

Manages prompt storage, versioning, content-addressed hashing, lifecycle state
machine, commit attestation, PromptFoo CI integration, and provenance chain
verification.

Subcommands:
  register <component> <version>   Register a new prompt version
  attest   <component> <version>   Attest a prompt to a commit
  verify   <commit-sha>            Verify a commit's attestation
  list                             List registered prompts

Run "helix-prompt <subcommand> --help" for per-command flags.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			prompt.Verbose = gOpts.verbose
		},
	}
	root.PersistentFlags().BoolVarP(&gOpts.verbose, "verbose", "v", false,
		"verbose output (log all registry operations)")
	root.AddCommand(
		newRegisterCmd(gOpts),
		newAttestCmd(gOpts),
		newVerifyCmd(gOpts),
		newListCmd(gOpts),
	)
	return root
}

// ---------------------------------------------------------------------------
// register subcommand
// ---------------------------------------------------------------------------

type registerOptions struct {
	*globalOptions
	promptFile string
	model      string
	provider   string
	specRef    string
	dryRun     bool
}

func newRegisterCmd(gOpts *globalOptions) *cobra.Command {
	opts := &registerOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "register <component> <version>",
		Short: "Register a new prompt version",
		Long: `Register a new prompt in the registry. Reads the prompt file, computes its
SHA-256 hash, creates the directory structure under prompts/<component>/<version>/,
writes prompt.md and metadata.yaml, and updates the index.

Use --dry-run to validate without writing.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRegister(opts, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&opts.promptFile, "prompt-file", "",
		"path to prompt.md (default: prompts/<component>/<version>/prompt.md)")
	cmd.Flags().StringVar(&opts.model, "model", "", "model that produced this prompt")
	cmd.Flags().StringVar(&opts.provider, "provider", "", "provider name")
	cmd.Flags().StringVar(&opts.specRef, "spec-ref", "", "link to spec file this prompt implements")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "validate without writing")
	return cmd
}

func runRegister(opts *registerOptions, component, version string) error {
	promptFile := opts.promptFile
	if promptFile == "" {
		promptFile = fmt.Sprintf("prompts/%s/%s/prompt.md", component, version)
	}

	regOpts := &prompt.RegisterOptions{
		DryRun: opts.dryRun,
	}

	pv, err := prompt.Register(component, version, promptFile,
		opts.model, opts.provider, opts.specRef, regOpts)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "PROMPT REGISTERED:")
	fmt.Fprintf(os.Stdout, "  Component:  %s\n", pv.Component)
	fmt.Fprintf(os.Stdout, "  Version:    %s\n", pv.Version)
	fmt.Fprintf(os.Stdout, "  Hash:       %s\n", pv.Hash)
	if pv.Model != "" {
		fmt.Fprintf(os.Stdout, "  Model:      %s", pv.Model)
		if pv.Provider != "" {
			fmt.Fprintf(os.Stdout, " (%s)", pv.Provider)
		}
		fmt.Fprintln(os.Stdout)
	}
	if opts.specRef != "" {
		fmt.Fprintf(os.Stdout, "  Spec:       %s\n", opts.specRef)
	}
	fmt.Fprintf(os.Stdout, "  Status:     %s\n", pv.Status)
	fmt.Fprintf(os.Stdout, "  Location:   prompts/%s/%s/\n", component, version)

	if opts.dryRun {
		os.Exit(prompt.ExitDryRun)
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Next steps:")
	fmt.Fprintf(os.Stdout, "  1. Review prompt: cat prompts/%s/%s/prompt.md\n", component, version)
	fmt.Fprintf(os.Stdout, "  2. Propose for review: helix-prompt list --component %s\n", component)
	fmt.Fprintln(os.Stdout, "  3. After review: attest with commit")
	return nil
}

// ---------------------------------------------------------------------------
// attest subcommand
// ---------------------------------------------------------------------------

type attestOptions struct {
	*globalOptions
	commitSHA string
	force     bool
}

func newAttestCmd(gOpts *globalOptions) *cobra.Command {
	opts := &attestOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "attest <component> <version>",
		Short: "Attest a prompt to a commit",
		Long: `Validate that a prompt can be attested and optionally link it to a commit.
Checks lifecycle status, hash integrity, and PromptFoo results.

Use --force to skip validation (emergency only).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAttest(opts, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&opts.commitSHA, "commit-sha", "HEAD",
		"the commit being attested (default: HEAD)")
	cmd.Flags().BoolVar(&opts.force, "force", false,
		"skip validation (emergency only)")
	return cmd
}

func runAttest(opts *attestOptions, component, version string) error {
	pv, err := prompt.LookupByComponent(component, version)
	if err != nil {
		return err
	}

	if opts.force {
		fmt.Fprintf(os.Stdout, "ATTESTED (forced): %s/%s hash=%s\n", component, version, pv.Hash)
		fmt.Fprintf(os.Stdout, "  WARNING: validation skipped (--force)\n")
		return nil
	}

	att := &prompt.Attestation{
		Hash:     pv.Hash,
		Model:    pv.Model,
		Provider: pv.Provider,
	}

	result, err := prompt.Attest(att, opts.commitSHA, ".")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "ATTESTATION RESULT: %s/%s\n", component, version)
	fmt.Fprintf(os.Stdout, "  Hash match:    %t\n", result.HashMatch)
	fmt.Fprintf(os.Stdout, "  Lifecycle OK:  %t (status: %s)\n", result.LifecycleOK, result.Status)
	fmt.Fprintf(os.Stdout, "  PromptFoo:     %t\n", result.PromptfooPass)
	if len(result.Errors) > 0 {
		fmt.Fprintln(os.Stdout, "  Issues:")
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stdout, "    - %s\n", e)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// verify subcommand
// ---------------------------------------------------------------------------

type verifyOptions struct {
	*globalOptions
	checkHash      bool
	checkLifecycle bool
	checkPromptfoo bool
	fullChain      bool
}

func newVerifyCmd(gOpts *globalOptions) *cobra.Command {
	opts := &verifyOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "verify <commit-sha>",
		Short: "Verify a commit's prompt attestation",
		Long: `Verify the prompt attestation on a commit. Checks hash integrity, lifecycle
status, PromptFoo results, and optionally the full provenance chain.

Use --full-chain to trace: commit → prompt → spec → work item → intent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.checkHash, "check-hash", false,
		"verify prompt hash matches content")
	cmd.Flags().BoolVar(&opts.checkLifecycle, "check-lifecycle", false,
		"verify prompt is in active or attested state")
	cmd.Flags().BoolVar(&opts.checkPromptfoo, "check-promptfoo", false,
		"verify PromptFoo tests passed")
	cmd.Flags().BoolVar(&opts.fullChain, "full-chain", false,
		"walk the full provenance chain")
	return cmd
}

func runVerify(opts *verifyOptions, commitSHA string) error {
	// Get commit attestation
	att, err := prompt.GetCommitAttestation(commitSHA, ".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[FAIL] Cannot get commit attestation: %v\n", err)
		return err
	}

	fmt.Fprintf(os.Stdout, "COMMIT:      %s\n", commitSHA)
	fmt.Fprintf(os.Stdout, "  PROMPT:    %s\n", att.Hash)

	// If no specific checks requested, run all
	runHash := opts.checkHash || (!opts.checkHash && !opts.checkLifecycle && !opts.checkPromptfoo && !opts.fullChain)
	runLifecycle := opts.checkLifecycle || runHash
	runPromptfoo := opts.checkPromptfoo || runHash

	// Validate attestation
	result, _ := prompt.ValidateAttestation(att, ".")

	if runHash {
		if result.HashMatch {
			fmt.Fprintf(os.Stdout, "    HASH:    verified\n")
		} else {
			fmt.Fprintf(os.Stdout, "    HASH:    MISMATCH\n")
		}
	}
	if runLifecycle {
		if result.LifecycleOK {
			fmt.Fprintf(os.Stdout, "    STATUS:  %s OK\n", result.Status)
		} else {
			fmt.Fprintf(os.Stdout, "    STATUS:  %s REJECTED\n", result.Status)
		}
	}
	if runPromptfoo {
		if result.PromptfooPass {
			fmt.Fprintf(os.Stdout, "    PROMPTFOO: pass\n")
		} else {
			fmt.Fprintf(os.Stdout, "    PROMPTFOO: FAIL\n")
		}
	}
	if att.SpecRef != "" {
		fmt.Fprintf(os.Stdout, "    SPEC:    %s\n", att.SpecRef)
	}

	// Full provenance chain
	if opts.fullChain {
		fmt.Fprintln(os.Stdout)
		chain, _ := prompt.WalkProvenance(commitSHA, att.Hash, ".")
		for _, link := range chain.Links {
			status := "OK"
			if !link.OK {
				status = "FAIL"
			}
			fmt.Fprintf(os.Stdout, "    %s: %s — %s\n",
				strings.ToUpper(link.Stage[:1])+link.Stage[1:], link.Name, status)
			if link.Detail != "" {
				fmt.Fprintf(os.Stdout, "      %s\n", link.Detail)
			}
		}
		fmt.Fprintln(os.Stdout)
		if chain.Complete {
			fmt.Fprintf(os.Stdout, "PROVENANCE CHAIN: COMPLETE\n")
		} else {
			fmt.Fprintf(os.Stdout, "PROVENANCE CHAIN: INCOMPLETE\n")
		}
	}

	// Print errors
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", e)
	}

	return nil
}

// ---------------------------------------------------------------------------
// list subcommand
// ---------------------------------------------------------------------------

type listOptions struct {
	*globalOptions
	component string
	status    string
	model     string
	format    string
}

func newListCmd(gOpts *globalOptions) *cobra.Command {
	opts := &listOptions{globalOptions: gOpts}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered prompts",
		Long: `List all prompts in the registry, optionally filtered by component, status,
or model.

Output formats:
  table  (default) human-readable table
  json   machine-readable JSON array`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(opts)
		},
	}
	cmd.Flags().StringVar(&opts.component, "component", "", "filter by component")
	cmd.Flags().StringVar(&opts.status, "status", "active", "filter by lifecycle status (default: active)")
	cmd.Flags().StringVar(&opts.model, "model", "", "filter by model")
	cmd.Flags().StringVar(&opts.format, "format", "table", "output format: table|json")
	return cmd
}

func runList(opts *listOptions) error {
	filter := prompt.ListFilter{
		Component: opts.component,
		Status:    prompt.LifecycleStatus(opts.status),
		Model:     opts.model,
	}

	versions, err := prompt.List(filter)
	if err != nil {
		return err
	}

	if opts.format == "json" {
		// Ensure non-nil slice for [] not null
		if versions == nil {
			versions = []*prompt.PromptVersion{}
		}
		data, err := json.MarshalIndent(versions, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// Table format
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tVERSION\tSTATUS\tMODEL\tHASH")
	for _, v := range versions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			v.Component, v.Version, v.Status, v.Model, shortHashDisplay(v.Hash))
	}
	w.Flush()
	return nil
}

// shortHashDisplay truncates a hash for table display.
func shortHashDisplay(hash string) string {
	if len(hash) > 20 {
		return hash[:20] + "..."
	}
	return hash
}
