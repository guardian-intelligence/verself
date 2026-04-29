// versions.cue - pinned versions and binary manifests for platform SBOM surfaces.
package catalog

// #DevToolTier categorises how a dev tool reaches the controller. Values
// other than `legacy_install_plan` are migration targets; the end state has
// no entries on the legacy tier and `roles/dev_tools/` is reduced to the
// per-tier-driven tasks (Bazel untar / uv sync). See
// `src/cue-renderer/AGENTS.md` for the per-tier delivery contract.
//
// Apt packages live in the top-level `systemPackages` block, not in
// devTools, because they are not version-pinned by sha256 — the
// `risk_acknowledgement` field forces an explicit declaration that we
// trust upstream Ubuntu archive integrity for the affected entries.
#DevToolTier:
	"pinned_http_file" |
	"source_built_go" |
	"lockfile_uv" |
	"bootstrap_pivot" |
	"legacy_install_plan"

// #SystemPackage describes an apt-managed system package. The risk
// acknowledgement is mandatory: every entry must explicitly state why
// we accept apt's lack of content pinning. The pattern requires the
// substring "upstream" so an empty or boilerplate string fails CUE
// evaluation rather than passing silently.
#SystemPackage: {
	risk_acknowledgement: string & =~"upstream"
	apt_version_constraint?: string
	version_cmd:             string & !=""
}

versions: {
	production: {
		platform:                    "0.1.0"
		ubuntu:                      "24.04"
		ubuntuBase:                  "24.04.4"
		postgresql:                  "16"
		clickhouse:                  "26.3.2.3"
		tigerbeetle:                 "0.17.1"
		zitadel:                     "4.13.1"
		openbao:                     "2.5.2"
		spire:                       "1.14.5"
		spiffeHelper:                "0.11.0"
		natsServer:                  "2.12.7"
		garage:                      "v2.3.0"
		forgejo:                     "15.0.0"
		otelcolContrib:              "0.149.0"
		temporal:                    "1.30.4"
		grafana:                     "12.4.2"
		grafanaClickhouseDatasource: "4.14.1"
		containerd:                  "2.2.2"
		nodejs:                      "22.22.2"
		caddy:                       "2.11.2"
		xcaddy:                      "0.4.5"
		corazaCaddy:                 "2.4.0"
		stalwart:                    "0.15.5"
		stalwartCli:                 "0.15.5"
		stalwartWebadmin:            "0.1.37"
		stalwartSpamFilter:          "2.0.5"
		bazelRemote:                 "2.6.1"
		electric:                    "1.5.0"
		firecracker:                 "1.15.0"
		guestKernel:                 "6.1.155"
		guestGo:                     "1.25.8"
		guestNodejs:                 "24.15.0"
		pnpm:                        "10.33.0"
		githubActionsRunner:         "2.333.1"
		forgejoRunner:               "12.9.0"
	}

	development: {
		go:                                        "1.25.8"
		bazel:                                     "9.1.0"
		bazelisk:                                  "1.28.1"
		buildifier:                                "8.5.1"
		zig:                                       "0.15.2"
		opentofu:                                  "1.11.5"
		ansibleCore:                               "2.20.3"
		ansibleLint:                               "26.4.0"
		ansibleOpentelemetrySdk:                   "1.41.0"
		ansibleOpentelemetryExporterOtlpProtoGrpc: "1.41.0"
		ansibleOpentelemetryExporterOtlpProtoHttp: "1.41.0"
		preCommit:                                 "4.5.1"
		protoc:                                    "34.0"
		cue:                                       "0.16.1"
		buf:                                       "1.66.1"
		shellcheck:                                "0.11.0"
		jq:                                        "1.8.1"
		sops:                                      "3.12.2"
		age:                                       "1.3.1"
		uv:                                        "0.11.3"
		clickhouse:                                "26.3.2.3"
		golangciLint:                              "2.11.3"
		gosec:                                     "2.25.0"
		gofumpt:                                   "0.9.2"
		sqlc:                                      "1.30.0"
		protocGenGo:                               "1.36.11"
		protocGenGoGrpc:                           "1.6.1"
		crun:                                      "1.14.1"
		debootstrap:                               "1.0.134"
		guarddog:                                  "2.9.0"
		osvScanner:                                "2.3.5"
		stripe:                                    "1.40.0"
		agentBrowser:                              "0.25.4"
		latitudeProvider:                          "2.9.4"
		pnpm:                                      "10.33.0"
	}
}

// serverTools advertises the single Bazel artefact Ansible consumes for the
// pinned third-party server-tool bundle. Per-tool URLs and sha256 sums live
// in MODULE.bazel http_file rules; this block exists so consumers can request
// "the server-tools archive" by Bazel label and detect version drift via the
// version string. The version is a stable composite of every bundled tool's
// pinned version, so any catalog bump produces a new value and forces a
// re-unpack on the host.
serverTools: {
	bazel_label: "//src/cue-renderer/binaries:server_tools.tar.zst"
	version:     "clickhouse-\(versions.production.clickhouse)_tigerbeetle-\(versions.production.tigerbeetle)_zitadel-\(versions.production.zitadel)_openbao-\(versions.production.openbao)_spire-\(versions.production.spire)_spiffe-helper-\(versions.production.spiffeHelper)_nats-server-\(versions.production.natsServer)_garage-\(versions.production.garage)_forgejo-\(versions.production.forgejo)_bazel-remote-\(versions.production.bazelRemote)_otelcol-contrib-\(versions.production.otelcolContrib)_temporal-\(versions.production.temporal)_grafana-\(versions.production.grafana)_grafana-clickhouse-datasource-\(versions.production.grafanaClickhouseDatasource)_containerd-\(versions.production.containerd)_nodejs-\(versions.production.nodejs)_stalwart-\(versions.production.stalwart)_stalwart-cli-\(versions.production.stalwartCli)_caddy-\(versions.production.caddy)"
}

