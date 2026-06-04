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
    in
    {
      packages = forAll (pkgs: rec {
        xpc = pkgs.buildGoModule {
          pname = "xpc";
          version = "0.2.4";
          src = ./.;
          # Covers the replaced shen-go fork + yaml.v3 (see go.mod/go.sum).
          # nix prints the correct value on the first build; fill it in.
          vendorHash = "sha256-fNgbwKEUTS3mKe1BzrgNkuHyA3o4iu5TNc5JTEgHcOo=";
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
      });

      devShells = forAll (pkgs: {
        default = pkgs.mkShell {
          inputsFrom = [ self.packages.${pkgs.system}.xpc ];
        };
      });
    };
}
