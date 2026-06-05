package seed

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

type File struct {
	Source string
	Target string
	Mode   string
}

type Manifest struct {
	ArchiveSHA256       string
	CompressedBytes     int64
	UncompressedBytes   int64
	Files               []ManifestFile
	Compression         string
	CompressionSettings string
}

type ManifestFile struct {
	Source string
	Target string
	Mode   string
	SHA256 string
	Size   int64
}

func WriteTarZstd(w io.Writer, files []File) (Manifest, error) {
	if len(files) == 0 {
		return Manifest{}, errors.New("seed archive requires at least one file")
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
	encoder, err := zstd.NewWriter(counter, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(runtime.GOMAXPROCS(0)))
	if err != nil {
		return Manifest{}, fmt.Errorf("create zstd encoder: %w", err)
	}
	tw := tar.NewWriter(encoder)
	manifest := Manifest{
		Files:               make([]ManifestFile, 0, len(ordered)),
		Compression:         "zstd",
		CompressionSettings: "speed_fastest",
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
	if err := tw.Close(); err != nil {
		_ = encoder.Close()
		return Manifest{}, fmt.Errorf("close tar stream: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return Manifest{}, fmt.Errorf("close zstd stream: %w", err)
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
	info, err := os.Lstat(file.Source)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("%s: %w", file.Source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ManifestFile{}, fmt.Errorf("%s: symlink seed sources are not allowed", file.Source)
	}
	if !info.Mode().IsRegular() {
		return ManifestFile{}, fmt.Errorf("%s: seed source must be a regular file", file.Source)
	}
	f, err := os.Open(file.Source)
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
		Source: filepath.ToSlash(file.Source),
		Target: target,
		Mode:   file.Mode,
		SHA256: "sha256:" + hex.EncodeToString(hash.Sum(nil)),
		Size:   info.Size(),
	}, nil
}

func cleanTarget(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("seed target is required")
	}
	if path.IsAbs(target) {
		return "", fmt.Errorf("seed target %q must be relative", target)
	}
	if strings.Contains(target, "\\") {
		return "", fmt.Errorf("seed target %q must use slash separators", target)
	}
	cleaned := path.Clean(target)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("seed target %q escapes the seed root", target)
	}
	return cleaned, nil
}

func parseMode(mode string) (int64, error) {
	if len(mode) != 4 || mode[0] != '0' {
		return 0, fmt.Errorf("mode %q must be a four-digit octal string", mode)
	}
	parsed, err := strconv.ParseInt(mode, 8, 64)
	if err != nil {
		return 0, fmt.Errorf("mode %q must be octal", mode)
	}
	return parsed, nil
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
