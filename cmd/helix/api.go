package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/totalwindupflightsystems/helix/pkg/api"
)

// ============================================================================
// helix api CLI — API contract HTTP server (spec §15)
// ============================================================================

const (
	apiExitOK    = 0
	apiExitError = 2
)

// apiFlags holds parsed CLI flags for the api subcommand.
type apiFlags struct {
	subcommand string // serve, contracts, validate
	addr       string
	body       string // --body for validate subcommand
	jsonOut    bool
	args       []string // remaining args for subcommands
}

// parseAPIFlags parses the args for `helix api`.
func parseAPIFlags(args []string) (apiFlags, bool, int) {
	var f apiFlags
	f.addr = ":9096"
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--json":
			f.jsonOut = true
		case arg == "--body":
			if i+1 < len(args) {
				f.body = args[i+1]
				i++
			} else {
				return f, false, apiExitError
			}
		case arg == "--addr":
			if i+1 < len(args) {
				f.addr = args[i+1]
				i++
			} else {
				return f, false, apiExitError
			}
		case strings.HasPrefix(arg, "--"):
			return f, false, apiExitError
		default:
			if f.subcommand == "" {
				f.subcommand = arg
			} else {
				f.args = append(f.args, arg)
			}
		}
		i++
	}

	if f.subcommand == "" {
		f.subcommand = "help"
	}

	return f, helpWanted, apiExitOK
}

// printAPIHelp prints the help text for the api subcommand.
func printAPIHelp(w io.Writer) {
	fmt.Fprintln(w, `helix api — API contract server (spec §15)

Usage:
  helix api serve [--addr :9096]
  helix api contracts [--json]
  helix api validate <service> <endpoint> --body '<json>'
  helix api services [--json]
  helix api help

Subcommands:
  serve      Start the HTTP contract server (blocks until Ctrl+C)
  contracts  List all service contracts (5 services)
  validate   Validate a request body against a service endpoint contract
  services   List all service IDs
  help       Show this help

Flags:
  --addr <addr>   Listen address for serve (default :9096)
  --body <json>   Request body JSON for validate subcommand
  --json          Structured JSON output (for contracts/services/validate)
  --help, -h      Show this help`)
}

// runAPI is the entry point for `helix api`.
func runAPI(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, rc := parseAPIFlags(args)
	if rc != apiExitOK {
		return rc
	}
	if helpWanted {
		printAPIHelp(stdout)
		return apiExitOK
	}

	switch flags.subcommand {
	case "help":
		printAPIHelp(stdout)
		return apiExitOK
	case "serve":
		return runAPIServe(flags, stdout, stderr)
	case "contracts":
		return runAPIContracts(flags, stdout, stderr)
	case "services":
		return runAPIServices(flags, stdout, stderr)
	case "validate":
		return runAPIValidate(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown subcommand %q\n", flags.subcommand)
		return apiExitError
	}
}

// runAPIServe starts the HTTP server.
func runAPIServe(flags apiFlags, stdout, stderr io.Writer) int {
	srv := api.NewContractServer()
	httpSrv := &http.Server{
		Addr:              flags.addr,
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Check if port is available
	ln, err := net.Listen("tcp", flags.addr)
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot listen on %s: %v\n", flags.addr, err)
		return apiExitError
	}

	fmt.Fprintf(stdout, "Helix API contract server listening on %s\n", flags.addr)
	fmt.Fprintf(stdout, "Endpoints:\n")
	fmt.Fprintf(stdout, "  GET  /api/v1/contracts            List all contracts\n")
	fmt.Fprintf(stdout, "  GET  /api/v1/contracts/{service}  Get one service contract\n")
	fmt.Fprintf(stdout, "  POST /api/v1/validate/{svc}/{ep}  Validate request body\n")
	fmt.Fprintf(stdout, "  GET  /api/v1/services             List service IDs\n")
	fmt.Fprintf(stdout, "  GET  /health                      Health check\n")
	fmt.Fprintf(stdout, "\nPress Ctrl+C to stop.\n")

	// Handle graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- httpSrv.Serve(ln)
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-done:
		if err != nil {
			fmt.Fprintf(stderr, "error: server stopped: %v\n", err)
			return apiExitError
		}
	case <-sigChan:
		fmt.Fprintf(stdout, "\nShutting down...\n")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}

	return apiExitOK
}

