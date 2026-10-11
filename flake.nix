{
  description = "deja - A shell history tool";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "deja";
          version = "unstable";
          src = ./.;

          vendorHash = "sha256-XHcZUtx82zT3yPCYzJG+a7zfARPW4clbMn77/4luskw=";



          ldflags = [
            "-s"
            "-w"
          ];
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            sqlc
            go-tools
          ];
        };
      }
    );
}
