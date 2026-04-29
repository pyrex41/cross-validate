// Package loader reads YAML manifests from the filesystem and parses them
// into typed structures for the IR builder.
package loader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/pyrex41/cross-validate-/pkg/types"
	"gopkg.in/yaml.v3"
)

// LoadedDocument is a single parsed YAML document with its source location.
type LoadedDocument struct {
	Source     types.SourceLocation
	APIVersion string
	Kind       string
	Raw        map[string]interface{}
	RawNode    *yaml.Node
}

// LoadDirectory reads all YAML files from a directory (recursively) and
// returns the parsed documents. Helm chart directories (detected by the
// presence of a Chart.yaml) have their templates/ subdirectory skipped —
// those files contain Go-template syntax that doesn't parse as YAML until
// helm has rendered it.
//
// Files are decoded in parallel across GOMAXPROCS workers; the returned
// slice is sorted by source path so diagnostics remain deterministic
// regardless of OS scheduling.
func LoadDirectory(dir string) ([]LoadedDocument, error) {
	// Phase 1: walk to collect file paths. Walk is single-threaded and
	// fast (it's mostly stat syscalls); parallelising the walk itself buys
	// little and complicates the templates/ skip rule.
	var paths []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "templates" {
				parent := filepath.Dir(path)
				if fi, err := os.Stat(filepath.Join(parent, "Chart.yaml")); err == nil && !fi.IsDir() {
					return filepath.SkipDir
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Phase 2: decode in parallel. Cap concurrency at GOMAXPROCS so a huge
	// directory doesn't oversubscribe the CPU; each worker pulls from a
	// shared index. The first error short-circuits via errOnce.
	workers := runtime.GOMAXPROCS(0)
	if workers > len(paths) {
		workers = len(paths)
	}
	if workers < 1 {
		workers = 1
	}
	results := make([][]LoadedDocument, len(paths))
	var (
		idx     int64
		idxMu   sync.Mutex
		errOnce sync.Once
		firstErr error
		wg      sync.WaitGroup
	)
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for {
				idxMu.Lock()
				i := int(idx)
				idx++
				idxMu.Unlock()
				if i >= len(paths) {
					return
				}
				fileDocs, err := LoadFile(paths[i])
				if err != nil {
					errOnce.Do(func() { firstErr = fmt.Errorf("loading %s: %w", paths[i], err) })
					return
				}
				results[i] = fileDocs
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	// Phase 3: flatten in path order. paths is already in walk order
	// (filepath.Walk returns lexical), so concatenating preserves it. Belt
	// + braces: explicitly sort by source path before flatten, which also
	// guards against future walk-order changes.
	type indexed struct {
		path string
		docs []LoadedDocument
	}
	all := make([]indexed, 0, len(paths))
	for i, p := range paths {
		if results[i] != nil {
			all = append(all, indexed{p, results[i]})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].path < all[j].path })

	total := 0
	for _, e := range all {
		total += len(e.docs)
	}
	docs := make([]LoadedDocument, 0, total)
	for _, e := range all {
		docs = append(docs, e.docs...)
	}
	return docs, nil
}

// LoadFile reads a single YAML file (which may contain multiple documents)
// and returns the parsed documents.
func LoadFile(path string) ([]LoadedDocument, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadReader(f, path)
}

// LoadReader reads YAML documents from a reader.
func LoadReader(r io.Reader, sourcePath string) ([]LoadedDocument, error) {
	var docs []LoadedDocument
	decoder := yaml.NewDecoder(r)

	for {
		var node yaml.Node
		err := decoder.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoding YAML in %s: %w", sourcePath, err)
		}

		if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
			continue
		}

		var raw map[string]interface{}
		if err := node.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decoding document in %s: %w", sourcePath, err)
		}

		apiVersion, _ := raw["apiVersion"].(string)
		kind, _ := raw["kind"].(string)

		if apiVersion == "" || kind == "" {
			continue // skip non-Kubernetes documents
		}

		doc := LoadedDocument{
			Source: types.SourceLocation{
				File:   sourcePath,
				Line:   node.Content[0].Line,
				Column: node.Content[0].Column,
			},
			APIVersion: apiVersion,
			Kind:       kind,
			Raw:        raw,
			RawNode:    &node,
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// LoadStdin reads YAML documents from stdin.
func LoadStdin() ([]LoadedDocument, error) {
	return LoadReader(bufio.NewReader(os.Stdin), "<stdin>")
}

// ClassifyDocument returns the category of a loaded document.
func ClassifyDocument(doc LoadedDocument) string {
	switch {
	case doc.Kind == types.KindCustomResourceDefinition:
		return "crd"
	case doc.Kind == types.KindCompositeResourceDefinition:
		return "xrd"
	case doc.Kind == types.KindComposition:
		return "composition"
	case doc.Kind == types.KindFunction && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "function"
	case doc.Kind == types.KindProvider && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "provider"
	case doc.Kind == types.KindConfiguration && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "configuration"
	case doc.Kind == types.KindApplication && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-application"
	case doc.Kind == "AppProject" && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-appproject"
	case doc.Kind == "ApplicationSet" && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-applicationset"
	default:
		return "resource"
	}
}
