// Package audit implements the Merkle tree audit log for xpc.
// Audit logs are content-addressed, signed, and verifiable offline.
// (Renamed from "proof" to reserve that term for type-theoretic derivations.)
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pyrex41/cross-validate-/pkg/types"
)

// Proof is a Merkle tree of type-checking judgments.
type Proof struct {
	// Version of the proof format.
	Version int `json:"version"`

	// RootDigest is the Merkle root hash.
	RootDigest string `json:"rootDigest"`

	// Metadata about this proof.
	Metadata ProofMetadata `json:"metadata"`

	// Run summarizes the obligation framework run, including counts and IDs
	// of every obligation that was evaluated (satisfied + violated + unknown).
	// Committed to the Merkle root so that a proof attests to completeness,
	// not just the violations.
	Run *RunSummary `json:"run,omitempty"`

	// RuleSubtrees contains per-rule judgment subtrees.
	RuleSubtrees map[string]*RuleSubtree `json:"ruleSubtrees"`

	// ResourceSubtrees contains per-resource judgment subtrees.
	ResourceSubtrees map[string]*ResourceSubtree `json:"resourceSubtrees"`

	// Tree is the full Merkle tree (leaf hashes for verification).
	Tree []string `json:"tree"`
}

// RunSummary captures obligation framework totals and the full list of
// obligation IDs evaluated during a check run.
type RunSummary struct {
	TotalObligations int      `json:"totalObligations"`
	Satisfied        int      `json:"satisfied"`
	Violated         int      `json:"violated"`
	Unknown          int      `json:"unknown"`
	ObligationIDs    []string `json:"obligationIds,omitempty"`
}

// ProofMetadata holds the metadata block of a proof.
type ProofMetadata struct {
	// IRDigest is the content hash of the IR that was checked.
	IRDigest string `json:"irDigest"`

	// SnapshotDigest is the content hash of the snapshot used.
	SnapshotDigest string `json:"snapshotDigest"`

	// KernelVersion is the version of the type-checking kernel.
	KernelVersion string `json:"kernelVersion"`

	// RulesetVersion is the version of the rule set used.
	RulesetVersion string `json:"rulesetVersion"`

	// RulesetDigest is the content hash of the rule set.
	RulesetDigest string `json:"rulesetDigest"`

	// Timestamp when the proof was generated.
	Timestamp time.Time `json:"timestamp"`

	// SigningIdentity that signed this proof.
	SigningIdentity string `json:"signingIdentity,omitempty"`

	// Signature over the proof root.
	Signature string `json:"signature,omitempty"`

	// Repo is the source repository.
	Repo string `json:"repo,omitempty"`

	// Commit is the git commit SHA.
	Commit string `json:"commit,omitempty"`

	// Cluster is the cluster name this proof was checked against.
	Cluster string `json:"cluster,omitempty"`
}

// RuleSubtree contains all judgments for a single rule.
type RuleSubtree struct {
	RuleID   string     `json:"ruleId"`
	Digest   string     `json:"digest"`
	Judgments []Judgment `json:"judgments"`
}

// ResourceSubtree contains all judgments for a single resource.
type ResourceSubtree struct {
	ResourceKey string     `json:"resourceKey"` // kind/name
	Digest      string     `json:"digest"`
	Judgments    []Judgment `json:"judgments"`
}

// Judgment is a single type-checking judgment (ok or error).
type Judgment struct {
	Status   string `json:"status"` // "ok" or "error" or "warning"
	RuleID   string `json:"ruleId"`
	Resource string `json:"resource"`
	Message  string `json:"message,omitempty"`
	// ObligationID is the structured obligation ID (e.g. "XPC.B.comp-xrd-ref.xbucket-default").
	// Empty for diagnostics produced outside the obligation framework.
	ObligationID string `json:"obligationId,omitempty"`
	// Category is the obligation category letter (A–L), empty if no obligation provenance.
	Category string `json:"category,omitempty"`
	// Generator is the obligation generator name, empty if no obligation provenance.
	Generator string `json:"generator,omitempty"`
	Digest    string `json:"digest"`
}

