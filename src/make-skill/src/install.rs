//! Install a local mksk shim to an explicit bin directory.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;

#[cfg(unix)]
use std::os::unix::fs as unix_fs;

fn main() -> ExitCode {
    match run() {
        Ok(path) => {
            println!("installed {}", path.display());
            ExitCode::SUCCESS
        }
        Err(err) => {
            eprintln!("error: {err}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<PathBuf, String> {
    let bin_dir = parse_bin_dir()?;
    let mksk = resolve_bazel_runfile(
        &env::var("MKSK_BINARY").map_err(|_| "MKSK_BINARY is missing".to_string())?,
    )?;

    fs::create_dir_all(&bin_dir)
        .map_err(|err| format!("create '{}': {err}", bin_dir.display()))?;

    let link = bin_dir.join("mksk");
    install_symlink(&mksk, &link)?;
    Ok(link)
}

fn parse_bin_dir() -> Result<PathBuf, String> {
    let mut bin_dir = None;
    for arg in env::args().skip(1) {
        if let Some(value) = arg.strip_prefix("--bin-dir=") {
            if value.is_empty() {
                return Err("--bin-dir must not be empty".to_string());
            }
            if bin_dir.replace(PathBuf::from(value)).is_some() {
                return Err("--bin-dir provided more than once".to_string());
            }
            continue;
        }
        return Err(format!("unexpected argument {arg}; usage: install --bin-dir=<dir>"));
    }

    bin_dir.ok_or_else(|| "usage: install --bin-dir=<dir>".to_string())
}

fn resolve_bazel_runfile(path: &str) -> Result<PathBuf, String> {
    let candidate = PathBuf::from(path);
    if candidate.is_absolute() {
        return canonical_file(&candidate);
    }

    if let Ok(manifest) = env::var("RUNFILES_MANIFEST_FILE")
        && let Some(resolved) = lookup_manifest(&manifest, path)
    {
        return canonical_file(&resolved);
    }

    let runfiles_dir = env::var("RUNFILES_DIR").ok();
    let workspace = env::var("TEST_WORKSPACE")
        .ok()
        .or_else(|| env::var("RUNFILES_WORKSPACE").ok())
        .unwrap_or_else(|| "verself".to_string());

    if let Some(dir) = runfiles_dir {
        for prefix in [workspace, "_main".to_string()] {
            let resolved = Path::new(&dir).join(prefix).join(&candidate);
            if resolved.exists() {
                return canonical_file(&resolved);
            }
        }
    }

    canonical_file(&candidate)
}

fn lookup_manifest(manifest: &str, path: &str) -> Option<PathBuf> {
    let body = fs::read_to_string(manifest).ok()?;
    for key in [path.to_string(), format!("_main/{path}")] {
        let prefix = format!("{key} ");
        if let Some(line) = body.lines().find(|line| line.starts_with(&prefix)) {
            let (_, resolved) = line.split_once(' ')?;
            return Some(PathBuf::from(resolved));
        }
    }
    None
}

fn canonical_file(path: &Path) -> Result<PathBuf, String> {
    let absolute =
        fs::canonicalize(path).map_err(|err| format!("resolve '{}': {err}", path.display()))?;
    if !absolute.is_file() {
        return Err(format!("'{}' is not a file", absolute.display()));
    }
    Ok(absolute)
}

#[cfg(unix)]
fn install_symlink(target: &Path, link: &Path) -> Result<(), String> {
    if let Ok(metadata) = fs::symlink_metadata(link) {
        if metadata.file_type().is_dir() {
            return Err(format!("'{}' is a directory", link.display()));
        }
        if !metadata.file_type().is_symlink() {
            return Err(format!("'{}' exists and is not a symlink", link.display()));
        }
    }

    let tmp = link.with_file_name(format!(
        ".{}.tmp.{}",
        link.file_name().and_then(|name| name.to_str()).unwrap_or("mksk"),
        std::process::id()
    ));
    let _ = fs::remove_file(&tmp);
    unix_fs::symlink(target, &tmp)
        .map_err(|err| format!("symlink '{}' -> '{}': {err}", tmp.display(), target.display()))?;
    fs::rename(&tmp, link).map_err(|err| {
        let _ = fs::remove_file(&tmp);
        format!("rename '{}' -> '{}': {err}", tmp.display(), link.display())
    })?;
    Ok(())
}

#[cfg(not(unix))]
fn install_symlink(_target: &Path, _link: &Path) -> Result<(), String> {
    Err("install currently supports Unix symlinks only".to_string())
}
