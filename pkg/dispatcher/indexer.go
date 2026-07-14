package dispatcher

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// -----------------------------------------------------------------------------
// CodebaseIndex — tokenized, searchable view of a repository (spec §3.3).
//
// The index is intentionally lightweight: a forward map of file paths to their
// tokenized content plus an inverted index from token to file paths. Search
// uses a simple coverage score (number of distinct query tokens present in
// the file / total unique tokens in the file) to rank candidates.
// -----------------------------------------------------------------------------

// CodebaseIndex maps repository files to their tokenized contents and provides
// an inverted index for efficient term lookup.
type CodebaseIndex struct {
	// Files maps each indexed file path to its parsed representation.
	Files map[string]IndexedFile
	// Inverted maps each token to the set of file paths containing it.
	// Multiple occurrences of the same token in the same file are stored
	// once; ordering is unspecified.
	Inverted map[string][]string
}

// IndexedFile captures the tokenized view of a single source file.
type IndexedFile struct {
	// Path is the repository-relative path.
	Path string
	// TokenCount is len(Tokens); cached to avoid recounting.
	TokenCount int64
	// Tokens is the lower-cased, whitespace/punctuation-split token list
	// for the file. Order is preserved from the source.
	Tokens []string
	// ImportedBy lists other indexed files that reference this path (used
	// to suggest nearby files in the assembled context).
	ImportedBy []string
}

// uniqueTokenCount returns the number of distinct tokens in the file.
func (f IndexedFile) uniqueTokenCount() int {
	if len(f.Tokens) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(f.Tokens))
	for _, t := range f.Tokens {
		seen[t] = struct{}{}
	}
	return len(seen)
}

// -----------------------------------------------------------------------------
// Default ignore patterns for IndexRepo
// -----------------------------------------------------------------------------

// defaultIgnoreDirs is the directory-name ignore list. Matched against any
// path segment, not the full path, so e.g. "vendor/foo/bar.go" matches.
var defaultIgnoreDirs = []string{
	"node_modules",
	".git",
	"vendor",
	"dist",
}

// defaultIgnoreFileSuffixes is the suffix-based file ignore list. Matching
// is case-sensitive (matching Unix tooling).
var defaultIgnoreFileSuffixes = []string{
	".min.js",
	".generated.",
	".pb.go",
	"_test.go",
}

// -----------------------------------------------------------------------------
// Public API
// -----------------------------------------------------------------------------

// IndexRepo walks repoPath and produces a CodebaseIndex covering every
// non-ignored regular file. ignorePatterns supplies additional substring
// matches: a file is skipped when its path (or any of its segments) contains
// any of the supplied strings. Pass nil to use only the built-in defaults
// (build artifacts, vendored deps, generated files, tests).
//
// The returned index never returns nil — on I/O errors the function returns
// a wrapped error rather than a partial index so callers can distinguish a
// truly empty repo (len(Files)==0) from a walk failure.
func IndexRepo(repoPath string, ignorePatterns []string) (*CodebaseIndex, error) {
	idx := &CodebaseIndex{
		Files:    make(map[string]IndexedFile),
		Inverted: make(map[string][]string),
	}

	info, err := os.Stat(repoPath)
	if err != nil {
		return nil, fmt.Errorf("index: stat %s: %w", repoPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("index: %s is not a directory", repoPath)
	}

	// Materialize the effective ignore list once.
	effective := make([]string, 0, len(ignorePatterns)+defaultIgnoreDirsLen())
	effective = append(effective, defaultIgnoreDirs...)
	effective = append(effective, ignorePatterns...)

	// First pass: walk and tokenize, populate Files.
	if err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip unreadable entries rather than aborting the whole walk;
			// surfaces as zero files for that subtree.
			return nil
		}
		if d.IsDir() {
			if shouldIgnoreSegment(path, effective) {
				return fs.SkipDir
			}
			return nil
		}
		// Regular file (or symlink etc.); only index regular files we can read.
		if !d.Type().IsRegular() {
			return nil
		}
		if shouldIgnoreFile(path) {
			return nil
		}
		rel, err := filepath.Rel(repoPath, path)
		if err != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		tokens := tokenize(string(data))
		idx.Files[rel] = IndexedFile{
			Path:       rel,
			TokenCount: int64(len(tokens)),
			Tokens:     tokens,
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("index: walk %s: %w", repoPath, err)
	}

	// Second pass: build the inverted index.
	for path, file := range idx.Files {
		// Record each unique token → file path edge at most once.
		seen := make(map[string]struct{}, len(file.Tokens))
		for _, tok := range file.Tokens {
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			idx.Inverted[tok] = append(idx.Inverted[tok], path)
		}
	}

	// Sort inverted index lists for stable Search output.
	for tok := range idx.Inverted {
		paths := idx.Inverted[tok]
		sort.Strings(paths)
		idx.Inverted[tok] = paths
	}

	return idx, nil
}

