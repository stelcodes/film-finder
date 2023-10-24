{
  description = "A very basic flake";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = inputs:
    inputs.flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import inputs.nixpkgs { inherit system; };
      in
      {
        packages = {
          ferret-cli = pkgs.callPackage ./packages/ferret-cli { };
        };

        devShells.default = pkgs.mkShell {
          packages = [
            inputs.self.packages.${system}.ferret-cli
          ];
          shellHook = ''
            echo 'Entering Nix dev shell...'
          '';
        };

      });
}
