# make-skill release tooling

make-skill owns the package-specific release command that produces
side-effect-free build bundles. distribution-service admits and serves
immutable OCI artifacts after a trusted publisher has pushed bytes and standard
evidence to Zot.

## Command catalog

Use intent commands for normal operation:

```text
aspect release mksk --nightly --source-ref=main
aspect release mksk --rc --source-ref=HEAD
aspect release mksk --stable --source-ref=<rc-source-sha>
aspect release mksk --stable --from-rc=mksk-v0.2.0-rc.2
```

Use exact-subject commands for retries and diagnostics:

```text
aspect release mksk \
  --channel=nightly \
  --version=0.2.0-nightly.20260527.1 \
  --source-ref=HEAD \
  --platform=linux/amd64 \
  --flavor=default
```

`--publish` selects `mksk-release publish`. It builds the same bundle, publishes
`artifact/make-skill.tar` as the OCI subject manifest, attaches SPDX, SLSA,
license, and test evidence as OCI referrers, and verifies the pushed descriptor
graph before returning.

The trusted-host defaults point at the local Zot listener:

```text
aspect release mksk --nightly --source-ref=main --publish
```

For diagnostics against a registry without authentication, pass both registry
credential flags as empty values. The production path uses the Zot publisher
identity:

```text
--registry=http://127.0.0.1:5080
--repository=verself/mksk
--registry-username=artifact-publisher
--registry-password-file=/etc/zot/publisher-password
```

`--signing=disabled` is the current tracer-bullet mode. `--signing=openbao-transit`
validates the OpenBao Transit configuration and then fails before building or
publishing; the signer boundary is in place for the next changeset that calls
Transit after OCI referrer verification.

## Release subject

After command parsing, every path becomes a complete release subject:

```text
package=mksk
version=<SemVer or channel prerelease>
channel=nightly|rc|stable
source_repository=https://github.com/guardian-intelligence/verself.git
source_ref=<operator input, exact source ref, or RC tag>
source_commit=<40-char git commit>
platform=linux/amd64
flavor=<opaque token>
```

## Version derivation

`mksk-release build` and `mksk-release publish` both support intent flags. There
is no public planning subcommand.

- `--nightly` reads `[workspace.package].version` from
  `src/make-skill/Cargo.toml` at the resolved source commit and derives
  `<base>-nightly.YYYYMMDD.N`.
- `--rc` reads the same base version and derives the next
  `<base>-rc.N` from existing `mksk-v<base>-rc.N` git tags and local release
  bundles. If the resolved commit already has an RC tag, that RC version is
  reused.
- `--stable --from-rc=<tag>` resolves the RC tag, strips `-rc.N`, and rebuilds
  the RC source commit with the final SemVer.
- `--stable --source-ref=<ref>` reads the final SemVer base from the resolved
  source commit.

Exact `--channel/--version` builds do not derive versions. They only resolve
source authority and validate the provided subject.

## Build bundle

The command runs the package-owned Bazel tests and
`//src/make-skill:release_tar` with `MKSK_RELEASE_VERSION` set to the subject
version. It emits:

```text
artifact/make-skill.tar
sbom/make-skill.artifact.spdx.json
sbom/make-skill.source.spdx.json
licenses/make-skill.cargo-about.json
evidence/make-skill.provenance.intoto.json
tests/*.xml
checksums.sha256
```

Stable builds use the selected RC source commit and a final SemVer. They do not
reuse RC bytes because the version is embedded in the binary.