// devToolsArchive is the dev-tools twin of serverTools: the single Bazel
// label Ansible's bridge requests, plus a composite version that flips
// whenever any pinned_http_file dev tool is bumped. Forces a re-unpack
// on the controller when any version moves.
devToolsArchive: {
	bazel_label: "//src/cue-renderer/binaries:dev_tools.tar.zst"
	version:     "age-\(versions.development.age)_agent-browser-\(versions.development.agentBrowser)_buf-\(versions.development.buf)_buildifier-\(versions.development.buildifier)_clickhouse-\(versions.development.clickhouse)_cue-\(versions.development.cue)_go-\(versions.development.go)_jq-\(versions.development.jq)_osv-scanner-\(versions.development.osvScanner)_protoc-\(versions.development.protoc)_shellcheck-\(versions.development.shellcheck)_sops-\(versions.development.sops)_stripe-\(versions.development.stripe)_tofu-\(versions.development.opentofu)_uv-\(versions.development.uv)_zig-\(versions.development.zig)"
}

// lockfileUvTools: per-project uv project layouts. Each project lives
// at `src/platform/uv-tools/<name>/` with a committed pyproject.toml +
// uv.lock pair. The dev_tools role runs `uv sync --frozen --project
// <project_dir>` for each entry, then symlinks every entrypoint into
// /usr/local/bin/. Per-tool version_check spans (filtered on
// tier=lockfile_uv) confirm the symlinked entrypoint reports the
// CUE-pinned version after sync.
//
// Adding a new Python tool:
//   1. mkdir src/platform/uv-tools/<name> && cd it
//   2. uv init && uv add <pkg>==<version>; commit pyproject.toml+uv.lock
//   3. add a row here with project_dir + entrypoints
//   4. add tier=lockfile_uv devTool entries for any entrypoints that
//      should appear in the version_check gate
lockfileUvTools: {
	"ansible-core": {
		project_dir: "src/platform/uv-tools/ansible-core"
		version:     versions.development.ansibleCore
		// Every entrypoint that should land at /usr/local/bin/. Includes
		// ansible-lint and the playbook/galaxy/etc. subcommands; OTel
		// runtime imports are libraries, not entrypoints, so they don't
		// appear here.
		entrypoints: [
			"ansible",
			"ansible-config",
			"ansible-console",
			"ansible-doc",
			"ansible-galaxy",
			"ansible-inventory",
			"ansible-lint",
			"ansible-playbook",
			"ansible-pull",
			"ansible-vault",
		]
	}
	"pre-commit": {
		project_dir: "src/platform/uv-tools/pre-commit"
		version:     versions.development.preCommit
		entrypoints: ["pre-commit"]
	}
	guarddog: {
		project_dir: "src/platform/uv-tools/guarddog"
		version:     versions.development.guarddog
		entrypoints: ["guarddog"]
	}
}

// systemPackages: apt-managed packages that intentionally bypass content
// pinning. Each entry must declare `risk_acknowledgement` containing the
// substring "upstream"; the schema rejects empty or boilerplate strings.
// The dev_tools role iterates this map verbatim; no install-plan
// projection is involved.
systemPackages: [Name=string]: #SystemPackage
systemPackages: {
	"build-essential": {
		risk_acknowledgement: "build-essential is a Debian metapackage that has no version of its own; we accept upstream Ubuntu's archive integrity for the gcc/g++/make versions it pulls in."
		version_cmd:          "gcc --version"
	}
	crun: {
		apt_version_constraint: versions.development.crun
		risk_acknowledgement:   "crun is consumed by Firecracker tooling; we accept upstream Ubuntu's archive integrity in exchange for kernel-matched OCI runtime defaults that a Bazel http_file rule would not give us."
		version_cmd:            "crun --version"
	}
	debootstrap: {
		apt_version_constraint: versions.development.debootstrap
		risk_acknowledgement:   "debootstrap is the bootstrap utility for guest rootfs builds; we accept upstream Ubuntu's archive integrity because the binary's only output is a chroot tree we then sha-pin downstream."
		version_cmd:            "debootstrap --version"
	}
}

