package renderer

import (
	"errors"
	"io/fs"
	"os"
)

// readFile wraps os.ReadFile. It exists so the Helm code can be unit-tested
// by swapping readFile/isNotExist without reaching into the stdlib.
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

// isNotExist is a small wrapper so callers don't need to know about both
// os.IsNotExist and errors.Is on fs.ErrNotExist.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}

// tempValuesFile is a scope-guarded temp file used to hand helm the merged
// valuesObject / inline values as a single --values file.
type tempValuesFile struct {
	path string
}

func (t tempValuesFile) cleanup() {
	if t.path != "" {
		_ = os.Remove(t.path)
	}
}

func writeTempValues(data []byte) (tempValuesFile, error) {
	f, err := os.CreateTemp("", "xpc-helm-values-*.yaml")
	if err != nil {
		return tempValuesFile{}, err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		_ = os.Remove(f.Name())
		return tempValuesFile{}, err
	}
	return tempValuesFile{path: f.Name()}, nil
}
