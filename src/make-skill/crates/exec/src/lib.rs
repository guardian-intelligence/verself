//! Headless command behavior for `mksk`.

use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::process::ExitCode;

use make_skill_core::{create_skill, CreateSkillRequest, SkillTarget};

const USAGE: &str = "usage: mksk <skill-name>\n       mksk upgrade [--channel CHANNEL] [--json]";

#[derive(Debug, Clone, Copy, Eq, PartialEq)]
pub enum ExitStatus {
    Success,
    Failure,
}

impl ExitStatus {
    pub fn into_exit_code(self) -> ExitCode {
        match self {
            Self::Success => ExitCode::SUCCESS,
            Self::Failure => ExitCode::FAILURE,
        }
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct ExecOutput {
    pub status: ExitStatus,
    pub stdout: String,
    pub stderr: String,
}

impl ExecOutput {
    fn success(stdout: String) -> Self {
        Self {
            status: ExitStatus::Success,
            stdout,
            stderr: String::new(),
        }
    }

    fn failure(stderr: String) -> Self {
        Self {
            status: ExitStatus::Failure,
            stdout: String::new(),
            stderr,
        }
    }
}

pub fn run<I>(args: I) -> ExecOutput
where
    I: IntoIterator<Item = String>,
{
    let workspace_root = match std::env::current_dir() {
        Ok(workspace_root) => workspace_root,
        Err(err) => return ExecOutput::failure(format!("error: read current directory: {err}\n")),
    };
    run_in_workspace(args, workspace_root)
}

pub fn run_in_workspace<I>(args: I, workspace_root: impl Into<std::path::PathBuf>) -> ExecOutput
where
    I: IntoIterator<Item = String>,
{
    let mut args = args.into_iter();
    let Some(target) = args.next().filter(|arg| !arg.is_empty()) else {
        return ExecOutput::failure(format!("{USAGE}\n"));
    };
    if target == "upgrade" {
        return run_upgrade(args.collect());
    }
    if let Some(extra) = args.next() {
        return ExecOutput::failure(format!("error: unexpected argument {extra:?}\n{USAGE}\n"));
    }

    let target = match SkillTarget::parse(&target) {
        Ok(target) => target,
        Err(err) => return ExecOutput::failure(format!("error: {err}\n")),
    };

    match create_skill(CreateSkillRequest::new(workspace_root, target)) {
        Ok(created) => ExecOutput::success(format!(
            "created {}\n",
            created.relative_skill_file().display()
        )),
        Err(err) => ExecOutput::failure(format!("error: {err}\n")),
    }
}

fn run_upgrade(args: Vec<String>) -> ExecOutput {
    let updater = match find_verself_updater() {
        Some(updater) => updater,
        None => {
            return ExecOutput::failure(
                "error: mksk upgrade requires the verself CLI on PATH or VERSELF_MKSK_UPDATER\n"
                    .to_string(),
            );
        }
    };
    let current = match std::env::current_exe() {
        Ok(path) => path,
        Err(err) => return ExecOutput::failure(format!("error: resolve mksk executable: {err}\n")),
    };
    let output = match Command::new(&updater)
        .args(upgrade_command_args(&args, &current))
        .output()
    {
        Ok(output) => output,
        Err(err) => {
            return ExecOutput::failure(format!("error: run '{}': {err}\n", updater.display()));
        }
    };
    ExecOutput {
        status: if output.status.success() {
            ExitStatus::Success
        } else {
            ExitStatus::Failure
        },
        stdout: String::from_utf8_lossy(&output.stdout).into_owned(),
        stderr: String::from_utf8_lossy(&output.stderr).into_owned(),
    }
}

fn upgrade_command_args(args: &[String], binary_path: &Path) -> Vec<OsString> {
    let mut out = Vec::with_capacity(args.len() + 4);
    out.push(OsString::from("internal-upgrade"));
    out.push(OsString::from("mksk"));
    out.push(OsString::from("--binary-path"));
    out.push(binary_path.as_os_str().to_owned());
    out.extend(args.iter().map(OsString::from));
    out
}

fn find_verself_updater() -> Option<PathBuf> {
    if let Some(value) = std::env::var_os("VERSELF_MKSK_UPDATER") {
        if !value.is_empty() {
            return Some(PathBuf::from(value));
        }
    }
    let path = std::env::var_os("PATH")?;
    for dir in std::env::split_paths(&path) {
        let candidate = dir.join("verself");
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::upgrade_command_args;
    use std::ffi::OsString;
    use std::path::Path;

    #[test]
    fn mksk_upgrade_delegates_to_scoped_verself_entrypoint() {
        let args = upgrade_command_args(
            &["--channel".to_string(), "canary".to_string()],
            Path::new("/tmp/mksk"),
        );
        assert_eq!(
            args,
            vec![
                OsString::from("internal-upgrade"),
                OsString::from("mksk"),
                OsString::from("--binary-path"),
                OsString::from("/tmp/mksk"),
                OsString::from("--channel"),
                OsString::from("canary")
            ]
        );
    }
}
