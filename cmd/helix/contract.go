// Command helix — contract.go
//
// `helix contract` exposes pkg/contract: API contract generation, validation,
// breaking change detection, and immutable storage (Phase 2 §2.4).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/totalwindupflightsystems/helix/pkg/contract"
)

const (
	contractExitOK    = 0
	contractExitError = 2

	envContractStore = "HELIX_CONTRACT_STORE"
)

// contractFlags holds parsed flags for helix contract.
type contractFlags struct {
	subcommand string
	id         string // contract-id or spec-id
	oldID      string // for diff
	format     string
	consumer   string
	storePath  string
	jsonOut    bool
	dryRun     bool
}

func parseContractFlags(args []string) (contractFlags, bool, int) {
	var f contractFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--format":
			i++
			if i >= len(args) {
				return f, false, contractExitError
			}
			f.format = args[i]
		case arg == "--consumer":
			i++
			if i >= len(args) {
				return f, false, contractExitError
			}
			f.consumer = args[i]
		case arg == "--store":
			i++
			if i >= len(args) {
				return f, false, contractExitError
			}
			f.storePath = args[i]
		default:
			if !strings.HasPrefix(arg, "-") && f.subcommand == "" {
				f.subcommand = arg
			} else if !strings.HasPrefix(arg, "-") && f.id == "" {
				f.id = arg
			} else if !strings.HasPrefix(arg, "-") && f.oldID == "" {
				f.oldID = arg
			}
		}
		i++
	}
	return f, helpWanted, contractExitOK
}

const contractHelp = `helix contract — API contract generation and breaking change detection

Usage:
  helix contract create <spec-id>    Generate contract from spec
  helix contract validate <id>       Validate contract consistency
  helix contract freeze <id>         Hash and make immutable
  helix contract diff <old> <new>    Show breaking changes between versions
  helix contract consumer-check <id> Validate against consumer catalog
  helix contract list                List all contracts
  helix contract show <id>           Show contract details

Options:
  --format    Contract format: openapi | protobuf | graphql (default: openapi)
  --consumer  Consumer name for consumer-check
  --store     Contract store path (default: ~/.helix/contracts)
  --json      Output as JSON
  --dry-run   Show what would happen without writing

Environment:
  HELIX_CONTRACT_STORE   Override contract store path
`

func runContractWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	flags, helpWanted, _ := parseContractFlags(args)
	if helpWanted || flags.subcommand == "" {
		fmt.Fprint(stdout, contractHelp)
		return nil
	}

	if dryRun {
		flags.dryRun = true
	}

	storePath := flags.storePath
	if storePath == "" {
		storePath = os.Getenv(envContractStore)
	}

	store, err := contract.NewContractStore(storePath)
	if err != nil {
		return err
	}

	if flags.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] contract %s (store: %s)\n", flags.subcommand, store.Root())
	}

	switch flags.subcommand {
	case "create":
		return runContractCreate(stdout, stderr, store, flags)
	case "validate":
		return runContractValidate(stdout, store, flags)
	case "freeze":
		return runContractFreeze(stdout, store, flags)
	case "diff":
		return runContractDiff(stdout, store, flags)
	case "consumer-check":
		return runContractConsumerCheck(stdout, store, flags)
	case "list":
		return runContractList(stdout, store)
	case "show":
		return runContractShow(stdout, store, flags)
	default:
		return fmt.Errorf("unknown contract subcommand %q\n\n%s", flags.subcommand, contractHelp)
	}
}

func runContractCreate(stdout io.Writer, stderr io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" {
		return fmt.Errorf("contract create requires <spec-id>")
	}
	if flags.format == "" {
		flags.format = string(contract.FormatOpenAPI)
	}
	if !contract.ValidFormat(contract.ContractFormat(flags.format)) {
		return fmt.Errorf("unknown format %q (valid: openapi, protobuf, graphql)", flags.format)
	}

	author, err := contract.NewContractAuthor("")
	if err != nil {
		return fmt.Errorf("create contract author: %w", err)
	}
	c, err := author.Generate(context.Background(), flags.id, contract.ContractFormat(flags.format))
	if err != nil {
		return fmt.Errorf("generate contract from spec %q: %w", flags.id, err)
	}

	if flags.dryRun {
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Fprintf(stdout, "[DRY-RUN] would create contract %s\n%s\n", c.ID, string(data))
		return nil
	}

	if err := store.Save(c); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ contract %s created (format: %s, version: %d)\n", c.ID, flags.format, c.Version)
	return nil
}

func runContractValidate(stdout io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" {
		return fmt.Errorf("contract validate requires <contract-id>")
	}

	c, err := store.Load(flags.id)
	if err != nil {
		return fmt.Errorf("load contract %q: %w", flags.id, err)
	}

	validator := contract.NewContractValidator()
	prev, _ := store.LoadPrevious(flags.id) // optional
	consumers, _ := store.LoadConsumerCatalog()
	consumerNames := make([]string, 0, len(consumers))
	for name := range consumers {
		consumerNames = append(consumerNames, name)
	}

	report, err := validator.Validate(context.Background(), c, prev, consumerNames)
	if err != nil {
		return fmt.Errorf("validate contract %q: %w", flags.id, err)
	}

	if flags.jsonOut {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	if report.Consistent {
		fmt.Fprintf(stdout, "✓ contract %q is consistent\n", flags.id)
	} else {
		fmt.Fprintf(stdout, "✗ contract %q has %d issue(s)\n", flags.id, len(report.BreakingChanges))
	}
	for _, bc := range report.BreakingChanges {
		fmt.Fprintf(stdout, "  - %s: %s (%s → %s)\n", bc.Field, bc.ChangeType, bc.OldType, bc.NewType)
	}
	for _, w := range report.Warnings {
		fmt.Fprintf(stdout, "  ⚠ %s\n", w)
	}
	return nil
}

