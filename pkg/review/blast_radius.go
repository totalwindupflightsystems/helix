package review

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// =============================================================================
// Blast radius — dependency impact map for human change management
// Spec: specs/plans/phase-5-6-review.md §6.1
// =============================================================================

// BlastPackage describes one package touched by a change or its dependents.
type BlastPackage struct {
	// ImportPath is the full module-relative import path (e.g. github.com/.../pkg/trust).
	ImportPath string `json:"import_path"`
	// Dir is the filesystem directory relative to the repo root when known.
	Dir string `json:"dir,omitempty"`
	// Role is "changed", "direct", or "transitive".
	Role string `json:"role"`
	// Depth is 0 for changed packages, 1 for direct importers, 2+ for transitive.
	Depth int `json:"depth"`
}

// BlastRadiusMap is the dependency impact of a set of changed files.
type BlastRadiusMap struct {
	ChangedFiles         []string       `json:"changed_files"`
	Packages             []BlastPackage `json:"packages"`
	Services             []string       `json:"services"`
	Teams                []string       `json:"teams,omitempty"`
	DirectDependents     []string       `json:"direct_dependents"`
	TransitiveDependents []string       `json:"transitive_dependents"`
	MaxDepth             int            `json:"max_depth"`
	ModulePath           string         `json:"module_path,omitempty"`
	FilesScanned         int            `json:"files_scanned"`
}

// BlastRadiusOptions configures blast radius computation.
type BlastRadiusOptions struct {
	// RepoRoot is the repository root containing go.mod (required for graph walk).
	RepoRoot string
	// ModulePath overrides module path detection from go.mod.
	ModulePath string
	// MaxDepth caps transitive BFS (default 8).
	MaxDepth int
	// TeamMap maps package path prefixes (under the module) to team names.
	// Example: "pkg/identity" → "platform-identity".
	TeamMap map[string]string
	// ServicePrefixes maps path segments that indicate a deployable service.
	// Default: cmd/, deploy/, services/.
	ServicePrefixes []string
}

// BuildBlastRadius walks the Go import graph under RepoRoot and computes the
// blast radius of changedFiles. Files outside the module (or non-Go files)
// still contribute as "changed" packages via their directory path.
//
// The graph is built with go/parser (stdlib only) — no network, no go list.
func BuildBlastRadius(changedFiles []string, opts BlastRadiusOptions) (*BlastRadiusMap, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 8
	}
	if len(opts.ServicePrefixes) == 0 {
		opts.ServicePrefixes = []string{"cmd/", "deploy/", "services/"}
	}

	out := &BlastRadiusMap{
		ChangedFiles: uniqueSorted(changedFiles),
	}

	if opts.RepoRoot == "" {
		// File-only mode: no graph — still classify packages/services from paths.
		return fileOnlyBlast(out, opts), nil
	}

	modulePath := opts.ModulePath
	if modulePath == "" {
		var err error
		modulePath, err = readModulePath(filepath.Join(opts.RepoRoot, "go.mod"))
		if err != nil {
			// Fall back to path-only classification when go.mod is missing.
			return fileOnlyBlast(out, opts), nil
		}
	}
	out.ModulePath = modulePath

	// reverse[importPath] = packages that import it
	reverse, dirOf, scanned, err := buildReverseImportGraph(opts.RepoRoot, modulePath)
	if err != nil {
		return nil, err
	}
	out.FilesScanned = scanned

	// Seeds: packages owning the changed files.
	seeds := map[string]string{} // importPath → role "changed"
	for _, f := range out.ChangedFiles {
		pkg := packageForFile(opts.RepoRoot, modulePath, f)
		if pkg == "" {
			continue
		}
		seeds[pkg] = "changed"
	}

	// BFS over reverse edges.
	type node struct {
		path  string
		depth int
		role  string
	}
	queue := make([]node, 0, len(seeds))
	seen := map[string]int{} // path → min depth
	for p := range seeds {
		queue = append(queue, node{path: p, depth: 0, role: "changed"})
		seen[p] = 0
	}

	var packages []BlastPackage
	var direct, transitive []string
	maxDepth := 0

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth > maxDepth {
			maxDepth = cur.depth
		}
		packages = append(packages, BlastPackage{
			ImportPath: cur.path,
			Dir:        dirOf[cur.path],
			Role:       cur.role,
			Depth:      cur.depth,
		})
		if cur.role == "direct" {
			direct = append(direct, cur.path)
		} else if cur.role == "transitive" {
			transitive = append(transitive, cur.path)
		}
		if cur.depth >= opts.MaxDepth {
			continue
		}
		for _, dep := range reverse[cur.path] {
			nextDepth := cur.depth + 1
			if prev, ok := seen[dep]; ok && prev <= nextDepth {
				continue
			}
			seen[dep] = nextDepth
			role := "direct"
			if nextDepth > 1 {
				role = "transitive"
			}
			queue = append(queue, node{path: dep, depth: nextDepth, role: role})
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Depth != packages[j].Depth {
			return packages[i].Depth < packages[j].Depth
		}
		return packages[i].ImportPath < packages[j].ImportPath
	})
	out.Packages = packages
	out.DirectDependents = uniqueSorted(direct)
	out.TransitiveDependents = uniqueSorted(transitive)
	out.MaxDepth = maxDepth
	out.Services = inferServices(out.ChangedFiles, packages, opts.ServicePrefixes)
	out.Teams = inferTeams(packages, opts.TeamMap, modulePath)
	return out, nil
}

