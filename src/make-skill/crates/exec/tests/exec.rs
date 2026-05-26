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
fn prints_release_metadata_url() {
    let out = run(["--version".to_string()]);
    assert_eq!(out.status, ExitStatus::Success);
    let mut lines = out.stdout.lines();
    let version = lines
        .next()
        .and_then(|line| line.strip_prefix("mksk "))
        .expect("first line should contain mksk version");
    let want_release = format!("release: https://oci.verself.sh/releases/mksk/{version}");
    assert_eq!(lines.next(), Some(want_release.as_str()));
    assert_eq!(lines.next(), None);
}

#[test]
fn version_subcommand_uses_same_release_metadata_output() {
    let flag = run(["--version".to_string()]);
    let subcommand = run(["version".to_string()]);
    assert_eq!(subcommand.status, ExitStatus::Success);
    assert_eq!(subcommand.stdout, flag.stdout);
}

#[test]
fn rejects_invalid_skill_name() {
    let out = run(["BadName".to_string()]);
    assert_eq!(out.status, ExitStatus::Failure);
    assert!(out.stderr.contains("must match"));
}
