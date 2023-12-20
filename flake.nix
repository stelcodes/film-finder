{
  description = "A very basic flake";

  inputs = {
    nixpkgs.url = "nixpkgs";
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
            pkgs.golint
            pkgs.graphviz # for `go tool pprof`
            pkgs.just
            pkgs.chromium
            # inputs.self.packages.${system}.ferret-cli
          ];
          shellHook = ''
            echo 'Entering Nix dev shell...'
            # mkdir -p ./local/go
            export GOPATH="$PWD/local/go"
            export CHROME_BIN="${pkgs.chromium}/bin/chromium"
          '';
        };

      });
}

# Docs
# https://www.freecodecamp.org/news/golang-environment-gopath-vs-go-mod/
# https://pkg.go.dev/std
# https://go.dev/tour/list
# https://go.dev/doc/
# https://go.dev/ref/spec
# https://just.systems/man/en
# https://jvns.ca/blog/2017/09/24/profiling-go-with-pprof/
# https://gosamples.dev/sqlite-intro/
