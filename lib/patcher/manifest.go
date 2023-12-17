package patcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// Filename for the manifest under the root dir.
const ManifestFilename = "ta-manifest.json"

// A ManifestEntry records the last recorded checksum and change time for a file.
type ManifestEntry struct {
	LastChange   time.Time `json:"last_change"`
	LastChecksum string    `json:"last_checksum"`
}

// A Manifest records the last recorded checksum and change time for files.
// It's used to bypass expensive computation of the checksum for a file.
// A Manifest is a map from relative filename (with OS specific separator) to
// manifest data. E.g. a key can be "Binaries\InstallData\dotNetFx40_Full_setup.exe"
// on Windows.
type Manifest struct {
	// Identifier for whatever game is installed, something like "renx_alpha".
	Product string
	Entries map[string]ManifestEntry
}

// NewManifest creates a new empty manifest for the product.
func NewManifest(product string) *Manifest {
	return &Manifest{
		Product: product,
		Entries: make(map[string]ManifestEntry),
	}
}

// ReadManifest reads a manifest from the standard location in the installation dir.
// Verifies that the manifest has the correct product field set. Returns an empty manifest
// if there's no manifest file.
func ReadManifest(installDir string, product string) (*Manifest, error) {
	filename := filepath.Join(installDir, ManifestFilename)
	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return NewManifest(product), nil
		}
		return nil, fmt.Errorf("couldn't read manifest at %q: %w", filename, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("couldn't decode manifest at %q: %w", filename, err)
	}
	if manifest.Product != product {
		return nil, fmt.Errorf(
			"manifest contains wrong product (are you updating the wrong game?), expected %q got %q",
			product, manifest.Product,
		)
	}
	return &manifest, nil
}

// WriteManifest writes a manifest to the standard location in the installation dir.
func (m *Manifest) WriteManifest(installDir string) error {
	filename := filepath.Join(installDir, ManifestFilename)

	encoded, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return fmt.Errorf("couldn't encode manifest: %w", err)
	}

	if err := os.WriteFile(filename, encoded, 0644); err != nil {
		return fmt.Errorf("couldn't write manifest to %q: %w", filename, err)
	}

	return nil
}

// Add adds a file along with last change info and known checksum to the manifest.
// Overwrites an existing entry for the file.
func (m *Manifest) Add(filename string, lastChange time.Time, checksum string) {
	m.Entries[path.Clean(filename)] = ManifestEntry{LastChange: lastChange, LastChecksum: checksum}
}

// Check returns true iff a file with the given name, last change time and checksum exists
// in the manifest. (I.e. if a file can be assumed to have the correct checksum.)
func (m *Manifest) Check(filename string, lastChange time.Time, checksum string) bool {
	entry, found := m.Entries[path.Clean(filename)]
	if !found {
		return false
	}
	return entry.LastChange.Equal(lastChange) && entry.LastChecksum == checksum
}