serverToolDownloads: {
	clickhouse: {
		name:                 "server_tool_clickhouse"
		downloaded_file_path: "clickhouse-common-static-\(versions.production.clickhouse)-amd64.tgz"
		sha256:               "2b0ccdc84bc3cc624408a8a490181c6eed6b8df4e090f9b4ed7e647e46093278"
		url:                  "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-\(versions.production.clickhouse)-amd64.tgz"
	}
	tigerbeetle: {
		name:                 "server_tool_tigerbeetle"
		downloaded_file_path: "tigerbeetle-x86_64-linux.zip"
		sha256:               "0071b5a86876afffea067851f26a90b1e6bb60968fe8afbf8121ea55d4a7af19"
		url:                  "https://github.com/tigerbeetle/tigerbeetle/releases/download/\(versions.production.tigerbeetle)/tigerbeetle-x86_64-linux.zip"
	}
	zitadel: {
		name:                 "server_tool_zitadel"
		downloaded_file_path: "zitadel-linux-amd64.tar.gz"
		sha256:               "fe1f5231e5dcbdca63ae77adab0d2241daafeb9712e7d6cded3713e9ef50f1cb"
		url:                  "https://github.com/zitadel/zitadel/releases/download/v\(versions.production.zitadel)/zitadel-linux-amd64.tar.gz"
	}
	openbao: {
		name:                 "server_tool_openbao"
		downloaded_file_path: "openbao_\(versions.production.openbao)_linux_amd64.deb"
		sha256:               "5b915011ba8fa8137bd3309830aded0250b8fce42706bd6dcd2b91ac5560cde7"
		url:                  "https://github.com/openbao/openbao/releases/download/v\(versions.production.openbao)/openbao_\(versions.production.openbao)_linux_amd64.deb"
	}
	spire: {
		name:                 "server_tool_spire"
		downloaded_file_path: "spire-\(versions.production.spire)-linux-amd64-musl.tar.gz"
		sha256:               "cacab9ff32b7a24714edcf4328c4ce27bddd38496b443e8750395883c72a3bfb"
		url:                  "https://github.com/spiffe/spire/releases/download/v\(versions.production.spire)/spire-\(versions.production.spire)-linux-amd64-musl.tar.gz"
	}
	spiffe_helper: {
		name:                 "server_tool_spiffe_helper"
		downloaded_file_path: "spiffe-helper_v\(versions.production.spiffeHelper)_Linux-x86_64.tar.gz"
		sha256:               "7fba909574320d6a656e2e7d7f0657890fefad08a2abecd86c7bafe62d6d9134"
		url:                  "https://github.com/spiffe/spiffe-helper/releases/download/v\(versions.production.spiffeHelper)/spiffe-helper_v\(versions.production.spiffeHelper)_Linux-x86_64.tar.gz"
	}
	nats_server: {
		name:                 "server_tool_nats_server"
		downloaded_file_path: "nats-server-v\(versions.production.natsServer)-linux-amd64.tar.gz"
		sha256:               "570d2d627db111e679cc1e6bc57ba78f373ed1769acd8dc9c21c8f62d15b3c52"
		url:                  "https://github.com/nats-io/nats-server/releases/download/v\(versions.production.natsServer)/nats-server-v\(versions.production.natsServer)-linux-amd64.tar.gz"
	}
	garage: {
		name:                 "server_tool_garage"
		downloaded_file_path: "garage"
		sha256:               "f98d317942bb341151a2775162016bb50cf86b865d0108de03eb5db16e2120cd"
		url:                  "https://garagehq.deuxfleurs.fr/_releases/\(versions.production.garage)/x86_64-unknown-linux-musl/garage"
	}
	forgejo: {
		name:                 "server_tool_forgejo"
		downloaded_file_path: "forgejo-\(versions.production.forgejo)-linux-amd64"
		sha256:               "3919f10a7845f3b71bacc2c7a3bfa2cd71aed58a0b8be6ab5e95f2e150b4ded7"
		url:                  "https://codeberg.org/forgejo/forgejo/releases/download/v\(versions.production.forgejo)/forgejo-\(versions.production.forgejo)-linux-amd64"
	}
	bazel_remote: {
		name:                 "server_tool_bazel_remote"
		downloaded_file_path: "bazel-remote-\(versions.production.bazelRemote)-linux-amd64"
		sha256:               "025d53aeb03a7fdd4a0e76262a5ae9eeee9f64d53ca510deff1c84cf3f276784"
		url:                  "https://github.com/buchgr/bazel-remote/releases/download/v\(versions.production.bazelRemote)/bazel-remote-\(versions.production.bazelRemote)-linux-amd64"
	}
	otelcol_contrib: {
		name:                 "server_tool_otelcol_contrib"
		downloaded_file_path: "otelcol-contrib_\(versions.production.otelcolContrib)_linux_amd64.tar.gz"
		sha256:               "4acb57355e9388f257b28de8c18422ff43e52eb329052bd54ebecde000dcbb47"
		url:                  "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v\(versions.production.otelcolContrib)/otelcol-contrib_\(versions.production.otelcolContrib)_linux_amd64.tar.gz"
	}
	temporal: {
		name:                 "server_tool_temporal"
		downloaded_file_path: "temporal_\(versions.production.temporal)_linux_amd64.tar.gz"
		sha256:               "83f900fe8f9fd23c0e6369041355d2edbd768a91667dd6bc22a98d8316632177"
		url:                  "https://github.com/temporalio/temporal/releases/download/v\(versions.production.temporal)/temporal_\(versions.production.temporal)_linux_amd64.tar.gz"
	}
	grafana: {
		name:                 "server_tool_grafana"
		downloaded_file_path: "grafana-\(versions.production.grafana).linux-amd64.tar.gz"
		sha256:               "f240b200a803bf64592fae645331750c2681df5f07496743d714db12393a591f"
		url:                  "https://dl.grafana.com/oss/release/grafana-\(versions.production.grafana).linux-amd64.tar.gz"
	}
	grafana_clickhouse_datasource: {
		name:                 "server_tool_grafana_clickhouse_datasource"
		downloaded_file_path: "grafana-clickhouse-datasource-\(versions.production.grafanaClickhouseDatasource).linux_amd64.zip"
		sha256:               "11f569287a607043a9c60c4abca493784cc187b6bc4298270e5ef764719a22f1"
		url:                  "https://github.com/grafana/clickhouse-datasource/releases/download/v\(versions.production.grafanaClickhouseDatasource)/grafana-clickhouse-datasource-\(versions.production.grafanaClickhouseDatasource).linux_amd64.zip"
	}
	stalwart: {
		name:                 "server_tool_stalwart"
		downloaded_file_path: "stalwart-x86_64-unknown-linux-musl.tar.gz"
		sha256:               "b2042dbcf0a110a4a756a5288de013649fd1f7ee84fa002bb3d2e6ec1e5f1f0b"
		url:                  "https://github.com/stalwartlabs/stalwart/releases/download/v\(versions.production.stalwart)/stalwart-x86_64-unknown-linux-musl.tar.gz"
	}
	stalwart_cli: {
		name:                 "server_tool_stalwart_cli"
		downloaded_file_path: "stalwart-cli-x86_64-unknown-linux-musl.tar.gz"
		sha256:               "8fbe74206bed46974c272623e184b11d6c8362eb606dddc155fdb6ae9df7b3e9"
		url:                  "https://github.com/stalwartlabs/stalwart/releases/download/v\(versions.production.stalwartCli)/stalwart-cli-x86_64-unknown-linux-musl.tar.gz"
	}
	containerd: {
		name:                 "server_tool_containerd"
		downloaded_file_path: "containerd-static-\(versions.production.containerd)-linux-amd64.tar.gz"
		sha256:               "5db46232ce716f85bf1e71497a9038c87e63030574bf03f9d09557802188ad27"
		url:                  "https://github.com/containerd/containerd/releases/download/v\(versions.production.containerd)/containerd-static-\(versions.production.containerd)-linux-amd64.tar.gz"
	}
	nodejs: {
		name:                 "server_tool_nodejs"
		downloaded_file_path: "node-v\(versions.production.nodejs)-linux-x64.tar.xz"
		sha256:               "88fd1ce767091fd8d4a99fdb2356e98c819f93f3b1f8663853a2dee9b438068a"
		url:                  "https://nodejs.org/dist/v\(versions.production.nodejs)/node-v\(versions.production.nodejs)-linux-x64.tar.xz"
	}
}

