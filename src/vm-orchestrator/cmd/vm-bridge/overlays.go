package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	// Overlay contract files at the root of a read-only toolchain image.
	// Both are optional; a toolchain image without either mounts as a
	// plain read-only ext4 with no host-side post-processing.
	writableOverlaysFile = ".verself-writable-overlays"
	etcOverlayDir        = "etc-overlay"
)

// applyToolchainOverlays runs the overlay contract for one read-only
// toolchain image. It is called by mountFilesystems immediately after
// the ext4 is mounted at mountPath. The contract is documented at
// src/vm-orchestrator/guest-images/BUILD.bazel:
//
//	<mount>/.verself-writable-overlays    newline-separated absolute
//	                                      paths to tmpfs-mount over
//	                                      the read-only base.
//	<mount>/etc-overlay/<rel>             files copied to /etc/<rel>
//	                                      (refusing to overwrite
//	                                      anything previously
//	                                      overlaid by a sibling image).
//	<mount>/etc-overlay/passwd            new user entries trigger
//	                                      mkdir + chown of $HOME.
//
// Any failure short-circuits and propagates up — overlays are
// load-bearing for runner exec credentials, so a partial application
// is the wrong recovery posture.
func (s *agentSession) applyToolchainOverlays(imageName, mountPath string) error {
	if err := s.applyEtcOverlay(imageName, mountPath); err != nil {
		return err
	}
	if err := s.applyWritableOverlays(mountPath); err != nil {
		return err
	}
	return nil
}

// applyEtcOverlay walks <mountPath>/etc-overlay/ and copies each file
// into /etc/<rel>. Files are copied (not bind-mounted) because the
// substrate's /etc is rwroot inside the lease; the entries land
// alongside any pre-baked substrate config and are wiped when the
// lease is destroyed.
func (s *agentSession) applyEtcOverlay(imageName, mountPath string) error {
	overlayRoot := filepath.Join(mountPath, etcOverlayDir)
	info, err := os.Stat(overlayRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", overlayRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", overlayRoot)
	}

	if s.etcOverlayApplied == nil {
		s.etcOverlayApplied = map[string]etcOverlayEntry{}
	}

	walkErr := filepath.Walk(overlayRoot, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(overlayRoot, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join("/etc", rel)
		if fi.IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dest, err)
			}
			return nil
		}
		digest, err := overlayFileDigest(path)
		if err != nil {
			return err
		}
		if prior, ok := s.etcOverlayApplied[rel]; ok {
			if prior.sha256 == digest {
				// Same bytes from a second image (typically because
				// both toolchains pulled this file from the shared
				// runner-overlay-common filegroup). Skip the rewrite;
				// re-recording the entry as the second image would
				// erase the chronological "first writer wins" trail.
				return nil
			}
			return fmt.Errorf("etc-overlay collision: %s previously written by %s with digest %s; %s wants to overwrite with digest %s",
				rel, prior.imageName, prior.sha256, imageName, digest)
		}
		if err := overlayCopyFile(path, dest, fi.Mode().Perm()); err != nil {
			return err
		}
		s.etcOverlayApplied[rel] = etcOverlayEntry{imageName: imageName, sha256: digest}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// passwd is the only overlay file that drives further side effects:
	// new user entries get their HOME materialised. Walk it once, after
	// the bulk copy, so the on-disk /etc/passwd is the authoritative
	// source even if the same image overlaid both passwd and a profile.d
	// hook.
	if _, ok := s.etcOverlayApplied["passwd"]; ok {
		if err := s.materializeHomeDirsFromOverlay(filepath.Join(overlayRoot, "passwd")); err != nil {
			return err
		}
	}
	return nil
}

// materializeHomeDirsFromOverlay parses <overlay>/etc-overlay/passwd
// and creates+chowns each entry's home directory. We don't try to
// reconcile against /etc/passwd's pre-existing entries — the overlay
// owns those rows and any conflict was caught by applyEtcOverlay's
// collision check.
func (s *agentSession) materializeHomeDirsFromOverlay(passwdPath string) error {
	f, err := os.Open(passwdPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", passwdPath, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Standard /etc/passwd format:
		//   name:x:UID:GID:gecos:HOME:SHELL
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			return fmt.Errorf("malformed passwd entry: %q", line)
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil {
			return fmt.Errorf("passwd entry %q: parse uid: %w", fields[0], err)
		}
		gid, err := strconv.Atoi(fields[3])
		if err != nil {
			return fmt.Errorf("passwd entry %q: parse gid: %w", fields[0], err)
		}
		home := strings.TrimSpace(fields[5])
		if home == "" || home == "/" || home == "/nonexistent" {
			continue
		}
		if err := os.MkdirAll(home, 0o755); err != nil {
			return fmt.Errorf("mkdir home %s: %w", home, err)
		}
		if err := os.Chown(home, uid, gid); err != nil {
			return fmt.Errorf("chown home %s: %w", home, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", passwdPath, err)
	}
	return nil
}

// applyWritableOverlays reads <mountPath>/.verself-writable-overlays
// and tmpfs-mounts each listed path on top of the read-only base.
// Paths must be absolute and live under mountPath; entries pointing
// elsewhere are rejected to keep image-declared overlays scoped to
// their own mount.
func (s *agentSession) applyWritableOverlays(mountPath string) error {
	manifest := filepath.Join(mountPath, writableOverlaysFile)
	f, err := os.Open(manifest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", manifest, err)
	}
	defer func() { _ = f.Close() }()

	cleanMount := filepath.Clean(mountPath)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		clean := filepath.Clean(raw)
		if !strings.HasPrefix(clean, "/") {
			return fmt.Errorf("writable-overlay %q must be absolute", raw)
		}
		if clean != cleanMount && !strings.HasPrefix(clean, cleanMount+"/") {
			return fmt.Errorf("writable-overlay %q is outside its image mount %s", raw, cleanMount)
		}
		if err := os.MkdirAll(clean, 0o755); err != nil {
			return fmt.Errorf("mkdir writable-overlay %s: %w", clean, err)
		}
		if err := syscall.Mount("tmpfs", clean, "tmpfs", syscall.MS_NOSUID|syscall.MS_NODEV, ""); err != nil {
			return fmt.Errorf("tmpfs mount writable-overlay %s: %w", clean, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", manifest, err)
	}
	return nil
}

// overlayFileDigest returns the sha256 hex digest of an overlay source
// file. Used so two toolchain images writing the same /etc/<rel> path
// with byte-identical content (e.g. both consuming the
// runner-overlay-common Bazel filegroup) compose cleanly while a true
// content collision still hard-errors at lease boot.
func overlayFileDigest(path string) (string, error) {
	in, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = in.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, in); err != nil {
		return "", fmt.Errorf("digest %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// overlayCopyFile copies src to dest preserving perms. dest's parent
// directory must already exist (filepath.Walk visits parents first so
// the etc-overlay walker has already created it via MkdirAll).
func overlayCopyFile(src, dest string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dest, err)
	}
	return nil
}
