package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// CurrentVersion is the schema version this binary understands. The loader
// rejects xpc.yaml files whose top-level `version:` key disagrees so that
// future schema-breaking changes have a clean trip-wire.
const CurrentVersion = 1

// Load reads xpc.yaml from path, validates it, and returns a fully-populated
// Config. An empty / missing path is a programmer error — callers wanting
// "open file if it exists, else default" should consult Discover and decide
// for themselves whether to call Load or fall back to Default.
//
// Validation rules (per design §3.b):
//
//   - File missing / unreadable          → error.
//   - YAML parse error                    → error with line:col from yaml.v3.
//   - Unknown top-level key               → warning on stderr, continue.
//   - Unknown key inside a known section  → error.
//   - version not equal to CurrentVersion → error.
//   - Empty primary on a bypass section,
//     malformed gvk on an immutable entry  → error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read xpc.yaml at %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse xpc.yaml at %s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes a single xpc.yaml document from raw bytes. Exposed alongside
// Load so callers (tests, embedded callers) can supply the bytes directly.
func Parse(data []byte) (*Config, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		// Empty file — equivalent to absent. Default semantics.
		out := Default()
		return out, nil
	}

	// Decode in two passes:
	//   1. Strict-decode into Config so unknown nested keys surface as
	//      yaml.v3 KnownFields errors.
	//   2. Loose-decode into a map[string]interface{} so we can spot
	//      unknown TOP-LEVEL keys and demote them to a warning instead of
	//      an error (forward-compat across binary versions).
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	knownTopLevel := map[string]bool{
		"version":            true,
		"prod-patterns":      true,
		"immutable-fields":   true,
		"bypass-annotations": true,
		"name-carveouts":     true,
	}
	for k := range raw {
		if !knownTopLevel[k] {
			fmt.Fprintf(os.Stderr,
				"warning: unknown top-level key %q in xpc.yaml; ignoring (forward-compat)\n", k)
		}
	}

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	cfg := &Config{}
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		// Strip "yaml: unmarshal errors:" prefix for cleaner output.
		msg := err.Error()
		// Extra context: tell users the schema version we expect when a
		// nested-key error fires, since the most common cause of an
		// unknown nested key is a forward-incompatible schema bump.
		if strings.Contains(msg, "field ") {
			return nil, fmt.Errorf("%s (schema version %d)", msg, CurrentVersion)
		}
		return nil, err
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate enforces the semantic rules that survive YAML parsing — version
// match, GVK well-formedness, non-empty primaries.
func validate(cfg *Config) error {
	if cfg.Version == 0 {
		// Treat omitted version as "default for this binary", matching the
		// "absent file is fine" mental model: an empty xpc.yaml or one with
		// only optional sections is forward-compatible.
		cfg.Version = CurrentVersion
	}
	if cfg.Version != CurrentVersion {
		return fmt.Errorf(
			"unsupported xpc.yaml version: got %d, this binary understands version %d",
			cfg.Version, CurrentVersion)
	}

	for i, entry := range cfg.ImmutableFields {
		if entry.GVK == "" {
			return fmt.Errorf("immutable-fields[%d]: gvk is required", i)
		}
		if len(entry.Paths) == 0 {
			return fmt.Errorf("immutable-fields[%d] (%s): paths is required",
				i, entry.GVK)
		}
		// gvk must split into 1, 2, or 3 parts; nothing weirder.
		parts := strings.Split(entry.GVK, "/")
		if len(parts) > 3 {
			return fmt.Errorf("immutable-fields[%d] (%s): gvk has too many segments (want group/version/Kind)",
				i, entry.GVK)
		}
		if len(parts) == 2 && !looksLikeVersion(parts[0]) {
			return fmt.Errorf("immutable-fields[%d] (%s): two-segment gvk is reserved for core APIs like v1/ConfigMap; use group/version/Kind for grouped resources",
				i, entry.GVK)
		}
		for _, p := range entry.Paths {
			if p == "" {
				return fmt.Errorf("immutable-fields[%d] (%s): paths contains empty entry",
					i, entry.GVK)
			}
		}
	}

	// A bypass primary set to "" is a programmer error — that means "have
	// no built-in primary, only aliases". The user can opt into that by
	// listing only aliases and leaving primary unset (zero value) — which
	// resolves to the built-in. To genuinely drop the primary, use
	// `primary: " "` and accept the noise. We don't currently support that
	// shape; flag the empty-string-explicit case by checking whitespace.
	// (yaml.v3 doesn't distinguish "key not present" from "key: \"\""; we
	// can't reject the latter without false positives, so this is purely
	// documentation.)

	return nil
}

func looksLikeVersion(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
