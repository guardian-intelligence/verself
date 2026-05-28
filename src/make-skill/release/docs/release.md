# make-skill release build

make-skill owns the package-specific release command that produces
side-effect-free build bundles. distribution-service admits and serves
immutable OCI artifacts after a trusted publisher has pushed bytes and standard
evidence to Zot.

## Build

```text
aspect release mksk \
  --channel=nightly \
  --version=0.2.0-nightly.20260527.1 \
  --source-ref=HEAD \
  --platform=linux/amd64 \
  --flavor=default
```

`build` requires a complete release subject:

```text
package=mksk
version=<SemVer or channel prerelease>
channel=nightly|rc|stable
source_commit=<40-char git commit>
platform=linux/amd64
flavor=<opaque token>
```

The command runs the package-owned Bazel tests and `//src/make-skill:release_tar`
with `MKSK_RELEASE_VERSION` set to the subject version. It emits:

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
