package uploadbundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	Format       = "tar.gz"
	ManifestPath = "guardian-upload-manifest.json"
	ChecksumPath = "guardian-upload-sha256sums.txt"
)

var RequiredBuildArtifacts = []RequiredArtifact{
	{
		Source: "bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian",
		Target: "bazel-bin/src/guardian-specification/cli/cmd/guardian/guardian_/guardian",
		Mode:   "0755",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar",
		Target: "bazel-bin/src/infrastructure-components/nomad/nomad-runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover",
		Target: "bazel-bin/src/infrastructure-components/nomad/cmd/nomad-recover/nomad-recover_/nomad-recover",
		Mode:   "0755",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar",
		Target: "bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover",
		Target: "bazel-bin/src/infrastructure-components/openbao/cmd/openbao-recover/openbao-recover_/openbao-recover",
		Mode:   "0755",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar",
		Target: "bazel-bin/src/infrastructure-components/haproxy/haproxy-runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar",
		Target: "bazel-bin/src/infrastructure-components/postgresql/postgresql_runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar",
		Target: "bazel-bin/src/infrastructure-components/clickhouse/clickhouse-runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover",
		Target: "bazel-bin/src/infrastructure-components/clickhouse/cmd/clickhouse-recover/clickhouse-recover_/clickhouse-recover",
		Mode:   "0755",
	},
	{
		Source: "bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar",
		Target: "bazel-bin/src/integrations/cloudflare/control-plane/cloudflare-control-plane-runtime.tar",
		Mode:   "0644",
	},
	{
		Source: "bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar",
		Target: "bazel-bin/src/services/object-storage-service/cmd/object-storage-service/object-storage-service.tar",
		Mode:   "0644",
	},
}

type RequiredArtifact struct {
	Source string
	Target string
	Mode   string
}

type File struct {
	Source string
	Target string
	Mode   string
}

type Manifest struct {
	Format              string         `json:"format"`
	ArchiveSHA256       string         `json:"archive_sha256"`
	CompressedBytes     int64          `json:"compressed_bytes"`
	UncompressedBytes   int64          `json:"uncompressed_bytes"`
	Files               []ManifestFile `json:"files"`
	Compression         string         `json:"compression"`
	CompressionSettings string         `json:"compression_settings"`
}

type ManifestFile struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func BuildWorkspaceTarGzip(repoRoot string, w io.Writer) (Manifest, error) {
	files, err := WorkspaceFiles(repoRoot)
	if err != nil {
		return Manifest{}, err
	}
	return WriteTarGzip(w, files)
}

func WorkspaceFiles(repoRoot string) ([]File, error) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	workspaceFiles, err := listedWorkspaceFiles(root)
	if err != nil {
		return nil, err
	}
	generatedFiles, err := generatedWorkspaceFiles(root)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(workspaceFiles)+len(generatedFiles)+len(RequiredBuildArtifacts))
	files = append(files, workspaceFiles...)
	files = append(files, generatedFiles...)
	var missing []string
	for _, artifact := range RequiredBuildArtifacts {
		source := filepath.Join(root, filepath.FromSlash(artifact.Source))
		if _, _, err := resolvedRegularFile(source, root, true); err != nil {
			missing = append(missing, fmt.Sprintf("%s: %v", artifact.Source, err))
			continue
		}
		files = append(files, File{
			Source: source,
			Target: artifact.Target,
			Mode:   artifact.Mode,
		})
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("required build artifacts are missing; run bazelisk build //src/guardian-specification/cli/cmd/guardian:guardian //src/infrastructure-components/nomad:runtime_artifact //src/infrastructure-components/nomad/cmd/nomad-recover:nomad-recover //src/infrastructure-components/openbao:runtime_artifact //src/infrastructure-components/openbao/cmd/openbao-recover:openbao-recover //src/infrastructure-components/haproxy:runtime_artifact //src/infrastructure-components/postgresql:runtime_artifact //src/infrastructure-components/clickhouse:runtime_artifact //src/infrastructure-components/clickhouse/cmd/clickhouse-recover:clickhouse-recover //src/integrations/cloudflare/control-plane:runtime_artifact //src/services/object-storage-service/cmd/object-storage-service:object-storage-service_nomad_artifact: %s", strings.Join(missing, "; "))
	}
	return files, nil
}