serverToolPackaging: {
	profile_bin:                           "opt/verself/profile/bin"
	grafana_clickhouse_datasource_version: versions.production.grafanaClickhouseDatasource
	tar_single: [
		{name: "clickhouse", repo: "server_tool_clickhouse", tar_flag: "z", binary: "clickhouse-common-static-*/usr/bin/clickhouse", dest: "clickhouse"},
		{name: "zitadel", repo: "server_tool_zitadel", tar_flag: "z", binary: "zitadel-linux-amd64/zitadel", dest: "zitadel"},
		{name: "spire_server", repo: "server_tool_spire", tar_flag: "z", binary: "spire-*/bin/spire-server", dest: "spire-server"},
		{name: "spire_agent", repo: "server_tool_spire", tar_flag: "z", binary: "spire-*/bin/spire-agent", dest: "spire-agent"},
		{name: "spiffe_helper", repo: "server_tool_spiffe_helper", tar_flag: "z", binary: "spiffe-helper", dest: "spiffe-helper"},
		{name: "nats_server", repo: "server_tool_nats_server", tar_flag: "z", binary: "nats-server-*-linux-amd64/nats-server", dest: "nats-server"},
		{name: "otelcol_contrib", repo: "server_tool_otelcol_contrib", tar_flag: "z", binary: "otelcol-contrib", dest: "otelcol-contrib"},
		{name: "temporal_server", repo: "server_tool_temporal", tar_flag: "z", binary: "temporal-server", dest: "temporal-server"},
		{name: "temporal_sql_tool", repo: "server_tool_temporal", tar_flag: "z", binary: "temporal-sql-tool", dest: "temporal-sql-tool"},
		{name: "tdbg", repo: "server_tool_temporal", tar_flag: "z", binary: "tdbg", dest: "tdbg"},
		{name: "stalwart", repo: "server_tool_stalwart", tar_flag: "z", binary: "stalwart", dest: "stalwart"},
		{name: "stalwart_cli", repo: "server_tool_stalwart_cli", tar_flag: "z", binary: "stalwart-cli", dest: "stalwart-cli"},
		{name: "containerd", repo: "server_tool_containerd", tar_flag: "z", binary: "bin/containerd", dest: "containerd"},
	]
	zip_single: [
		{name: "tigerbeetle", repo: "server_tool_tigerbeetle", binary: "tigerbeetle", dest: "tigerbeetle"},
	]
	deb_member: [
		{name: "openbao", repo: "server_tool_openbao", binary: "usr/bin/bao", dest: "bao"},
	]
	raw: [
		{name: "garage", repo: "server_tool_garage", dest: "garage"},
		{name: "forgejo", repo: "server_tool_forgejo", dest: "forgejo"},
		{name: "bazel_remote", repo: "server_tool_bazel_remote", dest: "bazel-remote"},
	]
	archive_dir: [
		{name: "grafana", repo: "server_tool_grafana", tar_flag: "z", dest: "opt/verself/grafana"},
		{name: "nodejs", repo: "server_tool_nodejs", tar_flag: "J", dest: "opt/verself/nodejs"},
	]
	zip_dir: [
		{name: "grafana_clickhouse_datasource", repo: "server_tool_grafana_clickhouse_datasource", dest: "var/lib/grafana/plugins"},
	]
	symlinks: {
		"opt/verself/profile/bin/clickhouse-benchmark": "/opt/verself/profile/bin/clickhouse"
		"opt/verself/profile/bin/clickhouse-client":    "/opt/verself/profile/bin/clickhouse"
		"opt/verself/profile/bin/clickhouse-keeper":    "/opt/verself/profile/bin/clickhouse"
		"opt/verself/profile/bin/clickhouse-local":     "/opt/verself/profile/bin/clickhouse"
		"opt/verself/profile/bin/clickhouse-server":    "/opt/verself/profile/bin/clickhouse"
		"opt/verself/profile/bin/corepack":             "/opt/verself/nodejs/bin/corepack"
		"opt/verself/profile/bin/grafana":              "/opt/verself/grafana/bin/grafana"
		"opt/verself/profile/bin/node":                 "/opt/verself/nodejs/bin/node"
		"opt/verself/profile/bin/npm":                  "/opt/verself/nodejs/bin/npm"
		"opt/verself/profile/bin/npx":                  "/opt/verself/nodejs/bin/npx"
	}
}

