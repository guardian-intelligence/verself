{
  description = "forge-metal - Self-hosted bare-metal CI platform";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # ClickHouse: official static binary, pinned independently from nixpkgs.
        # ~500 MB static binary vs ~1.4 GB Nix closure (saves ~900 MB).
        clickhouse-static = pkgs.stdenv.mkDerivation {
          pname = "clickhouse-static";
          version = "26.3.2.3";
          src = pkgs.fetchurl {
            url = "https://packages.clickhouse.com/tgz/stable/clickhouse-common-static-26.3.2.3-amd64.tgz";
            hash = "sha256-KwzNyEvDzGJECKikkBgcbu1rjfTgkPm07X5kfkYJMng=";
          };
          dontConfigure = true;
          dontBuild = true;
          sourceRoot = ".";
          installPhase = ''
            mkdir -p $out/bin
            cp clickhouse-common-static-26.3.2.3/usr/bin/clickhouse $out/bin/
            # Create symlinks for the multi-tool binary
            for cmd in server client local keeper benchmark; do
              ln -s clickhouse $out/bin/clickhouse-$cmd
            done
          '';
        };

        # --- Firecracker CI guest ---

        # Guest init: PID 1 inside Firecracker VMs. Statically linked.
        forgevmInit = pkgs.buildGoModule {
          pname = "forgevm-init";
          version = "0.1.0";
          src = pkgs.lib.cleanSourceWith {
            src = ./.;
            filter = path: type:
              let baseName = baseNameOf (toString path);
              in !(baseName == "result" || baseName == "results" || baseName == ".direnv");
          };
          vendorHash = "sha256-gXsDmFP3ZLITxOZnZAQH+BoDpSsn78trFYnexBfhmSU=";
          subPackages = [ "cmd/forgevm-init" ];
          CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" ];
        };

        # Guest rootfs: ext4 image containing init + toolchain for CI jobs.
        # Written to a ZFS zvol on the host; Firecracker boots from it.
        ciGuestRootfs = let
          closureInfo = pkgs.closureInfo {
            rootPaths = [ forgevmInit pkgs.bashInteractive pkgs.coreutils pkgs.git pkgs.nodejs_22 ];
          };
        in pkgs.runCommand "ci-guest-rootfs" {
          nativeBuildInputs = [ pkgs.e2fsprogs ];
        } ''
          mkdir -p $out

          # Build rootfs directory tree.
          root=$TMPDIR/rootfs
          mkdir -p $root/{nix/store,sbin,bin,etc/ci,dev,proc,sys,tmp,run,var}
          mkdir -p $root/{home/runner,usr/bin,usr/local/bin}

          # Copy Nix store closure (all transitive dependencies).
          while IFS= read -r path; do
            cp -a "$path" "$root$path"
          done < ${closureInfo}/store-paths

          # /sbin/init -> forgevm-init (PID 1).
          ln -s ${forgevmInit}/bin/forgevm-init $root/sbin/init

          # Standard tool symlinks so scripts can find them at expected paths.
          for bin in bash sh; do
            ln -sf ${pkgs.bashInteractive}/bin/bash $root/bin/$bin
          done
          for bin in env cat ls cp mv rm mkdir chmod chown head tail wc tr sort uniq grep sed awk basename dirname readlink realpath mktemp tee; do
            ln -sf ${pkgs.coreutils}/bin/$bin $root/usr/bin/$bin
            ln -sf ${pkgs.coreutils}/bin/$bin $root/bin/$bin
          done
          ln -sf ${pkgs.git}/bin/git $root/usr/bin/git
          ln -sf ${pkgs.nodejs_22}/bin/node $root/usr/bin/node
          ln -sf ${pkgs.nodejs_22}/bin/node $root/usr/local/bin/node
          ln -sf ${pkgs.nodejs_22}/bin/npm $root/usr/bin/npm
          ln -sf ${pkgs.nodejs_22}/bin/npm $root/usr/local/bin/npm
          ln -sf ${pkgs.nodejs_22}/bin/npx $root/usr/bin/npx

          # Essential config files.
          echo "nameserver 8.8.8.8" > $root/etc/resolv.conf

          cat > $root/etc/passwd <<'PASSWD'
root:x:0:0:root:/root:/bin/bash
runner:x:1000:1000:runner:/home/runner:/bin/bash
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
PASSWD

          cat > $root/etc/group <<'GROUP'
root:x:0:
runner:x:1000:
nogroup:x:65534:
GROUP

          # Create ext4 image (4G, enough for node_modules + build artifacts).
          mke2fs -t ext4 -d $root -L ciroot -b 4096 $out/rootfs.ext4 4G
        '';

        # Guest kernel: stock nixpkgs kernel with vmlinux extracted.
        # Firecracker requires uncompressed ELF vmlinux (not bzImage).
        # The dev output of the kernel derivation includes vmlinux.
        ciKernel = pkgs.linuxPackages_6_6.kernel.dev;

        # Server profile: every service needed on a forge-metal node.
        # This Nix closure gets pushed to bare metal via `nix copy`.
        # All versions pinned transitively by flake.lock.
        serverProfile = pkgs.buildEnv {
          name = "forge-metal-server-${self.shortRev or "dev"}";
          paths = [
            # --- Observability stack ---
            clickhouse-static      # Wide event storage (official static binary)
            # MongoDB excluded -- installed via apt (SSPL license, no binary cache, 30min+ source build)
            pkgs.caddy             # Reverse proxy, auto-TLS
            pkgs.opentelemetry-collector-contrib  # OTLP ingestion

            # --- CI runtime ---
            pkgs.nodejs_22         # Node.js LTS (includes corepack for yarn)
            pkgs.containerd        # Container runtime
            # gVisor excluded from Nix profile -- downloaded as static binary
            # in containerd role (version-pinned, Go version sensitivity)
            pkgs.forgejo           # Git server
            pkgs.forgejo-runner    # CI runner (act_runner)
            pkgs.firecracker       # Firecracker microVM + jailer

            # --- System tools ---
            pkgs.wireguard-tools
            pkgs.git
            pkgs.curl
            pkgs.jq
            pkgs.sqlite
            pkgs.python3           # Ansible requires Python on remote

            # --- forge-metal binary ---
            self.packages.${system}.default
          ];
          pathsToLink = [ "/bin" "/share" "/lib" "/etc" ];
        };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            # Go
            pkgs.go_1_25
            pkgs.golangci-lint
            pkgs.gofumpt

            # Infrastructure
            pkgs.opentofu
            pkgs.ansible

            # Protobuf
            pkgs.protobuf
            pkgs.buf
            pkgs.protoc-gen-go
            pkgs.protoc-gen-go-grpc

            # Tools
            pkgs.shellcheck
            pkgs.jq
            clickhouse-static

            # Secrets
            pkgs.sops
            pkgs.age

            # Nix
            pkgs.nil

            # Test runtime (ZFS integration tests)
            pkgs.crun
            pkgs.debootstrap
          ];

          shellHook = ''
            echo "forge-metal dev shell"
            echo "  go:         $(go version | cut -d' ' -f3)"
            echo "  tofu:       $(tofu version -json | jq -r .terraform_version)"
            echo "  ansible:    $(ansible --version | head -1)"
            echo "  buf:        $(buf --version)"
            echo ""
            echo "Run: make build"
          '';
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "forge-metal";
            version = "0.1.0";
            src = pkgs.lib.cleanSourceWith {
              src = ./.;
              filter = path: type:
                let baseName = baseNameOf (toString path);
                in !(baseName == "result" || baseName == "results" || baseName == ".direnv");
            };
            vendorHash = "sha256-gXsDmFP3ZLITxOZnZAQH+BoDpSsn78trFYnexBfhmSU=";
            subPackages = [ "cmd/bmci" ];

            ldflags = [
              "-s" "-w"
              "-X main.version=${self.shortRev or "dev"}"
            ];
          };

          # Dev tools, installable to user profile:
          #   nix profile install .#dev-tools
          # Same packages as devShell, but in PATH permanently. No shell hook latency.
          dev-tools = pkgs.buildEnv {
            name = "forge-metal-dev-tools";
            ignoreCollisions = true;
            paths = [
              pkgs.go_1_25
              pkgs.golangci-lint
              pkgs.gofumpt
              pkgs.opentofu
              pkgs.ansible
              pkgs.protobuf
              pkgs.buf
              pkgs.protoc-gen-go
              pkgs.protoc-gen-go-grpc
              pkgs.shellcheck
              pkgs.jq
              clickhouse-static
              pkgs.sops
              pkgs.age
              pkgs.nil
              pkgs.crun
              pkgs.debootstrap
            ];
            pathsToLink = [ "/bin" ];
          };

          # Firecracker CI guest components.
          ci-guest-rootfs = ciGuestRootfs;
          ci-kernel = ciKernel;
          forgevm-init = forgevmInit;

          # The golden image closure. Push to bare metal with:
          #   nix copy --to ssh://user@host .#server-profile
          server-profile = serverProfile;
        };
      });
}