// fileOnlyBlast classifies blast radius from file paths alone (no import graph).
func fileOnlyBlast(out *BlastRadiusMap, opts BlastRadiusOptions) *BlastRadiusMap {
	var packages []BlastPackage
	seen := map[string]bool{}
	for _, f := range out.ChangedFiles {
		dir := filepath.ToSlash(filepath.Dir(f))
		if dir == "." || dir == "" {
			dir = f
		}
		if seen[dir] {
			continue
		}
		seen[dir] = true
		packages = append(packages, BlastPackage{
			ImportPath: dir,
			Dir:        dir,
			Role:       "changed",
			Depth:      0,
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ImportPath < packages[j].ImportPath
	})
	out.Packages = packages
	out.Services = inferServices(out.ChangedFiles, packages, opts.ServicePrefixes)
	out.Teams = inferTeams(packages, opts.TeamMap, opts.ModulePath)
	return out
}

// buildReverseImportGraph scans all .go files under root and returns:
//
//	reverse[imported] = []importer
//	dirOf[importPath] = relative directory
func buildReverseImportGraph(root, modulePath string) (reverse map[string][]string, dirOf map[string]string, scanned int, err error) {
	reverse = map[string][]string{}
	dirOf = map[string]string{}
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			// Skip common non-source trees.
			if name == "vendor" || name == "node_modules" || name == ".git" ||
				name == "bin" || name == "testdata" || strings.HasPrefix(name, ".") {
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// Skip generated / embed noise.
		base := filepath.Base(path)
		if strings.HasPrefix(base, "zz_generated") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		pkgDir := filepath.ToSlash(filepath.Dir(rel))
		importPath := modulePath
		if pkgDir != "." {
			importPath = modulePath + "/" + pkgDir
		}
		dirOf[importPath] = pkgDir

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// Still count the package as present even if parse fails.
			scanned++
			return nil
		}
		scanned++
		for _, imp := range f.Imports {
			target := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(target, modulePath) {
				continue // external dep — not in blast radius of our code
			}
			reverse[target] = append(reverse[target], importPath)
		}
		return nil
	})
	if err != nil {
		return nil, nil, scanned, err
	}
	// Dedup reverse edges.
	for k, v := range reverse {
		reverse[k] = uniqueSorted(v)
	}
	return reverse, dirOf, scanned, nil
}

func packageForFile(root, modulePath, file string) string {
	// Normalize: strip root prefix if absolute.
	rel := filepath.ToSlash(file)
	if filepath.IsAbs(file) && root != "" {
		if r, err := filepath.Rel(root, file); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	rel = strings.TrimPrefix(rel, "./")
	if !strings.HasSuffix(rel, ".go") {
		// Non-Go: use directory as pseudo-package under module.
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			return modulePath
		}
		return modulePath + "/" + dir
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return modulePath
	}
	return modulePath + "/" + dir
}

func readModulePath(goModPath string) (string, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no module line in %s", goModPath)
}

func inferServices(files []string, packages []BlastPackage, prefixes []string) []string {
	set := map[string]bool{}
	consider := func(p string) {
		p = filepath.ToSlash(p)
		for _, pref := range prefixes {
			if strings.Contains(p, pref) {
				// Extract next path segment after prefix as service name.
				idx := strings.Index(p, pref)
				rest := p[idx+len(pref):]
				rest = strings.Trim(rest, "/")
				if rest == "" {
					continue
				}
				svc := strings.Split(rest, "/")[0]
				if svc != "" {
					set[svc] = true
				}
			}
		}
	}
	for _, f := range files {
		consider(f)
	}
	for _, pkg := range packages {
		consider(pkg.ImportPath)
		if pkg.Dir != "" {
			consider(pkg.Dir)
		}
	}
	return sortedKeysBool(set)
}

func inferTeams(packages []BlastPackage, teamMap map[string]string, modulePath string) []string {
	if len(teamMap) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, pkg := range packages {
		rel := strings.TrimPrefix(pkg.ImportPath, modulePath+"/")
		if rel == pkg.ImportPath {
			rel = pkg.Dir
		}
		// Longest-prefix match.
		bestLen := -1
		bestTeam := ""
		for prefix, team := range teamMap {
			p := strings.Trim(prefix, "/")
			if rel == p || strings.HasPrefix(rel, p+"/") {
				if len(p) > bestLen {
					bestLen = len(p)
					bestTeam = team
				}
			}
		}
		if bestTeam != "" {
			set[bestTeam] = true
		}
	}
	return sortedKeysBool(set)
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			set[s] = true
		}
	}
	return sortedKeysBool(set)
}

func sortedKeysBool(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
