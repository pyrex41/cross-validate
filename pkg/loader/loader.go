// Package loader reads YAML manifests from the filesystem and parses them
// into typed structures for the IR builder.
package loader

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
// returns the parsed documents.
func LoadDirectory(dir string) ([]LoadedDocument, error) {
	var docs []LoadedDocument

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		fileDocs, err := LoadFile(path)
		if err != nil {
			return fmt.Errorf("loading %s: %w", path, err)
		}
		docs = append(docs, fileDocs...)
		return nil
	})
	if err != nil {
		return nil, err
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
	case doc.Kind == "CustomResourceDefinition":
		return "crd"
	case doc.Kind == "CompositeResourceDefinition":
		return "xrd"
	case doc.Kind == "Composition":
		return "composition"
	case doc.Kind == "Function" && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "function"
	case doc.Kind == "Provider" && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "provider"
	case doc.Kind == "Configuration" && strings.HasPrefix(doc.APIVersion, "pkg.crossplane.io/"):
		return "configuration"
	case doc.Kind == "Application" && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-application"
	case doc.Kind == "AppProject" && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-appproject"
	case doc.Kind == "ApplicationSet" && strings.HasPrefix(doc.APIVersion, "argoproj.io/"):
		return "argo-applicationset"
	default:
		return "resource"
	}
}