devTools: {
	go: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.go
		strategy:         "tarball"
		url:              "https://go.dev/dl/go\(version).linux-amd64.tar.gz"
		sha256:           "ceb5e041bbc3893846bd1614d76cb4681c91dadee579426cf21a63f2d7e03be6"
		install_dir:      "/usr/local"
		strip_components: 0
		bin_path:         "/usr/local/go/bin/go"
		version_cmd:      "go version"
	}
	zig: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.zig
		strategy:         "tarball"
		url:              "https://ziglang.org/download/\(version)/zig-x86_64-linux-\(version).tar.xz"
		sha256:           "02aa270f183da276e5b5920b1dac44a63f1a49e55050ebde3aecc9eb82f93239"
		install_dir:      "/usr/local/zig"
		strip_components: 1
		bin_path:         "/usr/local/zig/zig"
		symlink:          "/usr/local/bin/zig"
		version_cmd:      "zig version"
	}
	tofu: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.opentofu
		strategy:     "zip"
		url:          "https://github.com/opentofu/opentofu/releases/download/v\(version)/tofu_\(version)_linux_amd64.zip"
		sha256:       "901121681e751574d739de5208cad059eddf9bd739b575745cf9e3c961b28a13"
		install_path: "/usr/local/bin/tofu"
		version_cmd:  "tofu version -json"
	}
	ansible: {
		tier:        #DevToolTier & "lockfile_uv"
		version:     versions.development.ansibleCore
		project:     "ansible-core"
		version_cmd: "ansible --version"
	}
	"ansible-lint": {
		tier:        #DevToolTier & "lockfile_uv"
		version:     versions.development.ansibleLint
		project:     "ansible-core"
		version_cmd: "ansible-lint --version"
	}
	"pre-commit": {
		tier:        #DevToolTier & "lockfile_uv"
		version:     versions.development.preCommit
		project:     "pre-commit"
		version_cmd: "pre-commit --version"
	}
	protoc: {
		tier:        #DevToolTier & "pinned_http_file"
		version:     versions.development.protoc
		strategy:    "zip"
		url:         "https://github.com/protocolbuffers/protobuf/releases/download/v\(version)/protoc-\(version)-linux-x86_64.zip"
		sha256:      "e9a91b6fcfe4177ec2cd35fc8f15c1e811fa0ecdef9372755cd6d3513d5faaab"
		install_dir: "/usr/local"
		version_cmd: "protoc --version"
	}
	cue: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.cue
		strategy:         "tarball"
		url:              "https://github.com/cue-lang/cue/releases/download/v\(version)/cue_v\(version)_linux_amd64.tar.gz"
		sha256:           "5d644c1305a2b86504c8dcd2ec829cf5b4999efc2cf51ee375624e0455f774ae"
		strip_components: 0
		bin_name:         "cue"
		install_path:     "/usr/local/bin/cue"
		version_cmd:      "cue version"
	}
	buf: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.buf
		strategy:     "binary"
		url:          "https://github.com/bufbuild/buf/releases/download/v\(version)/buf-Linux-x86_64"
		sha256:       "ef835cb38ed973849f68e0e6a88153cb2168507e09eecbec43a2ada2f6a698be"
		install_path: "/usr/local/bin/buf"
		version_cmd:  "buf --version"
	}
	bazel: {
		tier:    #DevToolTier & "bootstrap_pivot"
		version: versions.development.bazel
		sha256:  "a667454f3f4f8878df8199136b82c199f6ada8477b337fae3b1ef854f01e4e2f"
	}
	bazelisk: {
		// scripts/bootstrap installs bazelisk before Bazel can run; this entry
		// is the version-of-record for that script's pin and is intentionally
		// excluded from devToolDownloads / devToolPackaging.
		tier:         #DevToolTier & "bootstrap_pivot"
		version:      versions.development.bazelisk
		strategy:     "binary"
		url:          "https://github.com/bazelbuild/bazelisk/releases/download/v\(version)/bazelisk-linux-amd64"
		sha256:       "22e7d3a188699982f661cf4687137ee52d1f24fec1ec893d91a6c4d791a75de8"
		install_path: "/usr/local/bin/bazelisk"
		version_cmd:  "bazelisk version"
	}
	buildifier: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.buildifier
		strategy:     "binary"
		url:          "https://github.com/bazelbuild/buildtools/releases/download/v\(version)/buildifier-linux-amd64"
		sha256:       "887377fc64d23a850f4d18a077b5db05b19913f4b99b270d193f3c7334b5a9a7"
		install_path: "/usr/local/bin/buildifier"
		version_cmd:  "buildifier --version"
	}
	shellcheck: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.shellcheck
		strategy:         "tarball"
		url:              "https://github.com/koalaman/shellcheck/releases/download/v\(version)/shellcheck-v\(version).linux.x86_64.tar.xz"
		sha256:           "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"
		strip_components: 1
		bin_name:         "shellcheck"
		install_path:     "/usr/local/bin/shellcheck"
		version_cmd:      "shellcheck --version"
	}
	jq: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.jq
		strategy:     "binary"
		url:          "https://github.com/jqlang/jq/releases/download/jq-\(version)/jq-linux-amd64"
		sha256:       "020468de7539ce70ef1bceaf7cde2e8c4f2ca6c3afb84642aabc5c97d9fc2a0d"
		install_path: "/usr/local/bin/jq"
		version_cmd:  "jq --version"
	}
	sops: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.sops
		strategy:     "binary"
		url:          "https://github.com/getsops/sops/releases/download/v\(version)/sops-v\(version).linux.amd64"
		sha256:       "14e2e1ba3bef31e74b70cf0b674f6443c80f6c5f3df15d05ffc57c34851b4998"
		install_path: "/usr/local/bin/sops"
		version_cmd:  "sops --version"
	}
	age: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.age
		strategy:         "tarball"
		url:              "https://github.com/FiloSottile/age/releases/download/v\(version)/age-v\(version)-linux-amd64.tar.gz"
		sha256:           "bdc69c09cbdd6cf8b1f333d372a1f58247b3a33146406333e30c0f26e8f51377"
		strip_components: 1
		install_dir:      "/tmp/age-extract"
		bins: ["age", "age-keygen"]
		install_path: "/usr/local/bin/age"
		version_cmd:  "age --version"
	}
	uv: {
		tier:             #DevToolTier & "pinned_http_file"
		version:          versions.development.uv
		strategy:         "tarball"
		url:              "https://github.com/astral-sh/uv/releases/download/\(version)/uv-x86_64-unknown-linux-gnu.tar.gz"
		sha256:           "c0f3236f146e55472663cfbcc9be3042a9f1092275bbe3fe2a56a6cbfd3da5ce"
		strip_components: 1
		install_dir:      "/tmp/uv-extract"
		bins: ["uv", "uvx"]
		install_path: "/usr/local/bin/uv"
		version_cmd:  "uv --version"
	}
	clickhouse: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.clickhouse
		strategy:     "tarball"
		url:          "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-\(version)-amd64.tgz"
		sha256:       "2b0ccdc84bc3cc624408a8a490181c6eed6b8df4e090f9b4ed7e647e46093278"
		install_path: "/usr/local/bin/clickhouse"
		version_cmd:  "clickhouse client --version"
	}
	"golangci-lint": {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.golangciLint
		strategy:    "go_install"
		go_package:  "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
		version_cmd: "golangci-lint --version"
	}
	gosec: {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.gosec
		strategy:    "go_install"
		go_package:  "github.com/securego/gosec/v2/cmd/gosec"
		version_cmd: "go version -m /usr/local/go-tools/bin/gosec"
	}
	gofumpt: {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.gofumpt
		strategy:    "go_install"
		go_package:  "mvdan.cc/gofumpt"
		version_cmd: "gofumpt --version"
	}
	sqlc: {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.sqlc
		strategy:    "go_install"
		go_package:  "github.com/sqlc-dev/sqlc/cmd/sqlc"
		version_cmd: "sqlc version"
	}
	"protoc-gen-go": {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.protocGenGo
		strategy:    "go_install"
		go_package:  "google.golang.org/protobuf/cmd/protoc-gen-go"
		version_cmd: "protoc-gen-go --version"
	}
	"protoc-gen-go-grpc": {
		tier:        #DevToolTier & "legacy_install_plan"
		version:     versions.development.protocGenGoGrpc
		strategy:    "go_install"
		go_package:  "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
		version_cmd: "protoc-gen-go-grpc --version"
	}
	guarddog: {
		tier:        #DevToolTier & "lockfile_uv"
		version:     versions.development.guarddog
		project:     "guarddog"
		version_cmd: "guarddog --version"
	}
	"osv-scanner": {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.osvScanner
		strategy:     "binary"
		url:          "https://github.com/google/osv-scanner/releases/download/v\(version)/osv-scanner_linux_amd64"
		sha256:       "bb30c580afe5e757d3e959f4afd08a4795ea505ef84c46962b9a738aa573b41b"
		install_path: "/usr/local/bin/osv-scanner"
		version_cmd:  "osv-scanner --version"
	}
	stripe: {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.stripe
		strategy:     "tarball"
		url:          "https://github.com/stripe/stripe-cli/releases/download/v\(version)/stripe_\(version)_linux_x86_64.tar.gz"
		sha256:       "a0f9c131d1e06240f97dedfdf05bb9cb96ee33a865e8877807945bab160eaa0f"
		install_path: "/usr/local/bin/stripe"
		version_cmd:  "stripe version"
	}
	"agent-browser": {
		tier:         #DevToolTier & "pinned_http_file"
		version:      versions.development.agentBrowser
		strategy:     "binary"
		url:          "https://github.com/vercel-labs/agent-browser/releases/download/v\(version)/agent-browser-linux-x64"
		sha256:       "02d26f105a9d8e203f8f966acfeb4bab191cfa4625431a535b8be5f8f5905472"
		install_path: "/usr/local/bin/agent-browser"
		version_cmd:  "agent-browser --version"
	}
}