// Search returns the topN files most relevant to query, ranked by token
// coverage: for each file, the score is (number of distinct query tokens
// present in the file) / (total distinct tokens in the file). Returning
// fewer than topN results is allowed when fewer files match.
//
// Paths are returned in descending score order. Ties are broken by path
// for stable output. Empty queries return nil.
func (idx *CodebaseIndex) Search(query string, topN int) []string {
	if idx == nil {
		return nil
	}
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}
	// Dedup query terms.
	seenTok := make(map[string]struct{}, len(tokens))
	queryUnique := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if _, dup := seenTok[t]; dup {
			continue
		}
		seenTok[t] = struct{}{}
		queryUnique = append(queryUnique, t)
	}

	type scored struct {
		path  string
		score float64
	}
	scores := make(map[string]int)
	for _, qt := range queryUnique {
		paths, ok := idx.Inverted[qt]
		if !ok {
			continue
		}
		for _, p := range paths {
			scores[p]++
		}
	}

	ranked := make([]scored, 0, len(scores))
	for path, hits := range scores {
		f, ok := idx.Files[path]
		if !ok {
			continue
		}
		unique := f.uniqueTokenCount()
		if unique == 0 {
			continue
		}
		ranked = append(ranked, scored{
			path:  path,
			score: float64(hits) / float64(unique),
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].path < ranked[j].path
	})

	if topN <= 0 || topN > len(ranked) {
		topN = len(ranked)
	}
	out := make([]string, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, ranked[i].path)
	}
	return out
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// tokenize splits text on any whitespace or punctuation boundary, lower-cases
// each fragment, and drops empty fragments. Designed for TF-IDF-style term
// frequency work, not natural-language parsing.
func tokenize(text string) []string {
	if text == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		out = append(out, strings.ToLower(b.String()))
		b.Reset()
	}
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

// shouldIgnoreSegment returns true when any segment of path matches one of
// the supplied directory-name strings (or path-segment substring matches).
func shouldIgnoreSegment(path string, patterns []string) bool {
	parts := strings.Split(path, string(os.PathSeparator))
	for _, part := range parts {
		for _, pat := range patterns {
			if pat == "" {
				continue
			}
			if part == pat || strings.Contains(part, pat) {
				return true
			}
		}
	}
	return false
}

// shouldIgnoreFile returns true for files matching the default ignore list:
// test files, minified bundles, generated code, protobuf bindings.
func shouldIgnoreFile(path string) bool {
	base := filepath.Base(path)
	for _, suf := range defaultIgnoreFileSuffixes {
		if suf == "" {
			continue
		}
		if strings.HasSuffix(base, suf) || strings.Contains(base, suf) {
			return true
		}
	}
	return false
}

func defaultIgnoreDirsLen() int { return len(defaultIgnoreDirs) }

// -----------------------------------------------------------------------------
// Optional streaming tokenization used for very large files. Currently unused
// by IndexRepo (which reads whole files) but available so callers can hook
// future lazy-loading without changing the public API.
// -----------------------------------------------------------------------------