// KernelVersion is the current kernel version.
const KernelVersion = "0.1.0"

// RulesetVersion is the current rule set version.
const RulesetVersion = "2026.04"

// Generate creates a proof from diagnostics, optional run summary, and metadata.
// When summary is non-nil, the obligation counts and IDs are committed to the
// Merkle root so the proof attests to run completeness, not just violations.
func Generate(diags []types.Diagnostic, summary *RunSummary, irDigest, snapshotDigest string) *Proof {
	p := &Proof{
		Version: 2,
		Metadata: ProofMetadata{
			IRDigest:       irDigest,
			SnapshotDigest: snapshotDigest,
			KernelVersion:  KernelVersion,
			RulesetVersion: RulesetVersion,
			RulesetDigest:  computeRulesetDigest(),
			Timestamp:      time.Now().UTC(),
		},
		Run:              summary,
		RuleSubtrees:     make(map[string]*RuleSubtree),
		ResourceSubtrees: make(map[string]*ResourceSubtree),
	}

	// Build judgments from diagnostics
	var judgments []Judgment
	for _, d := range diags {
		j := Judgment{
			RuleID:   d.Code,
			Resource: fmt.Sprintf("%s:%d", d.Source.File, d.Source.Line),
			Message:  d.Message,
		}
		if d.Obligation != nil {
			j.ObligationID = d.Obligation.ID
			j.Category = d.Obligation.Category
			j.Generator = d.Obligation.Generator
		}
		switch d.Severity {
		case types.SeverityError:
			j.Status = "error"
		case types.SeverityWarning:
			j.Status = "warning"
		default:
			j.Status = "ok"
		}
		j.Digest = hashJudgment(j)
		judgments = append(judgments, j)
	}

	// Group by rule
	ruleGroups := make(map[string][]Judgment)
	for _, j := range judgments {
		ruleGroups[j.RuleID] = append(ruleGroups[j.RuleID], j)
	}

	// All known rules get a subtree (even if empty = all ok)
	allRules := []string{"XPC001", "XPC002", "XPC003", "XPC004", "XPC005",
		"XPC006", "XPC007", "XPC008", "XPC009", "XPC010", "XPC011"}
	for _, ruleID := range allRules {
		js := ruleGroups[ruleID]
		st := &RuleSubtree{
			RuleID:    ruleID,
			Judgments: js,
		}
		st.Digest = hashRuleSubtree(st)
		p.RuleSubtrees[ruleID] = st
	}

	// Group by resource
	resGroups := make(map[string][]Judgment)
	for _, j := range judgments {
		resGroups[j.Resource] = append(resGroups[j.Resource], j)
	}
	for resKey, js := range resGroups {
		st := &ResourceSubtree{
			ResourceKey: resKey,
			Judgments:   js,
		}
		st.Digest = hashResourceSubtree(st)
		p.ResourceSubtrees[resKey] = st
	}

	// Build Merkle tree
	p.buildMerkleTree()

	return p
}