// devToolDownloads mirrors serverToolDownloads: each entry projects to one
// http_file() rule in dev_tools.MODULE.bazel. Only the 16 pinned_http_file
// dev tools appear here; bazelisk is the bootstrap_pivot exception that
// scripts/bootstrap installs directly. url and sha256 reference the
// devTools entry so the version pin lives in exactly one place.
devToolDownloads: {
	go: {
		name:                 "dev_tool_go"
		downloaded_file_path: "go\(versions.development.go).linux-amd64.tar.gz"
		sha256:               devTools.go.sha256
		url:                  devTools.go.url
	}
	zig: {
		name:                 "dev_tool_zig"
		downloaded_file_path: "zig-x86_64-linux-\(versions.development.zig).tar.xz"
		sha256:               devTools.zig.sha256
		url:                  devTools.zig.url
	}
	tofu: {
		name:                 "dev_tool_tofu"
		downloaded_file_path: "tofu_\(versions.development.opentofu)_linux_amd64.zip"
		sha256:               devTools.tofu.sha256
		url:                  devTools.tofu.url
	}
	protoc: {
		name:                 "dev_tool_protoc"
		downloaded_file_path: "protoc-\(versions.development.protoc)-linux-x86_64.zip"
		sha256:               devTools.protoc.sha256
		url:                  devTools.protoc.url
	}
	cue: {
		name:                 "dev_tool_cue"
		downloaded_file_path: "cue_v\(versions.development.cue)_linux_amd64.tar.gz"
		sha256:               devTools.cue.sha256
		url:                  devTools.cue.url
	}
	buf: {
		name:                 "dev_tool_buf"
		downloaded_file_path: "buf-Linux-x86_64"
		sha256:               devTools.buf.sha256
		url:                  devTools.buf.url
	}
	buildifier: {
		name:                 "dev_tool_buildifier"
		downloaded_file_path: "buildifier-linux-amd64"
		sha256:               devTools.buildifier.sha256
		url:                  devTools.buildifier.url
	}
	shellcheck: {
		name:                 "dev_tool_shellcheck"
		downloaded_file_path: "shellcheck-v\(versions.development.shellcheck).linux.x86_64.tar.xz"
		sha256:               devTools.shellcheck.sha256
		url:                  devTools.shellcheck.url
	}
	jq: {
		name:                 "dev_tool_jq"
		downloaded_file_path: "jq-linux-amd64"
		sha256:               devTools.jq.sha256
		url:                  devTools.jq.url
	}
	sops: {
		name:                 "dev_tool_sops"
		downloaded_file_path: "sops-v\(versions.development.sops).linux.amd64"
		sha256:               devTools.sops.sha256
		url:                  devTools.sops.url
	}
	age: {
		name:                 "dev_tool_age"
		downloaded_file_path: "age-v\(versions.development.age)-linux-amd64.tar.gz"
		sha256:               devTools.age.sha256
		url:                  devTools.age.url
	}
	uv: {
		name:                 "dev_tool_uv"
		downloaded_file_path: "uv-x86_64-unknown-linux-gnu.tar.gz"
		sha256:               devTools.uv.sha256
		url:                  devTools.uv.url
	}
	clickhouse: {
		name:                 "dev_tool_clickhouse"
		downloaded_file_path: "clickhouse-common-static-\(versions.development.clickhouse)-amd64.tgz"
		sha256:               devTools.clickhouse.sha256
		url:                  devTools.clickhouse.url
	}
	osv_scanner: {
		name:                 "dev_tool_osv_scanner"
		downloaded_file_path: "osv-scanner_linux_amd64"
		sha256:               devTools["osv-scanner"].sha256
		url:                  devTools["osv-scanner"].url
	}
	stripe: {
		name:                 "dev_tool_stripe"
		downloaded_file_path: "stripe_\(versions.development.stripe)_linux_x86_64.tar.gz"
		sha256:               devTools.stripe.sha256
		url:                  devTools.stripe.url
	}
	agent_browser: {
		name:                 "dev_tool_agent_browser"
		downloaded_file_path: "agent-browser-linux-x64"
		sha256:               devTools["agent-browser"].sha256
		url:                  devTools["agent-browser"].url
	}
}

