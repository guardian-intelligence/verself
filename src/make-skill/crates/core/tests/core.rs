use std::fs;
use std::path::PathBuf;
use std::sync::atomic::{AtomicUsize, Ordering};

use make_skill_core::{CreateSkillRequest, SkillTarget, create_skill};

static NEXT_ID: AtomicUsize = AtomicUsize::new(0);

fn workdir(tag: &str) -> PathBuf {
    let id = NEXT_ID.fetch_add(1, Ordering::Relaxed);
    let dir = std::env::temp_dir().join(format!(
        "make-skill-core-test-{}-{tag}-{id}",
        std::process::id()
    ));
    let _ = fs::remove_dir_all(&dir);
    fs::create_dir_all(&dir).unwrap();
    dir
}

#[test]
fn creates_nested_skill_scaffold() {
    let work = workdir("nested");

    let created = create_skill(CreateSkillRequest::new(
        &work,
        SkillTarget::parse("agents/my-skill").unwrap(),
    ))
    .unwrap();

    assert_eq!(
        created.relative_skill_file(),
        PathBuf::from("agents/my-skill/SKILL.md")
    );
    let body = fs::read_to_string(created.absolute_skill_file()).unwrap();
    assert!(body.starts_with("---\n"));
    assert!(body.contains("name: my-skill"));
    assert!(body.contains("# my-skill"));
}

#[test]
fn refuses_existing_directory() {
    let work = workdir("existing-dir");
    let target = work.join("dup");
    fs::create_dir(&target).unwrap();

    let err = create_skill(CreateSkillRequest::new(
        &work,
        SkillTarget::parse(target.file_name().unwrap().to_str().unwrap()).unwrap(),
    ))
    .unwrap_err();

    assert!(err.to_string().contains("already exists"));
}

#[test]
fn rejects_unsafe_targets() {
    for value in [
        "",
        "/tmp/my-skill",
        "../my-skill",
        ".",
        "BadName",
        "bad--name",
        "bad-",
    ] {
        assert!(
            SkillTarget::parse(value).is_err(),
            "{value} should be invalid"
        );
    }
}