func WriteTarGzip(w io.Writer, files []File) (Manifest, error) {
	if len(files) == 0 {
		return Manifest{}, errors.New("upload bundle requires at least one file")
	}
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(a File, b File) int {
		if a.Target < b.Target {
			return -1
		}
		if a.Target > b.Target {
			return 1
		}
		return strings.Compare(a.Source, b.Source)
	})
	archiveHash := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(w, archiveHash)}
	encoder := gzip.NewWriter(counter)
	encoder.ModTime = time.Unix(0, 0).UTC()
	encoder.OS = 3
	tw := tar.NewWriter(encoder)
	manifest := Manifest{
		Format:              Format,
		Files:               make([]ManifestFile, 0, len(ordered)+1),
		Compression:         "gzip",
		CompressionSettings: "default",
	}
	for _, file := range ordered {
		record, err := writeFile(tw, file)
		if err != nil {
			_ = tw.Close()
			_ = encoder.Close()
			return Manifest{}, err
		}
		manifest.UncompressedBytes += record.Size
		manifest.Files = append(manifest.Files, record)
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = tw.Close()
		_ = encoder.Close()
		return Manifest{}, fmt.Errorf("encode upload manifest: %w", err)
	}
	manifestRecord, err := writeBytes(tw, ManifestPath, "0644", manifestBytes)
	if err != nil {
		_ = tw.Close()
		_ = encoder.Close()
		return Manifest{}, err
	}
	manifest.UncompressedBytes += manifestRecord.Size
	checksumBytes := checksumFile(append(slices.Clone(manifest.Files), manifestRecord))
	checksumRecord, err := writeBytes(tw, ChecksumPath, "0644", checksumBytes)
	if err != nil {
		_ = tw.Close()
		_ = encoder.Close()
		return Manifest{}, err
	}
	manifest.UncompressedBytes += checksumRecord.Size
	if err := tw.Close(); err != nil {
		_ = encoder.Close()
		return Manifest{}, fmt.Errorf("close tar stream: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close gzip stream: %w", err)
	}
	manifest.CompressedBytes = counter.n
	manifest.ArchiveSHA256 = "sha256:" + hex.EncodeToString(archiveHash.Sum(nil))
	return manifest, nil
}

