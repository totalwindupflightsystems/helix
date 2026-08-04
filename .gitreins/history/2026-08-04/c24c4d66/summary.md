# Verdict: src-006

**Task:** Source CLI: helix source add/list/test/tools
**Evaluated:** 2026-08-04T11:48:31.158267
**Result:** ✓ PASS

## Pipeline Stages

- ✓ **tier1**
  -   ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 
- ✓ **tier2**
  - COMPLETE
  ✓ helix source add --name STRING --type [postgres|rest|local] --spec PATH is implemented in cmd/helix/source.go and wired into the cmd/helix/main.go switch via RunWithObs: cmd/helix/source.go runSourceAdd handles add with --name/--type/--spec flags (parseSourceFlags); main.go:489-495 case "source" calls RunWithObs("source", ...) -> runSourceWithDryRun -> runSource
  ✓ add validates the source (source.Validate) before writing; invalid type or missing required fields exit 2 and leave .helix/sources.yaml untouched: runSourceAdd calls src.Validate() before any write; on error returns sourceExitError(2); TestRunSourceAdd_ValidationFailureNoWrite asserts file not created
  ✓ add upserts into .helix/sources.yaml preserving existing sources and honors --dry-run (prints intent, writes nothing): runSourceAdd parses existing file, sets file.Sources[src.Name]=src (upsert), marshals+writes; --dry-run prints [DRY-RUN] and returns before writing; TestRunSourceAdd_CreatesAndUpserts and TestRunSourceAdd_DryRunNoWrite confirm
  ✓ helix source list [--enabled] prints a table sorted by name and filters by IsEnabled when --enabled is given (Source.Enabled *bool, missing = enabled): runSourceList sorts names via sort.Strings, prints NAME/TYPE/READ_ONLY/RATE_LIMIT/ALLOWED_AGENTS table, filters with s.IsEnabled() when f.enabled; Source.Enabled *bool with IsEnabled() returning true when nil (pkg/source/config.go:60); tests TestRunSourceList_SortedByName/EnabledFilter pass
  ✓ helix source test --name validates config, checks OpenAPI spec presence, runs type-specific probes (local root dir, REST reachability, postgres TCP dial); any failed check exits 2; Muster unreachable is a warning, not a failure: runSourceTest validates, checks OpenAPI via os.Stat (skips remote URLs), runs local os.Stat dir check, probeREST (HTTP HEAD/GET), probeTCP (net.DialTimeout); failed>0 returns sourceExitError(2); Muster health failure prints warning only and does not increment failed
  ✓ helix source tools --name generates tools via MusterBridge.GenerateToolsFromSource and prints them sorted by name; missing source or Muster unreachable exits 2 with a clear error: runSourceTools calls bridge.GenerateToolsFromSource(ctx,&src), sorts tools by Name, prints; missing source returns sourceExitError(2) with 'source tools: source %q not found'; Muster unreachable returns sourceExitError(2) with 'source tools:' error; tests TestRunSourceTools_SourceNotFound/MusterUnreachable pass
  ✓ unit tests in cmd/helix/source_test.go cover flag parsing, add validation/no-write, upsert, list sorting + enabled filter, test success/failure, tools error paths; go build ./... && go vet ./... && go test -short -count=1 ./... all pass; gofmt clean on changed files: source_test.go covers TestParseSourceFlags, TestRunSourceAdd_* (validation/no-write/dry-run/upsert), TestRunSourceList_* (sorting/enabled filter), TestRunSourceTest_* (success/failure), TestRunSourceTools_* (error paths); go build ./... and go vet ./... pass with no output; go test -short -count=1 ./... all packages ok (cmd/helix ok 12.629s); gofmt -l on source.go and source_test.go returns empty
All 7 criteria verified: source add/list/test/tools CLI fully implemented in cmd/helix/source.go, wired into main.go via RunWithObs, with comprehensive passing unit tests and clean build/vet/gofmt.

## Summary

Judge Result: src-006

Stage tier1: PASS
    ✓ lsp: 
  ✓ trust_tier: ✓ Trust tier: no changed files to check — PASS

  ✓ lint: 
  ✓ build: 
  ✓ tests: 
  ✓ secrets: 

Stage tier2: PASS
  COMPLETE
  ✓ helix source add --name STRING --type [postgres|rest|local] --spec PATH is implemented in cmd/helix/source.go and wired into the cmd/helix/main.go switch via RunWithObs: cmd/helix/source.go runSourceAdd handles add with --name/--type/--spec flags (parseSourceFlags); main.go:489-495 case "source" calls RunWithObs("source", ...) -> runSourceWithDryRun -> runSource
  ✓ add validates the source (source.Validate) before writing; invalid type or missing required fields exit 2 and leave .helix/sources.yaml untouched: runSourceAdd calls src.Validate() before any write; on error returns sourceExitError(2); TestRunSourceAdd_ValidationFailureNoWrite asserts file not created
  ✓ add upserts into .helix/sources.yaml preserving existing sources and honors --dry-run (prints intent, writes nothing): runSourceAdd parses existing file, sets file.Sources[src.Name]=src (upsert), marshals+writes; --dry-run prints [DRY-RUN] and returns before writing; TestRunSourceAdd_CreatesAndUpserts and TestRunSourceAdd_DryRunNoWrite confirm
  ✓ helix source list [--enabled] prints a table sorted by name and filters by IsEnabled when --enabled is given (Source.Enabled *bool, missing = enabled): runSourceList sorts names via sort.Strings, prints NAME/TYPE/READ_ONLY/RATE_LIMIT/ALLOWED_AGENTS table, filters with s.IsEnabled() when f.enabled; Source.Enabled *bool with IsEnabled() returning true when nil (pkg/source/config.go:60); tests TestRunSourceList_SortedByName/EnabledFilter pass
  ✓ helix source test --name validates config, checks OpenAPI spec presence, runs type-specific probes (local root dir, REST reachability, postgres TCP dial); any failed check exits 2; Muster unreachable is a warning, not a failure: runSourceTest validates, checks OpenAPI via os.Stat (skips remote URLs), runs local os.Stat dir check, probeREST (HTTP HEAD/GET), probeTCP (net.DialTimeout); failed>0 returns sourceExitError(2); Muster health failure prints warning only and does not increment failed
  ✓ helix source tools --name generates tools via MusterBridge.GenerateToolsFromSource and prints them sorted by name; missing source or Muster unreachable exits 2 with a clear error: runSourceTools calls bridge.GenerateToolsFromSource(ctx,&src), sorts tools by Name, prints; missing source returns sourceExitError(2) with 'source tools: source %q not found'; Muster unreachable returns sourceExitError(2) with 'source tools:' error; tests TestRunSourceTools_SourceNotFound/MusterUnreachable pass
  ✓ unit tests in cmd/helix/source_test.go cover flag parsing, add validation/no-write, upsert, list sorting + enabled filter, test success/failure, tools error paths; go build ./... && go vet ./... && go test -short -count=1 ./... all pass; gofmt clean on changed files: source_test.go covers TestParseSourceFlags, TestRunSourceAdd_* (validation/no-write/dry-run/upsert), TestRunSourceList_* (sorting/enabled filter), TestRunSourceTest_* (success/failure), TestRunSourceTools_* (error paths); go build ./... and go vet ./... pass with no output; go test -short -count=1 ./... all packages ok (cmd/helix ok 12.629s); gofmt -l on source.go and source_test.go returns empty
All 7 criteria verified: source add/list/test/tools CLI fully implemented in cmd/helix/source.go, wired into main.go via RunWithObs, with comprehensive passing unit tests and clean build/vet/gofmt.

Overall: PASS ✓
