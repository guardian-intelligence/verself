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

        # Server profile: every service needed on a forge-metal node.
        # This Nix closure gets pushed to bare metal via `nix copy`.
        # All versions pinned transitively by flake.lock.
        serverProfile = pkgs.buildEnv {
          name = "forge-metal-server-${self.shortRev or "dev"}";
          paths = [
            # --- Observability stack ---
            clickhouse-static      # Wide event storage (official static binary)
            pkgs.tigerbeetle       # Financial ledger (double-entry accounting)
            # MongoDB excluded -- installed via apt (SSPL license, no binary cache, 30min+ source build)
            (pkgs.caddy.withPlugins {  # Reverse proxy, auto-TLS + Coraza WAF
              plugins = [ "github.com/corazawaf/coraza-caddy/v2@v2.4.0" ];
              hash = "sha256-sDTek0V9sUpCks8eH987Q1I30nBMlHOVUxMh58PczN4=";
            })
            pkgs.opentelemetry-collector-contrib  # OTLP ingestion

            # --- CI runtime ---
            pkgs.nodejs_22         # Node.js LTS (includes corepack for yarn)
            pkgs.containerd        # Container runtime
            # gVisor excluded from Nix profile -- downloaded as static binary
            # in containerd role (version-pinned, Go version sensitivity)
            pkgs.forgejo           # Git server
            pkgs.forgejo-runner    # CI runner (act_runner)
            # Firecracker: static binaries deployed to /usr/local/bin/ separately
            # (Nix-packaged ones are dynamically linked, unusable in jailer chroot)

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
            vendorHash = "sha256-RtOvjXttFRD9F+btSaxn1Zm9JjVM18HR2q1ktYUXte4=";
            subPackages = [ "cmd/forge-metal" ];
            # Skip sandbox-hostile test that shells out to a temp script.
            checkFlags = [ "-run" "^(?!TestConfigEditUsesEditorAndCreatesFile)" ];

            ldflags = [
              "-s" "-w"
              "-X main.version=${self.shortRev or "dev"}"
            ];
          };

          # The golden image closure. Push to bare metal with:
          #   nix copy --to ssh://user@host .#server-profile
          server-profile = serverProfile;
        };
      });
}
