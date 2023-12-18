package patcher

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Limited information about a file.
type BasicFileInfo struct {
	// When the file was last modified.
	ModTime time.Time
}

// ScanFiles recursively determines all files in the directory
// and gets limited information such as modification time.
func ScanFiles(rootDir string) (map[string]BasicFileInfo, error) {
	infos := make(map[string]BasicFileInfo)
	filesystem := os.DirFS(rootDir)
	err := fs.WalkDir(filesystem, ".", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error while scanning file '%s': %w", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("error while statting file '%s': %w", path, err)
		}
		infos[filepath.Clean(path)] = BasicFileInfo{ModTime: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return infos, nil
}
