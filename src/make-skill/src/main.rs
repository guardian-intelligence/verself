//! make-skill (`mksk`): scaffold a new agent skill in one command.
//!
//! `mksk <name>` creates `<name>/SKILL.md`. Zero dependencies, zero config.

use std::env;
use std::fs;
use std::path::Path;
use std::process::ExitCode;

/// SKILL.md scaffold. `{name}` is substituted with the skill's leaf name.
const SKILL_TEMPLATE: &str = "\
---
name: {name}
description: TODO — one sentence on when an agent should use this skill.
---

# {name}

TODO — write the skill instructions here.
";

fn main() -> ExitCode {
    let Some(target) = env::args().nth(1).filter(|s| !s.is_empty()) else {
        eprintln!("usage: mksk <skill-name>");
        return ExitCode::FAILURE;
    };

    let dir = Path::new(&target);
    // Refuse to clobber: an existing path is a loud failure, not a silent merge.
    if dir.exists() {
        eprintln!("error: '{target}' already exists; refusing to overwrite");
        return ExitCode::FAILURE;
    }

    // Frontmatter name is the leaf, so `a/b/my-skill` still names `my-skill`.
    let name = dir.file_name().and_then(|s| s.to_str()).unwrap_or(&target);

    if let Err(e) = fs::create_dir_all(dir) {
        eprintln!("error: create '{target}': {e}");
        return ExitCode::FAILURE;
    }

    let skill = dir.join("SKILL.md");
    if let Err(e) = fs::write(&skill, SKILL_TEMPLATE.replace("{name}", name)) {
        eprintln!("error: write '{}': {e}", skill.display());
        return ExitCode::FAILURE;
    }

    println!("created {}", skill.display());
    ExitCode::SUCCESS
}
