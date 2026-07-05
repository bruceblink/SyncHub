package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	mode := flag.String("mode", "", "create or extract")
	source := flag.String("source", "", "source directory for create, destination directory for extract")
	archivePath := flag.String("archive", "", "tar.gz archive path")
	execList := flag.String("exec", "", "comma-separated executable file names")
	flag.Parse()

	executables := parseExecutableList(*execList)
	var err error
	switch *mode {
	case "create":
		err = createArchive(*source, *archivePath, executables)
	case "extract":
		err = extractArchive(*archivePath, *source, executables)
	default:
		err = fmt.Errorf("mode must be create or extract: %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseExecutableList(list string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(strings.ReplaceAll(item, "\\", "/"))
		item = strings.TrimPrefix(item, "./")
		if item == "" {
			continue
		}
		result[path.Base(item)] = struct{}{}
	}
	return result
}

func createArchive(sourceDir, archivePath string, executables map[string]struct{}) error {
	if sourceDir == "" {
		return errors.New("source is required")
	}
	if archivePath == "" {
		return errors.New("archive is required")
	}

	sourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return err
	}

	paths, err := collectArchivePaths(sourceDir)
	if err != nil {
		return err
	}

	out, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer out.Close()

	gzipWriter := gzip.NewWriter(out)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for _, filePath := range paths {
		if err := addArchiveEntry(tarWriter, sourceDir, filePath, executables); err != nil {
			return err
		}
	}
	return nil
}

func collectArchivePaths(sourceDir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(sourceDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == sourceDir {
			return nil
		}
		paths = append(paths, filePath)
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func addArchiveEntry(tarWriter *tar.Writer, sourceDir, filePath string, executables map[string]struct{}) error {
	info, err := os.Lstat(filePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlinks are not supported in release archives: %s", filePath)
	}

	rel, err := filepath.Rel(sourceDir, filePath)
	if err != nil {
		return err
	}
	name := filepath.ToSlash(rel)

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name
	header.Uid = 0
	header.Gid = 0
	header.Uname = ""
	header.Gname = ""

	if info.IsDir() {
		header.Mode = 0o755
		header.Name = strings.TrimSuffix(name, "/") + "/"
		return tarWriter.WriteHeader(header)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported release archive entry: %s", filePath)
	}

	header.Mode = 0o644
	if isExecutable(name, executables) {
		header.Mode = 0o755
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(tarWriter, file)
	return err
}

func extractArchive(archivePath, destDir string, executables map[string]struct{}) error {
	if archivePath == "" {
		return errors.New("archive is required")
	}
	if destDir == "" {
		return errors.New("source destination is required for extract mode")
	}

	destDir, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	missingExecutables := make(map[string]struct{}, len(executables))
	for name := range executables {
		missingExecutables[name] = struct{}{}
	}

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if err := extractArchiveEntry(tarReader, header, destDir, missingExecutables); err != nil {
			return err
		}
	}

	if len(missingExecutables) > 0 {
		names := make([]string, 0, len(missingExecutables))
		for name := range missingExecutables {
			names = append(names, name)
		}
		sort.Strings(names)
		return fmt.Errorf("archive missing executable entries: %s", strings.Join(names, ", "))
	}
	return nil
}

func extractArchiveEntry(tarReader *tar.Reader, header *tar.Header, destDir string, missingExecutables map[string]struct{}) error {
	name, err := cleanArchiveName(header.Name)
	if err != nil {
		return err
	}
	if name == "" {
		return nil
	}

	target := filepath.Join(destDir, filepath.FromSlash(name))
	if !isSubpath(destDir, target) {
		return fmt.Errorf("archive entry escapes destination: %s", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)
	case tar.TypeReg, tar.TypeRegA:
		executable := false
		baseName := path.Base(name)
		if _, ok := missingExecutables[baseName]; ok {
			executable = true
			if header.Mode&0o111 == 0 {
				return fmt.Errorf("archive executable entry lacks execute bits: %s", name)
			}
			delete(missingExecutables, baseName)
		}
		return writeExtractedFile(tarReader, target, executable)
	default:
		return fmt.Errorf("unsupported tar entry type %d: %s", header.Typeflag, header.Name)
	}
}

func writeExtractedFile(tarReader *tar.Reader, target string, executable bool) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, tarReader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func cleanArchiveName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	name = path.Clean(name)
	if name == "." {
		return "", nil
	}
	if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("unsafe archive entry path: %s", name)
	}
	return name, nil
}

func isSubpath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isExecutable(name string, executables map[string]struct{}) bool {
	_, ok := executables[path.Base(name)]
	return ok
}