// devToolPackaging is the shape `dev_tools.tar.zst` lays down on /. Mirrors
// serverToolPackaging. Paths are relative (no leading slash) because the
// tarball gets unpacked at / by Ansible.
devToolPackaging: {
	// One binary extracted from a tarball. tar_flag: z=gz, J=xz.
	tar_single: [
		{name: "cue", repo:        "dev_tool_cue", tar_flag:        "z", binary: "cue", dest:                                                                   "usr/local/bin/cue"},
		{name: "shellcheck", repo: "dev_tool_shellcheck", tar_flag: "J", binary: "shellcheck-v\(versions.development.shellcheck)/shellcheck", dest:             "usr/local/bin/shellcheck"},
		{name: "stripe", repo:     "dev_tool_stripe", tar_flag:     "z", binary: "stripe", dest:                                                                "usr/local/bin/stripe"},
		{name: "clickhouse", repo: "dev_tool_clickhouse", tar_flag: "z", binary: "clickhouse-common-static-\(versions.development.clickhouse)/usr/bin/clickhouse", dest: "usr/local/bin/clickhouse"},
	]

	// One binary extracted from a zip.
	zip_single: [
		{name: "tofu", repo:       "dev_tool_tofu", binary:    "tofu", dest:       "usr/local/bin/tofu"},
		{name: "protoc_bin", repo: "dev_tool_protoc", binary:  "bin/protoc", dest: "usr/local/bin/protoc"},
	]

	// Subdirectory copied from a zip, contents merged into dest. Used for
	// protoc's well-known proto headers.
	zip_directory: [
		{name: "protoc_include", repo: "dev_tool_protoc", src_sub: "include", dest: "usr/local/include"},
	]

	// Multiple binaries from one tarball with --strip-components.
	tar_multi: [
		{name: "age", repo: "dev_tool_age", tar_flag: "z", strip_components: 1, binaries: [
			{member: "age", dest:        "usr/local/bin/age"},
			{member: "age-keygen", dest: "usr/local/bin/age-keygen"},
		]},
		{name: "uv", repo: "dev_tool_uv", tar_flag: "z", strip_components: 1, binaries: [
			{member: "uv", dest:  "usr/local/bin/uv"},
			{member: "uvx", dest: "usr/local/bin/uvx"},
		]},
	]

	// Raw single-file binaries (no archive).
	raw: [
		{name: "buf", repo:           "dev_tool_buf", dest:           "usr/local/bin/buf"},
		{name: "buildifier", repo:    "dev_tool_buildifier", dest:    "usr/local/bin/buildifier"},
		{name: "jq", repo:            "dev_tool_jq", dest:            "usr/local/bin/jq"},
		{name: "sops", repo:          "dev_tool_sops", dest:          "usr/local/bin/sops"},
		{name: "osv_scanner", repo:   "dev_tool_osv_scanner", dest:   "usr/local/bin/osv-scanner"},
		{name: "agent_browser", repo: "dev_tool_agent_browser", dest: "usr/local/bin/agent-browser"},
	]

	// Whole archive extracted into a versioned install dir. Symlinks
	// below pin the canonical path to the version that just landed.
	archive_dir: [
		{name: "go_install", repo:  "dev_tool_go", tar_flag: "z", dest: "usr/local/go-\(versions.development.go)", strip_components:   1},
		{name: "zig_install", repo: "dev_tool_zig", tar_flag: "J", dest: "usr/local/zig-\(versions.development.zig)", strip_components: 1},
	]

	// pkg_tar `symlinks` argument. Keys are tar-internal paths (no leading
	// slash), values are absolute on-disk symlink targets.
	symlinks: {
		"usr/local/go":      "/usr/local/go-\(versions.development.go)"
		"usr/local/zig":     "/usr/local/zig-\(versions.development.zig)"
		"usr/local/bin/zig": "/usr/local/zig/zig"
	}
}

