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
            pkgs.go
            pkgs.gopls
            inputs.self.packages.${system}.ferret-cli
          ];
          shellHook = ''
            echo 'Entering Nix dev shell...'
            # mkdir -p ./local/go
            export GOPATH="$PWD/local/go"
          '';
        };

      });
}

# Docs
# https://www.freecodecamp.org/news/golang-environment-gopath-vs-go-mod/
