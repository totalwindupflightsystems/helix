package prompt

import (
	"fmt"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Provenance types
// ---------------------------------------------------------------------------

// ChainLink is one node in the provenance chain (spec §11).
type ChainLink struct {
	Stage  string
	Name   string
	Status string
	Detail string
	OK     bool
}

// ProvenanceChain is the full traceability path from commit to human intent.
type ProvenanceChain struct {
	CommitSHA string
	Links     []ChainLink
	Complete  bool
}

// WalkProvenance traces the full provenance chain for a commit:
// commit → prompt → spec → work item → intent (spec §11.1).
func WalkProvenance(commitSHA, attestHash, workDir string) (*ProvenanceChain, error) {
	chain := &ProvenanceChain{CommitSHA: commitSHA}

	// Step 1: Commit → Attestation
	if attestHash == "" {
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "commit",
			Name:   commitSHA,
			Status: "missing",
			Detail: "no attestation hash found in commit message",
			OK:     false,
		})
		return chain, nil
	}
	chain.Links = append(chain.Links, ChainLink{
		Stage:  "commit",
		Name:   commitSHA,
		Status: "parsed",
		Detail: fmt.Sprintf("attestation hash: %s", attestHash),
		OK:     true,
	})

	// Step 2: Attestation → Prompt
	pv, err := Lookup(attestHash)
	if err != nil {
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "prompt",
			Name:   attestHash,
			Status: "not_found",
			Detail: err.Error(),
			OK:     false,
		})
		return chain, nil
	}
	chain.Links = append(chain.Links, ChainLink{
		Stage:  "prompt",
		Name:   fmt.Sprintf("%s/%s", pv.Component, pv.Version),
		Status: string(pv.Status),
		Detail: fmt.Sprintf("hash: %s", pv.Hash),
		OK:     true,
	})

	// Step 3: Prompt → Spec
	meta, _ := readMetadata(pv.MetadataPath)
	if meta != nil && meta.SpecRef != "" {
		specPath := filepath.Join(workDir, meta.SpecRef)
		if _, err := os.Stat(specPath); err == nil {
			chain.Links = append(chain.Links, ChainLink{
				Stage:  "spec",
				Name:   meta.SpecRef,
				Status: "found",
				Detail: fmt.Sprintf("spec version: %s", meta.SpecVersion),
				OK:     true,
			})
		} else {
			chain.Links = append(chain.Links, ChainLink{
				Stage:  "spec",
				Name:   meta.SpecRef,
				Status: "missing",
				Detail: "spec file not found on disk",
				OK:     false,
			})
		}
	} else {
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "spec",
			Status: "missing",
			Detail: "no spec_ref in metadata",
			OK:     false,
		})
	}

	// Step 4: Spec → Work Item
	if meta != nil && meta.WorkItem != "" {
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "work_item",
			Name:   meta.WorkItem,
			Status: "referenced",
			Detail: meta.Changes,
			OK:     true,
		})

		// Step 5: Work Item → Intent
		intentOK := meta.Changes != ""
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "intent",
			Name:   meta.WorkItem,
			Status: "declared",
			Detail: meta.Changes,
			OK:     intentOK,
		})
	} else {
		chain.Links = append(chain.Links, ChainLink{
			Stage:  "work_item",
			Status: "missing",
			Detail: "no work_item in metadata",
			OK:     false,
		})
	}

	// Determine completeness
	chain.Complete = true
	for _, link := range chain.Links {
		if !link.OK {
			chain.Complete = false
			break
		}
	}

	return chain, nil
}

// VerifyProvenance checks whether all links in a chain are OK. Returns the
// list of failure descriptions for any broken links.
func VerifyProvenance(chain *ProvenanceChain) (allOK bool, failures []string) {
	allOK = true
	for _, link := range chain.Links {
		if !link.OK {
			allOK = false
			failures = append(failures,
				fmt.Sprintf("%s: %s — %s", link.Stage, link.Status, link.Detail))
		}
	}
	return allOK, failures
}
