use make_skill_exec::{ExitStatus, run};

#[test]
fn prints_usage_without_args() {
    let out = run([]);
    assert_eq!(out.status, ExitStatus::Failure);
    assert!(out.stderr.contains("usage: mksk <skill-name>"));
}

#[test]
fn rejects_extra_args() {
    let out = run(["one".to_string(), "two".to_string()]);
    assert_eq!(out.status, ExitStatus::Failure);
    assert!(out.stderr.contains("unexpected argument \"two\""));
}

#[test]
fn rejects_invalid_skill_name() {
    let out = run(["BadName".to_string()]);
    assert_eq!(out.status, ExitStatus::Failure);
    assert!(out.stderr.contains("must match"));
}