// guestImageDownloads mirrors serverToolDownloads / devToolDownloads:
// each entry projects to one http_file rule emitted into
// src/guest-images/guest_images.MODULE.bazel and is consumed by the
// guest-image build rules under //src/guest-images/. SHA256 + URL stay
// pinned alongside the version in `versions.production`; bumping a pin
// happens here, then `make topology-generate` regenerates the Bazel
// manifest. Composable image catalog (firecracker.images in
// instances/local/config.cue) references the resulting Bazel labels via
// pkg_tar/genrule layouts in //src/guest-images/.
guestImageDownloads: {
	ubuntu_base: {
		name:                 "guest_image_ubuntu_base"
		downloaded_file_path: "ubuntu-base-\(versions.production.ubuntuBase)-base-amd64.tar.gz"
		sha256:               "c1e67ef7b17a6300e136118bd1dc04725009cb376c1aad10abcf8cd453628d58"
		url:                  "https://cdimages.ubuntu.com/ubuntu-base/releases/\(versions.production.ubuntu)/release/ubuntu-base-\(versions.production.ubuntuBase)-base-amd64.tar.gz"
	}
	guest_kernel_vmlinux: {
		name:                 "guest_image_vmlinux"
		downloaded_file_path: "vmlinux-\(versions.production.guestKernel)"
		sha256:               "e20e46d0c36c55c0d1014eb20576171b3f3d922260d9f792017aeff53af3d4f2"
		url:                  "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/vmlinux-\(versions.production.guestKernel)"
	}
	guest_kernel_config: {
		name:                 "guest_image_vmlinux_config"
		downloaded_file_path: "vmlinux-\(versions.production.guestKernel).config"
		sha256:               "024b2aae62fe7131f9a9ab80f27619c882c0a37265dd105adae01d2867bef7c3"
		url:                  "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/vmlinux-\(versions.production.guestKernel).config"
	}
	firecracker_release: {
		name:                 "guest_image_firecracker_release"
		downloaded_file_path: "firecracker-v\(versions.production.firecracker)-x86_64.tgz"
		sha256:               "00cadf7f21e709e939dc0c8d16e2d2ce7b975a62bec6c50f74b421cc8ab3cab4"
		url:                  "https://github.com/firecracker-microvm/firecracker/releases/download/v\(versions.production.firecracker)/firecracker-v\(versions.production.firecracker)-x86_64.tgz"
	}
	github_actions_runner: {
		name:                 "guest_image_github_actions_runner"
		downloaded_file_path: "actions-runner-linux-x64-\(versions.production.githubActionsRunner).tar.gz"
		sha256:               "18f8f68ed1892854ff2ab1bab4fcaa2f5abeedc98093b6cb13638991725cab74"
		url:                  "https://github.com/actions/runner/releases/download/v\(versions.production.githubActionsRunner)/actions-runner-linux-x64-\(versions.production.githubActionsRunner).tar.gz"
	}
	forgejo_runner: {
		name:                 "guest_image_forgejo_runner"
		downloaded_file_path: "forgejo-runner-\(versions.production.forgejoRunner)-linux-amd64"
		sha256:               "41c40d82ab4bde07d80c3e20254e3474b1d6abc3b4b8f57e181a3e66c1006521"
		url:                  "https://code.forgejo.org/forgejo/runner/releases/download/v\(versions.production.forgejoRunner)/forgejo-runner-\(versions.production.forgejoRunner)-linux-amd64"
	}
}

guestVersions: {
	ubuntu_base: {
		version: versions.production.ubuntuBase
		arch:    "amd64"
		url:     "https://cdimages.ubuntu.com/ubuntu-base/releases/\(versions.production.ubuntu)/release/ubuntu-base-\(version)-base-amd64.tar.gz"
		sha256:  "c1e67ef7b17a6300e136118bd1dc04725009cb376c1aad10abcf8cd453628d58"
	}
	rootfs: size: "8G"
	go: {
		version: versions.production.guestGo
		url:     "https://go.dev/dl/go\(version).linux-amd64.tar.gz"
		sha256:  "ceb5e041bbc3893846bd1614d76cb4681c91dadee579426cf21a63f2d7e03be6"
	}
	nodejs: {
		version: versions.production.guestNodejs
		url:     "https://nodejs.org/dist/v\(version)/node-v\(version)-linux-x64.tar.xz"
		sha256:  "472655581fb851559730c48763e0c9d3bc25975c59d518003fc0849d3e4ba0f6"
	}
	pnpm: version: versions.production.pnpm
	github_actions_runner: {
		version: versions.production.githubActionsRunner
		url:     "https://github.com/actions/runner/releases/download/v\(version)/actions-runner-linux-x64-\(version).tar.gz"
		sha256:  "18f8f68ed1892854ff2ab1bab4fcaa2f5abeedc98093b6cb13638991725cab74"
	}
	forgejo_runner: {
		version: versions.production.forgejoRunner
		url:     "https://code.forgejo.org/forgejo/runner/releases/download/v\(version)/forgejo-runner-\(version)-linux-amd64"
		sha256:  "41c40d82ab4bde07d80c3e20254e3474b1d6abc3b4b8f57e181a3e66c1006521"
	}
	firecracker: {
		version: versions.production.firecracker
		arch:    "x86_64"
		url:     "https://github.com/firecracker-microvm/firecracker/releases/download/v\(version)/firecracker-v\(version)-x86_64.tgz"
		sha256:  "00cadf7f21e709e939dc0c8d16e2d2ce7b975a62bec6c50f74b421cc8ab3cab4"
	}
	guest_kernel: {
		version:       versions.production.guestKernel
		arch:          "x86_64"
		url:           "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/vmlinux-\(version)"
		sha256:        "e20e46d0c36c55c0d1014eb20576171b3f3d922260d9f792017aeff53af3d4f2"
		config_url:    "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.15/x86_64/vmlinux-\(version).config"
		config_sha256: "024b2aae62fe7131f9a9ab80f27619c882c0a37265dd105adae01d2867bef7c3"
	}
}
