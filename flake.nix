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
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            # Go
            pkgs.go_1_23
            pkgs.golangci-lint
            pkgs.gofumpt

            # Infrastructure
            pkgs.terraform
            pkgs.ansible

            # Protobuf
            pkgs.protobuf
            pkgs.buf
            pkgs.protoc-gen-go
            pkgs.protoc-gen-go-grpc

            # Tools
            pkgs.shellcheck
            pkgs.jq
            pkgs.clickhouse

            # Nix
            pkgs.nil
          ];

          shellHook = ''
            echo "forge-metal dev shell"
            echo "  go:         $(go version | cut -d' ' -f3)"
            echo "  terraform:  $(terraform version -json | jq -r .terraform_version)"
            echo "  ansible:    $(ansible --version | head -1)"
            echo "  buf:        $(buf --version)"
            echo ""
            echo "Run: make build"
          '';
        };

        packages.default = pkgs.buildGoModule {
          pname = "forge-metal";
          version = "0.1.0";
          src = ./.;
          vendorHash = null; # update after first `go mod tidy`
          subPackages = [ "cmd/bmci" ];

          ldflags = [
            "-s" "-w"
            "-X main.version=${self.shortRev or "dev"}"
          ];
        };
      });
}
