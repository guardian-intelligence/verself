//! Headless command behavior for `mksk`.

use std::process::ExitCode;

use make_skill_core::{CreateSkillRequest, SkillTarget, create_skill};

const USAGE: &str = "usage: mksk <skill-name>";

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