func writeFile(tw *tar.Writer, file File) (ManifestFile, error) {
	target, err := cleanTarget(file.Target)
	if err != nil {
		return ManifestFile{}, err
	}
	mode, err := parseMode(file.Mode)
	if err != nil {
		return ManifestFile{}, err
	}
	info, resolved, err := fileInfo(file.Source)
	if err != nil {
		return ManifestFile{}, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("%s: %w", file.Source, err)
	}
	defer func() { _ = f.Close() }()
	header := &tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       target,
		Mode:       mode,
		Size:       info.Size(),
		ModTime:    time.Unix(0, 0).UTC(),
		AccessTime: time.Unix(0, 0).UTC(),
		ChangeTime: time.Unix(0, 0).UTC(),
		Uid:        0,
		Gid:        0,
		Uname:      "root",
		Gname:      "root",
		Format:     tar.FormatPAX,
	}
	if err := tw.WriteHeader(header); err != nil {
		return ManifestFile{}, fmt.Errorf("%s: write tar header: %w", file.Source, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(tw, io.TeeReader(f, hash)); err != nil {
		return ManifestFile{}, fmt.Errorf("%s: write tar file: %w", file.Source, err)
	}
	return ManifestFile{
		Source: target,
		Target: target,
		Mode:   file.Mode,
		SHA256: "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		Size:   info.Size(),
	}, nil
}

func writeBytes(tw *tar.Writer, name string, mode string, data []byte) (ManifestFile, error) {
	target, err := cleanTarget(name)
	if err != nil {
		return ManifestFile{}, err
	}
	parsedMode, err := parseMode(mode)
	if err != nil {
		return ManifestFile{}, err
	}
	header := &tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       target,
		Mode:       parsedMode,
		Size:       int64(len(data)),
		ModTime:    time.Unix(0, 0).UTC(),
		AccessTime: time.Unix(0, 0).UTC(),
		ChangeTime: time.Unix(0, 0).UTC(),
		Uid:        0,
		Gid:        0,
		Uname:      "root",
		Gname:      "root",
		Format:     tar.FormatPAX,
	}
	if err := tw.WriteHeader(header); err != nil {
		return ManifestFile{}, fmt.Errorf("%s: write tar header: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return ManifestFile{}, fmt.Errorf("%s: write tar file: %w", name, err)
	}
	sum := sha256.Sum256(data)
	return ManifestFile{
		Source: target,
		Target: target,
		Mode:   mode,
		SHA256: "sha256:" + hex.EncodeToString(sum[:]),
		Size:   int64(len(data)),
	}, nil
}

func checksumFile(files []ManifestFile) []byte {
	var b strings.Builder
	for _, file := range files {
		_, _ = fmt.Fprintf(&b, "%s  %s\n", strings.TrimPrefix(file.SHA256, "sha256:"), file.Target)
	}
	return []byte(b.String())
}

func listedWorkspaceFiles(repoRoot string) ([]File, error) {
	tracked, err := gitWorkspaceFiles(repoRoot)
	if err == nil {
		return tracked, nil
	}
	return walkedWorkspaceFiles(repoRoot)
}

func gitWorkspaceFiles(repoRoot string) ([]File, error) {
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	parts := bytesSplitNul(output)
	files := make([]File, 0, len(parts))
	for _, rel := range parts {
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		if skipWorkspaceFile(rel) {
			continue
		}
		source := filepath.Join(repoRoot, filepath.FromSlash(rel))
		info, _, err := resolvedRegularFile(source, repoRoot, false)
		if err != nil {
			continue
		}
		files = append(files, File{
			Source: source,
			Target: "workspace/" + rel,
			Mode:   modeString(info.Mode()),
		})
	}
	return files, nil
}

func generatedWorkspaceFiles(repoRoot string) ([]File, error) {
	const flyDocumentPath = ".guardian/fly/document.json"
	source := filepath.Join(repoRoot, filepath.FromSlash(flyDocumentPath))
	info, _, err := resolvedRegularFile(source, repoRoot, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", flyDocumentPath, err)
	}
	return []File{{
		Source: source,
		Target: "workspace/" + flyDocumentPath,
		Mode:   modeString(info.Mode()),
	}}, nil
}

func walkedWorkspaceFiles(repoRoot string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(repoRoot, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repoRoot, current)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() && skipWorkspaceDir(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, _, err := resolvedRegularFile(current, repoRoot, false)
		if err != nil {
			return nil
		}
		files = append(files, File{
			Source: current,
			Target: "workspace/" + filepath.ToSlash(rel),
			Mode:   modeString(info.Mode()),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func skipWorkspaceDir(rel string) bool {
	return skipWorkspaceGeneratedPath(rel) || strings.HasSuffix(rel, "/reports")
}

func skipWorkspaceFile(rel string) bool {
	return skipWorkspaceGeneratedPath(rel) || strings.HasSuffix(rel, "/reports")
}

func skipWorkspaceGeneratedPath(rel string) bool {
	switch {
	case rel == ".git",
		rel == ".guardian",
		strings.HasPrefix(rel, ".guardian/"),
		rel == "node_modules",
		strings.HasPrefix(rel, "node_modules/"),
		rel == ".cache",
		strings.HasPrefix(rel, ".cache/"),
		rel == ".mypy_cache",
		strings.HasPrefix(rel, ".mypy_cache/"),
		rel == ".ruff_cache",
		strings.HasPrefix(rel, ".ruff_cache/"),
		rel == "bazel-bin",
		strings.HasPrefix(rel, "bazel-bin/"),
		rel == "bazel-out",
		strings.HasPrefix(rel, "bazel-out/"),
		rel == "bazel-testlogs",
		strings.HasPrefix(rel, "bazel-testlogs/"),
		rel == "bazel-verself-sh",
		strings.HasPrefix(rel, "bazel-verself-sh/"):
		return true
	default:
		return false
	}
}

func fileInfo(source string) (os.FileInfo, string, error) {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, "", fmt.Errorf("%s: resolve symlink: %w", source, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", source, err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%s: upload source must be a regular file", source)
	}
	return info, resolved, nil
}

func resolvedRegularFile(source string, repoRoot string, allowOutsideRepo bool) (os.FileInfo, string, error) {
	info, resolved, err := fileInfo(source)
	if err != nil {
		return nil, "", err
	}
	if !allowOutsideRepo {
		inside, err := pathIsInside(repoRoot, resolved)
		if err != nil {
			return nil, "", err
		}
		if !inside {
			return nil, "", fmt.Errorf("%s: symlink resolves outside repo", source)
		}
	}
	return info, resolved, nil
}

func pathIsInside(root string, candidate string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return false, err
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."), nil
}

func cleanTarget(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("upload target is required")
	}
	if path.IsAbs(target) {
		return "", fmt.Errorf("upload target %q must be relative", target)
	}
	if strings.Contains(target, "\\") {
		return "", fmt.Errorf("upload target %q must use slash separators", target)
	}
	cleaned := path.Clean(target)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("upload target %q escapes the upload root", target)
	}
	return cleaned, nil
}

func parseMode(mode string) (int64, error) {
	switch mode {
	case "0644":
		return 0o644, nil
	case "0755":
		return 0o755, nil
	default:
		return 0, fmt.Errorf("mode %q must be 0644 or 0755", mode)
	}
}

func modeString(mode os.FileMode) string {
	if mode&0o111 != 0 {
		return "0755"
	}
	return "0644"
}

func bytesSplitNul(data []byte) []string {
	raw := strings.Split(string(data), "\x00")
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}
