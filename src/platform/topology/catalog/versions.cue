// versions.cue - pinned versions and binary manifests for platform SBOM surfaces.
package catalog

versions: {
	production: {
		platform:                    "0.1.0"
		ubuntu:                      "24.04"
		ubuntuBase:                  "24.04.4"
		postgresql:                  "16"
		clickhouse:                  "26.3.2.3"
		tigerbeetle:                 "0.16.78"
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
		electric:                    "1.5.0"
		firecracker:                 "1.15.0"
		guestKernel:                 "6.1.155"
		guestGo:                     "1.25.8"
		guestNodejs:                 "24.15.0"
		pnpm:                        "10.33.0"
		vitePlus:                    "0.1.16"
		githubActionsRunner:         "2.333.1"
		forgejoRunner:               "12.9.0"
	}

	development: {
		go:                                        "1.25.8"
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

serverTools: {
	clickhouse: {
		version:        versions.production.clickhouse
		format:         "tarball"
		url:            "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-\(version)-amd64.tgz"
		sha256:         "2b0ccdc84bc3cc624408a8a490181c6eed6b8df4e090f9b4ed7e647e46093278"
		extract_binary: "clickhouse-common-static-\(version)/usr/bin/clickhouse"
		symlinks: ["clickhouse-server", "clickhouse-client", "clickhouse-local", "clickhouse-keeper", "clickhouse-benchmark"]
	}
	tigerbeetle: {
		version:        versions.production.tigerbeetle
		format:         "zip"
		url:            "https://github.com/tigerbeetle/tigerbeetle/releases/download/\(version)/tigerbeetle-x86_64-linux.zip"
		sha256:         "d32d7ce6aefd76559eff93efc17e74585243581059d47d988155458e4aaa2beb"
		extract_binary: "tigerbeetle"
	}
	zitadel: {
		version:        versions.production.zitadel
		format:         "tarball"
		url:            "https://github.com/zitadel/zitadel/releases/download/v\(version)/zitadel-linux-amd64.tar.gz"
		sha256:         "fe1f5231e5dcbdca63ae77adab0d2241daafeb9712e7d6cded3713e9ef50f1cb"
		extract_binary: "zitadel-linux-amd64/zitadel"
	}
	openbao: {
		version:        versions.production.openbao
		format:         "deb"
		url:            "https://github.com/openbao/openbao/releases/download/v\(version)/openbao_\(version)_linux_amd64.deb"
		sha256:         "5b915011ba8fa8137bd3309830aded0250b8fce42706bd6dcd2b91ac5560cde7"
		extract_binary: "usr/bin/bao"
	}
	spire: {
		version:     versions.production.spire
		format:      "tarball"
		url:         "https://github.com/spiffe/spire/releases/download/v\(version)/spire-\(version)-linux-amd64-musl.tar.gz"
		sha256:      "cacab9ff32b7a24714edcf4328c4ce27bddd38496b443e8750395883c72a3bfb"
		extract_dir: "spire-\(version)"
		binaries: ["spire-server", "spire-agent"]
	}
	spiffe_helper: {
		version:        versions.production.spiffeHelper
		format:         "tarball"
		url:            "https://github.com/spiffe/spiffe-helper/releases/download/v\(version)/spiffe-helper_v\(version)_Linux-x86_64.tar.gz"
		sha256:         "7fba909574320d6a656e2e7d7f0657890fefad08a2abecd86c7bafe62d6d9134"
		extract_binary: "spiffe-helper"
	}
	nats_server: {
		version:        versions.production.natsServer
		format:         "tarball"
		url:            "https://github.com/nats-io/nats-server/releases/download/v\(version)/nats-server-v\(version)-linux-amd64.tar.gz"
		sha256:         "570d2d627db111e679cc1e6bc57ba78f373ed1769acd8dc9c21c8f62d15b3c52"
		extract_binary: "nats-server-v\(version)-linux-amd64/nats-server"
	}
	garage: {
		version:     versions.production.garage
		format:      "binary"
		url:         "https://garagehq.deuxfleurs.fr/_releases/\(version)/x86_64-unknown-linux-musl/garage"
		sha256:      "f98d317942bb341151a2775162016bb50cf86b865d0108de03eb5db16e2120cd"
		binary_name: "garage"
	}
	forgejo: {
		version:     versions.production.forgejo
		format:      "binary"
		url:         "https://codeberg.org/forgejo/forgejo/releases/download/v\(version)/forgejo-\(version)-linux-amd64"
		sha256:      "3919f10a7845f3b71bacc2c7a3bfa2cd71aed58a0b8be6ab5e95f2e150b4ded7"
		binary_name: "forgejo"
	}
	otelcol_contrib: {
		version:        versions.production.otelcolContrib
		format:         "tarball"
		url:            "https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/v\(version)/otelcol-contrib_\(version)_linux_amd64.tar.gz"
		sha256:         "4acb57355e9388f257b28de8c18422ff43e52eb329052bd54ebecde000dcbb47"
		extract_binary: "otelcol-contrib"
	}
	temporal: {
		version: versions.production.temporal
		format:  "tarball"
		url:     "https://github.com/temporalio/temporal/releases/download/v\(version)/temporal_\(version)_linux_amd64.tar.gz"
		sha256:  "83f900fe8f9fd23c0e6369041355d2edbd768a91667dd6bc22a98d8316632177"
		binaries: ["temporal-server", "temporal-sql-tool", "tdbg"]
	}
	grafana: {
		version:     versions.production.grafana
		format:      "tarball"
		url:         "https://dl.grafana.com/oss/release/grafana-\(version).linux-amd64.tar.gz"
		sha256:      "f240b200a803bf64592fae645331750c2681df5f07496743d714db12393a591f"
		extract_dir: "grafana-v\(version)"
		install_dir: "/opt/verself/grafana"
		binary_name: "grafana"
	}
	grafana_clickhouse_datasource: {
		version:    versions.production.grafanaClickhouseDatasource
		format:     "zip"
		url:        "https://github.com/grafana/clickhouse-datasource/releases/download/v\(version)/grafana-clickhouse-datasource-\(version).linux_amd64.zip"
		sha256:     "11f569287a607043a9c60c4abca493784cc187b6bc4298270e5ef764719a22f1"
		plugin_dir: "grafana-clickhouse-datasource"
	}
	containerd: {
		version:        versions.production.containerd
		format:         "tarball"
		url:            "https://github.com/containerd/containerd/releases/download/v\(version)/containerd-static-\(version)-linux-amd64.tar.gz"
		sha256:         "5db46232ce716f85bf1e71497a9038c87e63030574bf03f9d09557802188ad27"
		extract_binary: "bin/containerd"
	}
	nodejs: {
		version:          versions.production.nodejs
		format:           "tarball_xz"
		url:              "https://nodejs.org/dist/v\(version)/node-v\(version)-linux-x64.tar.xz"
		sha256:           "88fd1ce767091fd8d4a99fdb2356e98c819f93f3b1f8663853a2dee9b438068a"
		strip_components: 1
		install_dir:      "/opt/verself/nodejs"
		bin_symlinks: ["node", "npm", "npx", "corepack"]
	}
	stalwart: {
		version:        versions.production.stalwart
		format:         "tarball"
		url:            "https://github.com/stalwartlabs/stalwart/releases/download/v\(version)/stalwart-x86_64-unknown-linux-musl.tar.gz"
		sha256:         "b2042dbcf0a110a4a756a5288de013649fd1f7ee84fa002bb3d2e6ec1e5f1f0b"
		extract_binary: "stalwart"
	}
	stalwart_cli: {
		version:        versions.production.stalwartCli
		format:         "tarball"
		url:            "https://github.com/stalwartlabs/stalwart/releases/download/v\(version)/stalwart-cli-x86_64-unknown-linux-musl.tar.gz"
		sha256:         "8fbe74206bed46974c272623e184b11d6c8362eb606dddc155fdb6ae9df7b3e9"
		extract_binary: "stalwart-cli"
	}
	caddy: {
		version: versions.production.caddy
		format:  "xcaddy_build"
		plugins: ["github.com/corazawaf/coraza-caddy/v2@v\(versions.production.corazaCaddy)"]
		xcaddy_version: versions.production.xcaddy
	}
}

devTools: {
	go: {
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
		version:      versions.development.opentofu
		strategy:     "zip"
		url:          "https://github.com/opentofu/opentofu/releases/download/v\(version)/tofu_\(version)_linux_amd64.zip"
		sha256:       "901121681e751574d739de5208cad059eddf9bd739b575745cf9e3c961b28a13"
		install_path: "/usr/local/bin/tofu"
		version_cmd:  "tofu version -json"
	}
	ansible: {
		version:    versions.development.ansibleCore
		strategy:   "uv_tool"
		uv_package: "ansible-core"
		with: [
			"ansible-lint==\(versions.development.ansibleLint)",
			"opentelemetry-sdk==\(versions.development.ansibleOpentelemetrySdk)",
			"opentelemetry-exporter-otlp-proto-grpc==\(versions.development.ansibleOpentelemetryExporterOtlpProtoGrpc)",
			"opentelemetry-exporter-otlp-proto-http==\(versions.development.ansibleOpentelemetryExporterOtlpProtoHttp)",
		]
		version_cmd: "ansible --version"
	}
	"ansible-opentelemetry-sdk": {
		version:     versions.development.ansibleOpentelemetrySdk
		strategy:    "uv_tool_companion"
		version_cmd: "/opt/uv-tools/ansible-core/bin/python -c \"import importlib.metadata as m; print(m.version('opentelemetry-sdk'))\""
	}
	"ansible-opentelemetry-exporter-otlp-proto-grpc": {
		version:     versions.development.ansibleOpentelemetryExporterOtlpProtoGrpc
		strategy:    "uv_tool_companion"
		version_cmd: "/opt/uv-tools/ansible-core/bin/python -c \"import importlib.metadata as m; print(m.version('opentelemetry-exporter-otlp-proto-grpc'))\""
	}
	"ansible-opentelemetry-exporter-otlp-proto-http": {
		version:     versions.development.ansibleOpentelemetryExporterOtlpProtoHttp
		strategy:    "uv_tool_companion"
		version_cmd: "/opt/uv-tools/ansible-core/bin/python -c \"import importlib.metadata as m; print(m.version('opentelemetry-exporter-otlp-proto-http'))\""
	}
	"ansible-lint": {
		version:     versions.development.ansibleLint
		strategy:    "uv_tool_companion"
		version_cmd: "ansible-lint --version"
	}
	"pre-commit": {
		version:     versions.development.preCommit
		strategy:    "uv_tool"
		uv_package:  "pre-commit"
		version_cmd: "pre-commit --version"
	}
	protoc: {
		version:     versions.development.protoc
		strategy:    "zip"
		url:         "https://github.com/protocolbuffers/protobuf/releases/download/v\(version)/protoc-\(version)-linux-x86_64.zip"
		sha256:      "e9a91b6fcfe4177ec2cd35fc8f15c1e811fa0ecdef9372755cd6d3513d5faaab"
		install_dir: "/usr/local"
		version_cmd: "protoc --version"
	}
	cue: {
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
		version:      versions.development.buf
		strategy:     "binary"
		url:          "https://github.com/bufbuild/buf/releases/download/v\(version)/buf-Linux-x86_64"
		sha256:       "ef835cb38ed973849f68e0e6a88153cb2168507e09eecbec43a2ada2f6a698be"
		install_path: "/usr/local/bin/buf"
		version_cmd:  "buf --version"
	}
	shellcheck: {
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
		version:      versions.development.jq
		strategy:     "binary"
		url:          "https://github.com/jqlang/jq/releases/download/jq-\(version)/jq-linux-amd64"
		sha256:       "020468de7539ce70ef1bceaf7cde2e8c4f2ca6c3afb84642aabc5c97d9fc2a0d"
		install_path: "/usr/local/bin/jq"
		version_cmd:  "jq --version"
	}
	sops: {
		version:      versions.development.sops
		strategy:     "binary"
		url:          "https://github.com/getsops/sops/releases/download/v\(version)/sops-v\(version).linux.amd64"
		sha256:       "14e2e1ba3bef31e74b70cf0b674f6443c80f6c5f3df15d05ffc57c34851b4998"
		install_path: "/usr/local/bin/sops"
		version_cmd:  "sops --version"
	}
	age: {
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
		version:      versions.development.clickhouse
		strategy:     "tarball"
		url:          "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-\(version)-amd64.tgz"
		sha256:       "2b0ccdc84bc3cc624408a8a490181c6eed6b8df4e090f9b4ed7e647e46093278"
		install_path: "/usr/local/bin/clickhouse"
		version_cmd:  "clickhouse client --version"
	}
	"golangci-lint": {
		version:     versions.development.golangciLint
		strategy:    "go_install"
		go_package:  "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
		version_cmd: "golangci-lint --version"
	}
	gosec: {
		version:     versions.development.gosec
		strategy:    "go_install"
		go_package:  "github.com/securego/gosec/v2/cmd/gosec"
		version_cmd: "go version -m /usr/local/go-tools/bin/gosec"
	}
	gofumpt: {
		version:     versions.development.gofumpt
		strategy:    "go_install"
		go_package:  "mvdan.cc/gofumpt"
		version_cmd: "gofumpt --version"
	}
	"protoc-gen-go": {
		version:     versions.development.protocGenGo
		strategy:    "go_install"
		go_package:  "google.golang.org/protobuf/cmd/protoc-gen-go"
		version_cmd: "protoc-gen-go --version"
	}
	"protoc-gen-go-grpc": {
		version:     versions.development.protocGenGoGrpc
		strategy:    "go_install"
		go_package:  "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
		version_cmd: "protoc-gen-go-grpc --version"
	}
	"build-essential": {
		strategy:    "apt"
		version_cmd: "gcc --version"
	}
	crun: {
		version:     versions.development.crun
		strategy:    "apt"
		version_cmd: "crun --version"
	}
	debootstrap: {
		version:     versions.development.debootstrap
		strategy:    "apt"
		version_cmd: "debootstrap --version"
	}
	guarddog: {
		version:     versions.development.guarddog
		strategy:    "uv_tool"
		uv_package:  "guarddog"
		version_cmd: "guarddog --version"
	}
	"osv-scanner": {
		version:      versions.development.osvScanner
		strategy:     "binary"
		url:          "https://github.com/google/osv-scanner/releases/download/v\(version)/osv-scanner_linux_amd64"
		sha256:       "bb30c580afe5e757d3e959f4afd08a4795ea505ef84c46962b9a738aa573b41b"
		install_path: "/usr/local/bin/osv-scanner"
		version_cmd:  "osv-scanner --version"
	}
	stripe: {
		version:      versions.development.stripe
		strategy:     "tarball"
		url:          "https://github.com/stripe/stripe-cli/releases/download/v\(version)/stripe_\(version)_linux_x86_64.tar.gz"
		sha256:       "a0f9c131d1e06240f97dedfdf05bb9cb96ee33a865e8877807945bab160eaa0f"
		install_path: "/usr/local/bin/stripe"
		version_cmd:  "stripe version"
	}
	"agent-browser": {
		version:      versions.development.agentBrowser
		strategy:     "binary"
		url:          "https://github.com/vercel-labs/agent-browser/releases/download/v\(version)/agent-browser-linux-x64"
		sha256:       "02d26f105a9d8e203f8f966acfeb4bab191cfa4625431a535b8be5f8f5905472"
		install_path: "/usr/local/bin/agent-browser"
		version_cmd:  "agent-browser --version"
	}
}

guestVersions: {
	ubuntu_base: {
		version: versions.production.ubuntuBase
		arch:    "amd64"
		url:     "https://cdimages.ubuntu.com/ubuntu-base/releases/\(versions.production.ubuntu)/release/ubuntu-base-\(version)-base-amd64.tar.gz"
		sha256:  "c1e67ef7b17a6300e136118bd1dc04725009cb376c1aad10abcf8cd453628d58"
	}
	rootfs: {
		size: "8G"
	}
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
	pnpm: {
		version: versions.production.pnpm
	}
	vite_plus: {
		version: versions.production.vitePlus
	}
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
