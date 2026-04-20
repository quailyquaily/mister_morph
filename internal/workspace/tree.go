package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type TreeEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	IsDir       bool   `json:"is_dir"`
	HasChildren bool   `json:"has_children"`
	SizeBytes   int64  `json:"size_bytes"`
}

type TreeListing struct {
	RootPath string      `json:"root_path,omitempty"`
	Path     string      `json:"path"`
	Items    []TreeEntry `json:"items"`
}

func ListAttachedTree(workspaceDir string, relPath string) (TreeListing, error) {
	rootDir, err := ValidateDir(workspaceDir, nil)
	if err != nil {
		return TreeListing{}, err
	}
	cleanPath, targetDir, err := resolveAttachedTreePath(rootDir, relPath)
	if err != nil {
		return TreeListing{}, err
	}
	items, err := listTreeEntries(targetDir, func(absPath string) string {
		rel, relErr := filepath.Rel(rootDir, absPath)
		if relErr != nil {
			return ""
		}
		if rel == "." {
			return ""
		}
		return filepath.Clean(rel)
	})
	if err != nil {
		return TreeListing{}, err
	}
	return TreeListing{
		RootPath: rootDir,
		Path:     cleanPath,
		Items:    items,
	}, nil
}

func ListSystemTree(rawPath string) (TreeListing, error) {
	requestPath := strings.TrimSpace(rawPath)
	if requestPath == "" {
		items, err := listSystemRootEntries()
		if err != nil {
			return TreeListing{}, err
		}
		return TreeListing{
			RootPath: systemRootLabel(),
			Path:     "",
			Items:    items,
		}, nil
	}
	targetDir, err := filepath.Abs(pathutil.ExpandHomePath(requestPath))
	if err != nil {
		return TreeListing{}, err
	}
	items, err := listTreeEntries(targetDir, func(absPath string) string {
		return filepath.Clean(absPath)
	})
	if err != nil {
		return TreeListing{}, err
	}
	return TreeListing{
		RootPath: targetDir,
		Path:     targetDir,
		Items:    items,
	}, nil
}

func resolveAttachedTreePath(rootDir string, relPath string) (string, string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" || relPath == "." {
		return "", rootDir, nil
	}
	if filepath.IsAbs(relPath) {
		return "", "", fmt.Errorf("workspace tree path must be relative")
	}
	cleanPath := filepath.Clean(relPath)
	if cleanPath == "." {
		return "", rootDir, nil
	}
	if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("workspace tree path is outside the attached directory")
	}
	targetDir, err := filepath.Abs(filepath.Join(rootDir, cleanPath))
	if err != nil {
		return "", "", err
	}
	if filepath.Clean(targetDir) != filepath.Clean(rootDir) && !pathutil.IsWithinDir(rootDir, targetDir) {
		return "", "", fmt.Errorf("workspace tree path is outside the attached directory")
	}
	return cleanPath, targetDir, nil
}

func listSystemRootEntries() ([]TreeEntry, error) {
	if runtime.GOOS != "windows" {
		root := string(filepath.Separator)
		return listTreeEntries(root, func(absPath string) string {
			return filepath.Clean(absPath)
		})
	}
	items := make([]TreeEntry, 0, 8)
	for drive := 'A'; drive <= 'Z'; drive += 1 {
		root := fmt.Sprintf("%c:\\", drive)
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		items = append(items, TreeEntry{
			Name:        root,
			Path:        root,
			IsDir:       true,
			HasChildren: dirHasChildren(root),
			SizeBytes:   info.Size(),
		})
	}
	sortTreeEntries(items)
	return items, nil
}

func systemRootLabel() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return string(filepath.Separator)
}

func listTreeEntries(dir string, pathBuilder func(absPath string) string) ([]TreeEntry, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("directory path is required")
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory does not exist: %s", dir)
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("directory is not readable: %s", dir)
	}
	items := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		absPath := filepath.Join(dir, name)
		sizeBytes := int64(-1)
		info, infoErr := entry.Info()
		if infoErr == nil {
			sizeBytes = info.Size()
		}
		items = append(items, TreeEntry{
			Name:        name,
			Path:        pathBuilder(absPath),
			IsDir:       entry.IsDir(),
			HasChildren: entry.IsDir() && dirHasChildren(absPath),
			SizeBytes:   sizeBytes,
		})
	}
	sortTreeEntries(items)
	return items, nil
}

func dirHasChildren(dir string) bool {
	file, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer file.Close()
	names, err := file.Readdirnames(1)
	return err == nil && len(names) > 0
}

func sortTreeEntries(items []TreeEntry) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		leftName := strings.ToLower(items[i].Name)
		rightName := strings.ToLower(items[j].Name)
		if leftName == rightName {
			return items[i].Name < items[j].Name
		}
		return leftName < rightName
	})
}