// buildMerkleTree constructs the Merkle tree and sets the root digest.
func (p *Proof) buildMerkleTree() {
	var leaves []string

	// Metadata leaf
	metaData, _ := json.Marshal(p.Metadata)
	leaves = append(leaves, hashBytes(metaData))

	// Run summary leaf (committed so the proof attests to completeness)
	if p.Run != nil {
		leaves = append(leaves, hashRunSummary(p.Run))
	}

	// Rule subtree leaves (sorted by rule ID)
	ruleIDs := make([]string, 0, len(p.RuleSubtrees))
	for id := range p.RuleSubtrees {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Strings(ruleIDs)
	for _, id := range ruleIDs {
		leaves = append(leaves, p.RuleSubtrees[id].Digest)
	}

	// Resource subtree leaves (sorted by key)
	resKeys := make([]string, 0, len(p.ResourceSubtrees))
	for key := range p.ResourceSubtrees {
		resKeys = append(resKeys, key)
	}
	sort.Strings(resKeys)
	for _, key := range resKeys {
		leaves = append(leaves, p.ResourceSubtrees[key].Digest)
	}

	p.Tree = leaves
	p.RootDigest = computeMerkleRoot(leaves)
}

// computeMerkleRoot computes the Merkle root from a list of leaf hashes.
func computeMerkleRoot(leaves []string) string {
	if len(leaves) == 0 {
		return hashBytes([]byte("empty"))
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Build tree bottom-up
	level := make([]string, len(leaves))
	copy(level, leaves)

	for len(level) > 1 {
		var next []string
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				combined := level[i] + level[i+1]
				next = append(next, hashBytes([]byte(combined)))
			} else {
				// Odd node: hash with itself
				combined := level[i] + level[i]
				next = append(next, hashBytes([]byte(combined)))
			}
		}
		level = next
	}

	return level[0]
}

// Save writes the proof to a file.
func (p *Proof) Save(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling proof: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadProof reads a proof from a file.
func LoadProof(path string) (*Proof, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading proof: %w", err)
	}
	var p Proof
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("unmarshaling proof: %w", err)
	}
	return &p, nil
}

// Verify checks that the proof's Merkle root matches its content.
func (p *Proof) Verify() bool {
	saved := p.RootDigest
	p.buildMerkleTree()
	return p.RootDigest == saved
}

// VerifyInclusion checks that a specific judgment is included in the proof.
func (p *Proof) VerifyInclusion(ruleID, resource string) bool {
	st, ok := p.RuleSubtrees[ruleID]
	if !ok {
		return false
	}
	for _, j := range st.Judgments {
		if j.Resource == resource {
			return true
		}
	}
	return false
}

