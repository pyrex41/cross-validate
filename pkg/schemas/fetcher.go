// Package schemas provides schema fetching and caching for CRDs.
// For v1, it extracts schemas from the loaded documents themselves.
// Future versions will support live cluster discovery and OCI registry fetching.
package schemas

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is a local schema cache keyed by content digest.
type Cache struct {
	dir     string
	entries map[string][]byte
}

// NewCache creates a new schema cache. If dir is empty, uses ~/.cache/xpc/schemas.
func NewCache(dir string) (*Cache, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("finding home directory: %w", err)
		}
		dir = filepath.Join(home, ".cache", "xpc", "schemas")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}
	return &Cache{
		dir:     dir,
		entries: make(map[string][]byte),
	}, nil
}

// Get retrieves a cached schema by digest.
func (c *Cache) Get(digest string) (map[string]interface{}, bool) {
	// Check in-memory first
	if data, ok := c.entries[digest]; ok {
		var schema map[string]interface{}
		if err := json.Unmarshal(data, &schema); err == nil {
			return schema, true
		}
	}

	// Check on-disk
	path := c.path(digest)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	c.entries[digest] = data
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, false
	}
	return schema, true
}

// Put stores a schema in the cache.
func (c *Cache) Put(digest string, schema map[string]interface{}) error {
	data, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	c.entries[digest] = data
	return os.WriteFile(c.path(digest), data, 0o644)
}

// Digest computes the content digest for a schema.
func Digest(schema map[string]interface{}) string {
	data, _ := json.Marshal(schema)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:16])
}

func (c *Cache) path(digest string) string {
	// Replace sha256: prefix and use first 4 chars as subdirectory
	safe := strings.ReplaceAll(digest, ":", "-")
	return filepath.Join(c.dir, safe+".json")
}

// FieldType represents the type of a field in an OpenAPI schema.
type FieldType string

const (
	FieldTypeString  FieldType = "string"
	FieldTypeInteger FieldType = "integer"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeObject  FieldType = "object"
	FieldTypeArray   FieldType = "array"
	FieldTypeUnknown FieldType = "unknown"
)

// ResolveFieldType walks a JSONSchema-like map to find the type at a dotted path.
func ResolveFieldType(schema map[string]interface{}, fieldPath string) FieldType {
	parts := strings.Split(fieldPath, ".")
	current := schema

	for _, part := range parts {
		// Look for the field in properties
		props := getNestedMap(current, "properties")
		if props == nil {
			// Try openAPIV3Schema wrapper
			oapi := getNestedMap(current, "openAPIV3Schema")
			if oapi != nil {
				props = getNestedMap(oapi, "properties")
			}
		}
		if props == nil {
			return FieldTypeUnknown
		}

		field := getNestedMap(props, part)
		if field == nil {
			return FieldTypeUnknown
		}
		current = field
	}

	typ, ok := current["type"].(string)
	if !ok {
		return FieldTypeUnknown
	}
	return FieldType(typ)
}

// TypeAssignable checks whether a value of fromType can be assigned to a field of toType.
func TypeAssignable(from, to FieldType) bool {
	if from == to {
		return true
	}
	if from == FieldTypeUnknown || to == FieldTypeUnknown {
		return true // can't determine, allow
	}
	// integer is assignable to number
	if from == FieldTypeInteger && to == FieldTypeNumber {
		return true
	}
	return false
}

func getNestedMap(m map[string]interface{}, key string) map[string]interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	vm, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return vm
}
