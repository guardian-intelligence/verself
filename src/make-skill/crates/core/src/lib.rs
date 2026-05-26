//! Core skill scaffolding behavior.

use std::error::Error;
use std::fmt;
use std::fs;
use std::io;
use std::io::Write;
use std::path::{Component, Path, PathBuf};

/// SKILL.md scaffold. `{name}` is substituted with the skill's leaf name.
const SKILL_TEMPLATE: &str = "\
---
name: {name}
description: TODO - one sentence on when an agent should use this skill.
---

# {name}

TODO - write the skill instructions here.
";

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct SkillName(String);

impl SkillName {
    pub fn parse(value: &str) -> Result<Self, SkillNameError> {
        if value.is_empty() {
            return Err(SkillNameError::Empty);
        }
        if !is_valid_skill_name(value) {
            return Err(SkillNameError::Invalid {
                value: value.to_string(),
            });
        }
        Ok(Self(value.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct SkillTarget {
    path: PathBuf,
    name: SkillName,
}

impl SkillTarget {
    pub fn parse(value: &str) -> Result<Self, SkillTargetError> {
        if value.is_empty() {
            return Err(SkillTargetError::Empty);
        }

        let path = PathBuf::from(value);
        if path.is_absolute() {
            return Err(SkillTargetError::Absolute {
                value: value.to_string(),
            });
        }

        for component in path.components() {
            match component {
                Component::Normal(_) | Component::CurDir => {}
                Component::ParentDir => {
                    return Err(SkillTargetError::Parent {
                        value: value.to_string(),
                    });
                }
                Component::Prefix(_) | Component::RootDir => {
                    return Err(SkillTargetError::Absolute {
                        value: value.to_string(),
                    });
                }
            }
        }

        let leaf = path
            .file_name()
            .and_then(|part| part.to_str())
            .ok_or_else(|| SkillTargetError::MissingName {
                value: value.to_string(),
            })?;
        let name = SkillName::parse(leaf)?;

        Ok(Self { path, name })
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    pub fn name(&self) -> &SkillName {
        &self.name
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct CreateSkillRequest {
    workspace_root: PathBuf,
    target: SkillTarget,
}

impl CreateSkillRequest {
    pub fn new(workspace_root: impl Into<PathBuf>, target: SkillTarget) -> Self {
        Self {
            workspace_root: workspace_root.into(),
            target,
        }
    }
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub struct CreatedSkill {
    relative_skill_file: PathBuf,
    absolute_skill_file: PathBuf,
}

impl CreatedSkill {
    pub fn relative_skill_file(&self) -> &Path {
        &self.relative_skill_file
    }

    pub fn absolute_skill_file(&self) -> &Path {
        &self.absolute_skill_file
    }
}

pub fn create_skill(request: CreateSkillRequest) -> Result<CreatedSkill, CreateSkillError> {
    let relative_target = request.target.path();
    let absolute_target = request.workspace_root.join(relative_target);
    if let Some(parent) = absolute_target
        .parent()
        .filter(|parent| !parent.as_os_str().is_empty())
    {
        fs::create_dir_all(parent).map_err(|source| CreateSkillError::CreateParent {
            path: parent.to_path_buf(),
            source,
        })?;
    }

    fs::create_dir(&absolute_target).map_err(|source| {
        if source.kind() == io::ErrorKind::AlreadyExists {
            CreateSkillError::AlreadyExists {
                path: relative_target.to_path_buf(),
            }
        } else {
            CreateSkillError::CreateTarget {
                path: absolute_target.clone(),
                source,
            }
        }
    })?;

    let relative_skill_file = relative_target.join("SKILL.md");
    let absolute_skill_file = absolute_target.join("SKILL.md");
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&absolute_skill_file)
        .map_err(|source| CreateSkillError::CreateSkillFile {
            path: absolute_skill_file.clone(),
            source,
        })?;
    file.write_all(render_skill(request.target.name()).as_bytes())
        .map_err(|source| CreateSkillError::WriteSkillFile {
            path: absolute_skill_file.clone(),
            source,
        })?;

    Ok(CreatedSkill {
        relative_skill_file,
        absolute_skill_file,
    })
}

fn render_skill(name: &SkillName) -> String {
    SKILL_TEMPLATE.replace("{name}", name.as_str())
}

fn is_valid_skill_name(value: &str) -> bool {
    let bytes = value.as_bytes();
    let Some(first) = bytes.first() else {
        return false;
    };
    if !first.is_ascii_lowercase() && !first.is_ascii_digit() {
        return false;
    }

    let mut previous_hyphen = false;
    for byte in bytes {
        match *byte {
            b'a'..=b'z' | b'0'..=b'9' => previous_hyphen = false,
            b'-' if !previous_hyphen => previous_hyphen = true,
            _ => return false,
        }
    }
    !previous_hyphen
}

#[derive(Debug, Clone, Eq, PartialEq)]
pub enum SkillNameError {
    Empty,
    Invalid { value: String },
}

impl fmt::Display for SkillNameError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => write!(f, "skill name must not be empty"),
            Self::Invalid { value } => {
                write!(f, "skill name {value:?} must match [a-z0-9]+(-[a-z0-9]+)*")
            }
        }
    }
}

impl Error for SkillNameError {}

#[derive(Debug, Clone, Eq, PartialEq)]
pub enum SkillTargetError {
    Empty,
    Absolute { value: String },
    Parent { value: String },
    MissingName { value: String },
    Name(SkillNameError),
}

impl From<SkillNameError> for SkillTargetError {
    fn from(value: SkillNameError) -> Self {
        Self::Name(value)
    }
}

impl fmt::Display for SkillTargetError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => write!(f, "skill target must not be empty"),
            Self::Absolute { value } => {
                write!(f, "skill target {value:?} must be relative")
            }
            Self::Parent { value } => {
                write!(f, "skill target {value:?} must not contain '..'")
            }
            Self::MissingName { value } => {
                write!(f, "skill target {value:?} must include a skill name")
            }
            Self::Name(source) => source.fmt(f),
        }
    }
}

impl Error for SkillTargetError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::Name(source) => Some(source),
            Self::Empty
            | Self::Absolute { .. }
            | Self::Parent { .. }
            | Self::MissingName { .. } => None,
        }
    }
}

#[derive(Debug)]
pub enum CreateSkillError {
    AlreadyExists { path: PathBuf },
    CreateParent { path: PathBuf, source: io::Error },
    CreateTarget { path: PathBuf, source: io::Error },
    CreateSkillFile { path: PathBuf, source: io::Error },
    WriteSkillFile { path: PathBuf, source: io::Error },
}

impl fmt::Display for CreateSkillError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::AlreadyExists { path } => {
                write!(
                    f,
                    "'{}' already exists; refusing to overwrite",
                    path.display()
                )
            }
            Self::CreateParent { path, source } => {
                write!(f, "create parent '{}': {source}", path.display())
            }
            Self::CreateTarget { path, source } => {
                write!(f, "create '{}': {source}", path.display())
            }
            Self::CreateSkillFile { path, source } => {
                write!(f, "create '{}': {source}", path.display())
            }
            Self::WriteSkillFile { path, source } => {
                write!(f, "write '{}': {source}", path.display())
            }
        }
    }
}

impl Error for CreateSkillError {
    fn source(&self) -> Option<&(dyn Error + 'static)> {
        match self {
            Self::AlreadyExists { .. } => None,
            Self::CreateParent { source, .. }
            | Self::CreateTarget { source, .. }
            | Self::CreateSkillFile { source, .. }
            | Self::WriteSkillFile { source, .. } => Some(source),
        }
    }
}
