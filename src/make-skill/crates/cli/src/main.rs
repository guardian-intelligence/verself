//! make-skill (`mksk`): scaffold a new agent skill in one command.

use std::io::Write;
use std::process::ExitCode;

fn main() -> ExitCode {
    let out = make_skill_exec::run(std::env::args().skip(1));
    if let Err(err) = write_all(std::io::stdout().lock(), out.stdout.as_bytes()) {
        eprintln!("error: write stdout: {err}");
        return ExitCode::FAILURE;
    }
    if write_all(std::io::stderr().lock(), out.stderr.as_bytes()).is_err() {
        return ExitCode::FAILURE;
    }
    out.status.into_exit_code()
}

fn write_all(mut writer: impl Write, bytes: &[u8]) -> std::io::Result<()> {
    writer.write_all(bytes)
}
