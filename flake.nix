{
  description = "Development environment for kite-mcp-server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_24
            gopls
            golangci-lint
            delve
            go-tools
          ];

          shellHook = ''
            echo "🚀 Welcome to the kite-mcp-server development environment!"
            echo "Go version: $(go version)"
            echo ""
            echo "Environment variables required to run the application:"
            echo "- KITE_API_KEY    : Your Kite API key"
            echo "- KITE_API_SECRET : Your Kite API secret"
            echo "- APP_MODE        : sse (default) or stdio"
            echo "- APP_PORT        : Port to listen on (default: 8080)"
            echo "- APP_HOST        : Host to listen on (default: localhost)"
          '';

          # Make Go modules available even in pure environments
          GOPATH = "$(pwd)/.go";
          GO111MODULE = "on";
        };
      }
    );
}