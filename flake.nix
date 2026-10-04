{
  description = "claude-tickets: a ticket workflow for Claude Code, with an Obsidian vault as long-term memory";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      built = pkgs: import ./nix/tools.nix { inherit pkgs; inherit (pkgs) lib; };
      obsidian = import ./nix/obsidian.nix;
    in
    {
      packages = forAll (pkgs: {
        default = (built pkgs).default;
        plugin = (built pkgs).plugin;
      });

      # home-manager: programs.claude-tickets.* (and .obsidian.* for vaults)
      homeModules.claudeTickets = ./nix/home.nix;
      homeModules.obsidianVaults = obsidian.homeModule;
      # NixOS: the Flathub Obsidian, scoped to the vault folder (needs
      # nix-flatpak's services.flatpak)
      nixosModules.obsidian = obsidian.nixosModule;

      checks = forAll (pkgs: { default = (built pkgs).default; });
    };
}