// Summary returns a human-readable summary of the proof.
func (p *Proof) Summary() string {
	var sb strings.Builder

	total := 0
	errors := 0
	warnings := 0
	ok := 0

	for _, st := range p.RuleSubtrees {
		for _, j := range st.Judgments {
			total++
			switch j.Status {
			case "error":
				errors++
			case "warning":
				warnings++
			case "ok":
				ok++
			}
		}
	}

	sb.WriteString(fmt.Sprintf("Proof: %s\n", truncDigest(p.RootDigest)))
	sb.WriteString(fmt.Sprintf("  IR:       %s\n", p.Metadata.IRDigest))
	sb.WriteString(fmt.Sprintf("  Snapshot: %s\n", p.Metadata.SnapshotDigest))
	sb.WriteString(fmt.Sprintf("  Kernel:   %s\n", p.Metadata.KernelVersion))
	sb.WriteString(fmt.Sprintf("  Ruleset:  %s (%s)\n", p.Metadata.RulesetVersion, truncDigest(p.Metadata.RulesetDigest)))
	sb.WriteString(fmt.Sprintf("  Time:     %s\n", p.Metadata.Timestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("  Judgments: %d total (%d errors, %d warnings, %d ok)\n",
		total, errors, warnings, ok))

	if errors > 0 {
		sb.WriteString("\n  Errors:\n")
		for _, st := range p.RuleSubtrees {
			for _, j := range st.Judgments {
				if j.Status == "error" {
					sb.WriteString(fmt.Sprintf("    %s %s: %s\n", j.RuleID, j.Resource, j.Message))
				}
			}
		}
	}

	return sb.String()
}

// DiffProofs produces a structured diff between two proofs.
func DiffProofs(a, b *Proof) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Proof diff: %s → %s\n", truncDigest(a.RootDigest), truncDigest(b.RootDigest)))

	if a.Metadata.SnapshotDigest != b.Metadata.SnapshotDigest {
		sb.WriteString(fmt.Sprintf("  Snapshot changed: %s → %s\n",
			truncDigest(a.Metadata.SnapshotDigest), truncDigest(b.Metadata.SnapshotDigest)))
	}

	// Compare rule subtrees
	unchanged := 0
	newlySatisfied := 0
	newlyViolated := 0

	allRules := make(map[string]bool)
	for id := range a.RuleSubtrees {
		allRules[id] = true
	}
	for id := range b.RuleSubtrees {
		allRules[id] = true
	}

	ruleIDs := make([]string, 0, len(allRules))
	for id := range allRules {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Strings(ruleIDs)

	var changes []string
	for _, id := range ruleIDs {
		aST := a.RuleSubtrees[id]
		bST := b.RuleSubtrees[id]

		aErrors := countErrors(aST)
		bErrors := countErrors(bST)

		if aErrors == bErrors {
			unchanged++
			continue
		}

		if aErrors > 0 && bErrors == 0 {
			newlySatisfied++
			changes = append(changes, fmt.Sprintf("    %s: %d errors → 0 (newly satisfied)", id, aErrors))
		} else if aErrors == 0 && bErrors > 0 {
			newlyViolated++
			msgs := getErrorMessages(bST)
			changes = append(changes, fmt.Sprintf("    %s: 0 → %d errors (newly violated)\n      %s",
				id, bErrors, strings.Join(msgs, "\n      ")))
		} else {
			changes = append(changes, fmt.Sprintf("    %s: %d errors → %d errors", id, aErrors, bErrors))
		}
	}

	totalJudgments := len(ruleIDs)
	sb.WriteString(fmt.Sprintf("  %d judgments: %d unchanged, %d newly satisfied, %d violated\n",
		totalJudgments, unchanged, newlySatisfied, newlyViolated))

	if len(changes) > 0 {
		sb.WriteString("  Changes:\n")
		for _, c := range changes {
			sb.WriteString(c + "\n")
		}
	}

	return sb.String()
}

func countErrors(st *RuleSubtree) int {
	if st == nil {
		return 0
	}
	count := 0
	for _, j := range st.Judgments {
		if j.Status == "error" {
			count++
		}
	}
	return count
}

func getErrorMessages(st *RuleSubtree) []string {
	if st == nil {
		return nil
	}
	var msgs []string
	for _, j := range st.Judgments {
		if j.Status == "error" {
			msgs = append(msgs, fmt.Sprintf("%s: %s", j.Resource, j.Message))
		}
	}
	return msgs
}

// truncDigest safely truncates a digest string for display.
func truncDigest(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}

func hashJudgment(j Judgment) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		j.Status, j.RuleID, j.Resource, j.Message,
		j.ObligationID, j.Category, j.Generator)
	return hashBytes([]byte(data))
}

func hashRunSummary(s *RunSummary) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%d|%d|", s.TotalObligations, s.Satisfied, s.Violated, s.Unknown)
	ids := make([]string, len(s.ObligationIDs))
	copy(ids, s.ObligationIDs)
	sort.Strings(ids)
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte("|"))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func hashRuleSubtree(st *RuleSubtree) string {
	h := sha256.New()
	h.Write([]byte(st.RuleID))
	for _, j := range st.Judgments {
		h.Write([]byte(j.Digest))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func hashResourceSubtree(st *ResourceSubtree) string {
	h := sha256.New()
	h.Write([]byte(st.ResourceKey))
	for _, j := range st.Judgments {
		h.Write([]byte(j.Digest))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func computeRulesetDigest() string {
	// Hash of all rule IDs + versions — in v1, rule content is fixed
	data := "XPC001:1.0|XPC002:1.0|XPC003:1.0|XPC004:1.0|XPC005:1.0|" +
		"XPC006:1.0|XPC007:1.0|XPC008:1.0|XPC009:1.0|XPC010:1.0|XPC011:1.0"
	return hashBytes([]byte(data))
}
