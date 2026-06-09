{
  description = "xpc — static analyzer / type checker for Argo CD + Crossplane";

  # Pin our own recent nixpkgs so the build always has Go 1.25+ regardless of a
  # consumer's pin (e.g. fg-manifold's nixpkgs is older). Consumers reference
  # the built package, not this nixpkgs.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});

      # Single source of truth for the release version. Keep in lockstep with
      # cmd/xpc/main.go's `const version` (the release-gitlab.sh check reads it
      # from there).
      version = "0.2.8";
    in
    {
      packages = forAll (pkgs: rec {
        xpc = pkgs.buildGoModule {
          pname = "xpc";
          inherit version;
          src = ./.;
          # Covers the replaced shen-go fork (pyrex41/shen-go v1.2.0) + yaml.v3
          # (see go.mod/go.sum). nix prints the correct value on the first build
          # after a dependency change; fill it in.
          vendorHash = "sha256-Hkt1szctweyhjptjQHXfHzlNNO0bKiHOMqEgtg3cW2U=";
          subPackages = [ "cmd/xpc" ];
          ldflags = [
            "-s"
            "-w"
          ];
          # kernel/*.shen and the agent skills are baked in via go:embed, so the
          # binary is fully self-contained — no runtime kernel dir needed.
          doCheck = false;
          meta = {
            description = "Static analyzer for Argo CD + Crossplane manifests";
            homepage = "https://github.com/pyrex41/cross-validate";
            mainProgram = "xpc";
          };
        };
        default = xpc;

        # Reproducible, content-addressed OCI image wrapping the nix-built xpc.
        # Linux-only (CI gate + presync hook run on linux/amd64); building the
        # x86_64-linux output requires a linux builder. Consumers retag + push
        # to their own registry (ECR, ghcr). ca-certificates is included so the
        # binary can fetch CRDs/snapshots over TLS when run with --render.
        xpcImage = pkgs.dockerTools.buildLayeredImage {
          name = "xpc";
          tag = "v${version}";
          contents = [
            xpc
            pkgs.cacert
          ];
          config = {
            Entrypoint = [ "${xpc}/bin/xpc" ];
            Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
          };
        };
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.system}.xpc ];
        };
      });
    };
}