// runAPIContracts lists all service contracts.
func runAPIContracts(flags apiFlags, stdout, stderr io.Writer) int {
	services := api.AllServices()

	if flags.jsonOut {
		type endpointInfo struct {
			Name   string `json:"name"`
			Method string `json:"method"`
			Path   string `json:"path"`
		}
		type svcOut struct {
			ID        string         `json:"id"`
			Name      string         `json:"name"`
			BaseURL   string         `json:"base_url"`
			Endpoints []endpointInfo `json:"endpoints"`
		}

		var list []svcOut
		for _, svc := range services {
			endpoints := api.EndpointsForService(svc.ID)
			var eps []endpointInfo
			for _, ep := range endpoints {
				eps = append(eps, endpointInfo{
					Name:   ep.Name,
					Method: string(ep.Method),
					Path:   ep.Path,
				})
			}
			list = append(list, svcOut{
				ID:        string(svc.ID),
				Name:      svc.Name,
				BaseURL:   svc.BaseURL,
				Endpoints: eps,
			})
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return apiExitError
		}
		fmt.Fprintln(stdout, string(data))
		return apiExitOK
	}

	fmt.Fprintf(stdout, "Helix API Contracts (spec §15)\n\n")
	for _, svc := range services {
		fmt.Fprintf(stdout, "%s (%s)\n", svc.Name, svc.ID)
		fmt.Fprintf(stdout, "  Base URL: %s\n", svc.BaseURL)
		endpoints := api.EndpointsForService(svc.ID)
		for _, ep := range endpoints {
			fmt.Fprintf(stdout, "  %-6s %-45s  %s\n", ep.Method, ep.Path, ep.Name)
		}
		fmt.Fprintln(stdout)
	}

	return apiExitOK
}

// runAPIServices lists all service IDs.
func runAPIServices(flags apiFlags, stdout, stderr io.Writer) int {
	services := api.AllServices()

	if flags.jsonOut {
		type svcBrief struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var list []svcBrief
		for _, svc := range services {
			list = append(list, svcBrief{ID: string(svc.ID), Name: svc.Name})
		}
		data, err := json.MarshalIndent(list, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return apiExitError
		}
		fmt.Fprintln(stdout, string(data))
		return apiExitOK
	}

	for _, svc := range services {
		fmt.Fprintf(stdout, "%-20s %s\n", svc.ID, svc.Name)
	}

	return apiExitOK
}

// runAPIValidate validates a request body against a service endpoint contract.
func runAPIValidate(flags apiFlags, stdout, stderr io.Writer) int {
	if len(flags.args) < 2 {
		fmt.Fprintf(stderr, "error: validate requires <service> <endpoint>\n")
		return apiExitError
	}

	svcID := api.ServiceID(flags.args[0])
	endpointSlug := flags.args[1]

	// Find the endpoint
	endpoints := api.EndpointsForService(svcID)
	if endpoints == nil {
		fmt.Fprintf(stderr, "error: unknown service: %s\n", svcID)
		return apiExitError
	}

	var matched *api.EndpointDef
	for i := range endpoints {
		if slugifyAPI(endpoints[i].Name) == endpointSlug {
			matched = &endpoints[i]
			break
		}
	}
	if matched == nil {
		fmt.Fprintf(stderr, "error: unknown endpoint: %s/%s\n", svcID, endpointSlug)
		return apiExitError
	}

	// Get body from --body flag or stdin
	var body []byte
	if flags.body != "" {
		body = []byte(flags.body)
	} else {
		var err error
		body, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(stderr, "error: reading stdin: %v\n", err)
			return apiExitError
		}
	}

	// Use the contract validator
	validator := api.NewContractValidator()
	validateAPIEndpoint(validator, svcID, matched.Name, body)

	if flags.jsonOut {
		result := map[string]any{
			"service":     string(svcID),
			"endpoint":    matched.Name,
			"valid":       !validator.HasErrors(),
			"error_count": len(validator.Errors()),
		}
		if validator.HasErrors() {
			errList := make([]map[string]string, len(validator.Errors()))
			for i, e := range validator.Errors() {
				errList[i] = map[string]string{
					"field":   e.Field,
					"message": e.Message,
				}
			}
			result["errors"] = errList
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		if validator.HasErrors() {
			fmt.Fprintf(stdout, "INVALID — %d error(s) for %s/%s:\n", len(validator.Errors()), svcID, matched.Name)
			for _, e := range validator.Errors() {
				fmt.Fprintf(stdout, "  ✗ %s: %s\n", e.Field, e.Message)
			}
		} else {
			fmt.Fprintf(stdout, "VALID — %s/%s passes all contract checks\n", svcID, matched.Name)
		}
	}

	return apiExitOK
}

// runAPIWithDryRun wraps runAPI with the global --dry-run flag.
func runAPIWithDryRun(args []string, stdout, stderr io.Writer, globalDryRun bool) error {
	rc := runAPI(args, stdout, stderr)
	if rc != 0 {
		return errExit{code: rc}
	}
	return nil
}

// slugifyAPI converts an endpoint name to a URL-friendly slug.
func slugifyAPI(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "-")
}

// validateAPIEndpoint dispatches validation using the raw body bytes.
// This mirrors pkg/api.ContractServer.validateEndpoint but operates on raw bytes.
func validateAPIEndpoint(v *api.ContractValidator, svc api.ServiceID, endpointName string, body []byte) {
	v.ValidateFromJSON(svc, endpointName, body)
}
