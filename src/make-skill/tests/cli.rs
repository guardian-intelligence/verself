//! End-to-end tests against the real `mksk` binary.
//!
//! We spawn the compiled artifact rather than calling library code, so the
//! tests exercise exactly what a user runs. Cargo and Bazel provide the binary
//! path differently, so the helper below accepts both forms.

use std::env;
use std::path::PathBuf;
use std::process::Command;

fn mksk() -> PathBuf {
    if let Ok(path) = env::var("MKSK_UNDER_TEST") {
        return resolve_mksk_path(path);
    }

    if let Some(path) = option_env!("CARGO_BIN_EXE_mksk") {
        return PathBuf::from(path);
    }

    panic!("CARGO_BIN_EXE_mksk or MKSK_UNDER_TEST is required");
}

fn resolve_mksk_path(path: String) -> PathBuf {
    let candidate = PathBuf::from(&path);
    if candidate.is_absolute() {
        return candidate;
    }
    if let Ok(resolved) = candidate.canonicalize() {
        return resolved;
    }

    if let Ok(manifest) = env::var("RUNFILES_MANIFEST_FILE")
        && let Some(resolved) = lookup_manifest(&manifest, &path)
    {
        return resolved;
    }

    let runfiles = PathBuf::from(env::var("TEST_SRCDIR").expect("TEST_SRCDIR is required"));
    for workspace in [
        env::var("TEST_WORKSPACE").ok(),
        env::var("RUNFILES_WORKSPACE").ok(),
        Some("_main".to_string()),
    ]
    .into_iter()
    .flatten()
    {
        let resolved = runfiles.join(workspace).join(&candidate);
        if resolved.exists() {
            return resolved.canonicalize().unwrap_or(resolved);
        }
    }

    if let Ok(cwd) = env::current_dir() {
        return cwd.join(&candidate);
    }

    let resolved = runfiles.join(candidate);
    resolved.canonicalize().unwrap_or(resolved)
}

fn lookup_manifest(manifest: &str, path: &str) -> Option<PathBuf> {
    let body = std::fs::read_to_string(manifest).ok()?;
    for key in [path.to_string(), format!("_main/{path}")] {
        let prefix = format!("{key} ");
        if let Some(line) = body.lines().find(|line| line.starts_with(&prefix)) {
            let (_, resolved) = line.split_once(' ')?;
            return Some(PathBuf::from(resolved));
        }
    }
    None
}

/// A clean, isolated working directory under Cargo's per-crate temp dir.
fn workdir(tag: &str) -> PathBuf {
    let base = env::var("CARGO_TARGET_TMPDIR")
        .or_else(|_| env::var("TEST_TMPDIR"))
        .map(PathBuf::from)
        .unwrap_or_else(|_| env::temp_dir().join(format!("mksk-test-{}", std::process::id())));
    let dir = base.join(tag);
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

#[test]
fn creates_skill_scaffold() {
    let work = workdir("creates");
    let bin = mksk();
    let out = Command::new(&bin)
        .arg("my-skill")
        .current_dir(&work)
        .output()
        .unwrap_or_else(|err| panic!("run '{}' in '{}': {err}", bin.display(), work.display()));
    assert!(
        out.status.success(),
        "stderr: {}",
        String::from_utf8_lossy(&out.stderr)
    );

    let body = std::fs::read_to_string(work.join("my-skill/SKILL.md")).unwrap();
    assert!(body.starts_with("---\n"), "missing frontmatter fence");
    assert!(body.contains("name: my-skill"), "missing frontmatter name");
    assert!(body.contains("# my-skill"), "missing H1 heading");
}

#[test]
fn refuses_to_clobber() {
    let work = workdir("clobber");
    let bin = mksk();
    assert!(
        Command::new(&bin)
            .arg("dup")
            .current_dir(&work)
            .status()
            .unwrap_or_else(|err| panic!("run '{}' in '{}': {err}", bin.display(), work.display()))
            .success()
    );

    let second = Command::new(&bin)
        .arg("dup")
        .current_dir(&work)
        .output()
        .unwrap_or_else(|err| panic!("run '{}' in '{}': {err}", bin.display(), work.display()));
    assert!(!second.status.success(), "second run should fail loudly");
    assert!(String::from_utf8_lossy(&second.stderr).contains("already exists"));
}

#[test]
fn usage_error_without_arg() {
    let out = Command::new(mksk()).output().unwrap();
    assert!(!out.status.success(), "no-arg invocation should fail");
    assert!(String::from_utf8_lossy(&out.stderr).contains("usage:"));
}