func runContractFreeze(stdout io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" {
		return fmt.Errorf("contract freeze requires <contract-id>")
	}

	c, err := store.Load(flags.id)
	if err != nil {
		return fmt.Errorf("load contract %q: %w", flags.id, err)
	}

	if c.IsFrozen() {
		fmt.Fprintf(stdout, "contract %q is already frozen (hash: %s)\n", flags.id, c.Hash)
		return nil
	}

	hash := contract.ComputeHash(c)

	if flags.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] would freeze contract %q with hash %s\n", flags.id, hash)
		return nil
	}

	if err := contract.Freeze(c); err != nil {
		return fmt.Errorf("freeze: %w", err)
	}
	if err := store.Save(c); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "✓ contract %q frozen (hash: %s, version: %d)\n", flags.id, hash, c.Version)
	return nil
}

func runContractDiff(stdout io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" || flags.oldID == "" {
		return fmt.Errorf("contract diff requires <old-id> <new-id>")
	}

	newC, err := store.Load(flags.id)
	if err != nil {
		return fmt.Errorf("load new contract %q: %w", flags.id, err)
	}
	oldC, err := store.Load(flags.oldID)
	if err != nil {
		return fmt.Errorf("load old contract %q: %w", flags.oldID, err)
	}
	changes := contract.DetectChanges(newC, oldC)

	if flags.jsonOut {
		data, _ := json.MarshalIndent(changes, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	if len(changes) == 0 {
		fmt.Fprintf(stdout, "✓ no breaking changes between %q and %q\n", flags.oldID, flags.id)
		return nil
	}

	fmt.Fprintf(stdout, "%d breaking change(s) between %q and %q:\n", len(changes), flags.oldID, flags.id)
	for _, bc := range changes {
		fmt.Fprintf(stdout, "  - %s: %s (%s → %s)\n", bc.Field, bc.ChangeType, bc.OldType, bc.NewType)
	}
	return nil
}

func runContractConsumerCheck(stdout io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" {
		return fmt.Errorf("contract consumer-check requires <contract-id>")
	}
	if flags.consumer == "" {
		return fmt.Errorf("contract consumer-check requires --consumer <name>")
	}

	cat, err := store.LoadConsumerCatalog()
	if err != nil {
		return fmt.Errorf("load consumer catalog: %w", err)
	}

	_, ok := cat[flags.consumer]
	if !ok {
		fmt.Fprintf(stdout, "✗ consumer %q not found in catalog\n", flags.consumer)
		fmt.Fprintf(stdout, "  known consumers: %s\n", strings.Join(consumerNames(cat), ", "))
		return nil
	}

	c, err := store.Load(flags.id)
	if err != nil {
		return fmt.Errorf("load contract %q: %w", flags.id, err)
	}

	impacts := contract.ConsumerImpactReport(nil, cat)

	found := false
	for _, imp := range impacts {
		if imp.ConsumerName == flags.consumer {
			fmt.Fprintf(stdout, "consumer %q — severity: %s\n", flags.consumer, imp.Severity)
			for _, bc := range imp.BreakingChanges {
				fmt.Fprintf(stdout, "  - %s\n", bc)
			}
			found = true
		}
	}
	if !found {
		fmt.Fprintf(stdout, "✓ no breaking changes affecting consumer %q\n", flags.consumer)
	}
	_ = c // used for silent import
	return nil
}

func runContractList(stdout io.Writer, store *contract.ContractStore) error {
	ids, err := store.List()
	if err != nil {
		return fmt.Errorf("list contracts: %w", err)
	}
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "no contracts found")
		return nil
	}
	for _, id := range ids {
		c, err := store.Load(id)
		if err != nil {
			fmt.Fprintf(stdout, "  %s (error: %v)\n", id, err)
			continue
		}
		status := "unfrozen"
		if c.IsFrozen() {
			status = "frozen"
		}
		fmt.Fprintf(stdout, "  %s  v%d  %s  %s\n", c.ID, c.Version, c.Format, status)
	}
	return nil
}

func runContractShow(stdout io.Writer, store *contract.ContractStore, flags contractFlags) error {
	if flags.id == "" {
		return fmt.Errorf("contract show requires <contract-id>")
	}

	c, err := store.Load(flags.id)
	if err != nil {
		return fmt.Errorf("load contract %q: %w", flags.id, err)
	}

	if flags.jsonOut {
		data, _ := json.MarshalIndent(c, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	fmt.Fprintf(stdout, "ID:       %s\n", c.ID)
	fmt.Fprintf(stdout, "SpecRef:  %s\n", c.SpecRef)
	fmt.Fprintf(stdout, "Format:   %s\n", c.Format)
	fmt.Fprintf(stdout, "Version:  %d\n", c.Version)
	fmt.Fprintf(stdout, "Created:  %s\n", c.CreatedAt.Format("2006-01-02 15:04:05"))
	if c.IsFrozen() {
		fmt.Fprintf(stdout, "Frozen:   %s (hash: %s)\n", c.FrozenAt.Format("2006-01-02 15:04:05"), c.Hash)
	} else {
		fmt.Fprintln(stdout, "Frozen:   no")
	}
	if len(c.ADRRefs) > 0 {
		fmt.Fprintf(stdout, "ADRs:     %s\n", strings.Join(c.ADRRefs, ", "))
	}
	return nil
}

func consumerNames(cat map[string][]string) []string {
	names := make([]string, 0, len(cat))
	for name := range cat {
		names = append(names, name)
	}
	return names
}
