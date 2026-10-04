{
  # Obsidian, set up for claude-tickets (Linux, Flathub through nix-flatpak).
  #
  # NixOS module: installs the Flathub Obsidian. Its manifest (flathub/md.obsidian.Obsidian) requests
  # --filesystem=home plus /mnt, /run/media, /media, a Discord-RPC socket dir,
  # read-only gnupg, read-only fonts, and a persisted ~/.ssh. Revoke all of
  # that and scope it down to just the vault folder.
  nixosModule = { config, lib, ... }: {
    options.claude-tickets.obsidian.vaultRoot = lib.mkOption {
      type = lib.types.str;
      default = "~/Documents/Obsidian";
      description = "Folder holding the Obsidian vaults; the only one the Obsidian flatpak may access.";
    };

    config.services.flatpak.packages = [ "md.obsidian.Obsidian" ];

    config.services.flatpak.overrides.settings."md.obsidian.Obsidian".Context = {
      filesystems = [
        "!home"
        "!/mnt"
        "!/run/media"
        "!/media"
        "!xdg-run/app/com.discordapp.Discord"
        "!xdg-run/gnupg"
        "!~/.local/share/fonts"
        # A path, not xdg-documents/...: that resolves through user-dirs.dirs,
        # which may not exist, and then flatpak silently grants nothing
        "${config.claude-tickets.obsidian.vaultRoot}:create"
      ];
      sockets = [ "wayland" "fallback-x11" "pulseaudio" "!ssh-auth" ];
      persistent = [ "!~/.ssh" ];
    };
  };

  # Per vault:
  #  * Tasks community plugin: seeded once. Installs the plugin files only if
  #    missing, and only creates community-plugins.json if it doesn't exist
  #    yet.  After that, Obsidian's own in-app plugin updater/toggle owns it.
  #  * Tasks plugin settings: enforced. ./tasks-settings.json is the source of
  #    truth, written to every vault's data.json on every switch -- changes
  #    made in Obsidian's settings UI are reset unless copied back here:
  #      cp <vault root>/<vault>/.obsidian/plugins/obsidian-tasks-plugin/data.json \
  #        obsidian/tasks-settings.json
  #  * Minimal theme (kepano): seeded once, and selected only if the vault
  #    hasn't picked a theme, so in-app theme changes/updates stick.
  #  * Readable line length: turned off once (Obsidian and Minimal otherwise
  #    cap notes at ~40rem), only if the vault hasn't set it.
  # And Obsidian-wide: every vault is registered in obsidian.json by its real
  # path. A vault picked via the file chooser can end up registered as a
  # document-portal path (/run/user/<uid>/doc/<id>/<vault>); that FUSE mount
  # gets no inotify events for edits made outside it (agents, other editors),
  # so Obsidian never notices them. Skipped while Obsidian runs, since it
  # rewrites obsidian.json on exit -- the next switch catches up.
  homeModule = { config, lib, pkgs, ... }:
    let
      cfg = config.programs.claude-tickets.obsidian;
      inherit (cfg) vaults;
      root = cfg.vaultRoot;
      pluginId = "obsidian-tasks-plugin";

      # obsidian-tasks-group/obsidian-tasks release 8.4.0. Bump the version +
      # the three sha256s together when updating (nix-prefetch-url <url>).
      tasksVersion = "8.4.0";
      tasksAsset = name: sha256: pkgs.fetchurl {
        url = "https://github.com/obsidian-tasks-group/obsidian-tasks/releases/download/${tasksVersion}/${name}";
        inherit sha256;
      };
      tasksMainJs = tasksAsset "main.js" "1yj83saffq2sxm9mqy9hricicznjkjca4zir0qd7rvizrqxk7qy1";
      tasksManifest = tasksAsset "manifest.json" "0gy3czl5jqik5ddk654d2m1l5yybnsv1d7gwriqdvjx9dcagm729";
      tasksStyles = tasksAsset "styles.css" "0pck54nfgyxyaiiyvfhy3132alszd65x2ldjk5c2n5kl7cysab1v";

      # kepano/obsidian-minimal release 9.0.2 -- the newest whose
      # minAppVersion (1.13.0) the Flathub Obsidian (1.13.7) satisfies; 9.1.x
      # needs 1.14. Bump the version + both sha256s together
      # (nix-prefetch-url <url>).
      minimalVersion = "9.0.2";
      minimalAsset = name: sha256: pkgs.fetchurl {
        url = "https://github.com/kepano/obsidian-minimal/releases/download/${minimalVersion}/${name}";
        inherit sha256;
      };
      minimalCss = minimalAsset "theme.css" "0pvsgxjr9f98knvfmgm8is37qz6m3d0v1dgag4lcirn6lm7xhx49";
      minimalManifest = minimalAsset "manifest.json" "0dh5d28fin0gq2dnn2gf9xfhbg3j6n9pfkbvsm5awbma0a8r7ji2";

      # Sets a top-level key in a vault JSON settings file (keeping its other
      # keys) unless it's already set, so in-app changes stick afterwards
      setIfUnset = pkgs.writeShellScript "obsidian-set-if-unset" ''
        set -eu
        file="$1" key="$2" value="$3"
        current="$(cat "$file" 2>/dev/null || true)"
        [ -n "$current" ] || current='{}'
        if ${lib.getExe pkgs.jq} -e --arg k "$key" '.[$k] == null or .[$k] == ""' <<< "$current" >/dev/null; then
          ${lib.getExe pkgs.jq} --arg k "$key" --argjson v "$value" '.[$k] = $v' <<< "$current" > "$file.tmp"
          mv "$file.tmp" "$file"
        fi
      '';

      # Rewrites document-portal vault paths to the real path (keeping the
      # vault id, so per-vault app state survives) and adds any vault not yet
      # registered, with an id derived from its path
      registerVaults = pkgs.writeShellScript "obsidian-register-vaults" ''
        set -eu
        if ${lib.getExe pkgs.flatpak} ps --columns=application 2>/dev/null | grep -qx md.obsidian.Obsidian; then
          echo "obsidian: running, not registering vaults in obsidian.json (quit it and switch again)" >&2
          exit 0
        fi
        root="$1"
        shift
        file="$HOME/.var/app/md.obsidian.Obsidian/config/obsidian/obsidian.json"
        mkdir -p "$(dirname "$file")"
        current="$(cat "$file" 2>/dev/null || true)"
        [ -n "$current" ] || current='{}'
        ids="$(for v in "$@"; do
          printf '%s %s\n' "$v" "$(printf '%s' "$root/$v" | sha256sum | cut -c1-16)"
        done)"
        ${lib.getExe pkgs.jq} -c --arg root "$root" --arg ids "$ids" --argjson ts "$(date +%s%3N)" '
          .vaults = ((.vaults // {}) | map_values(
            if (.path // "") | test("^/run/user/[0-9]+/doc/")
            then .path = ($root + "/" + (.path | split("/") | last))
            else . end))
          | reduce ($ids | split("\n")[] | select(. != "") | split(" ")) as [$name, $id] (.;
              ($root + "/" + $name) as $path
              | if any(.vaults[]; .path == $path) then .
                else .vaults[$id] = { path: $path, ts: $ts } end)
        ' <<< "$current" > "$file.tmp"
        if cmp -s "$file.tmp" "$file"; then rm "$file.tmp"; else mv "$file.tmp" "$file"; fi
      '';

      seedVaultScript = vault: ''
        vault_dir="${root}/${vault}"
        plugin_dir="$vault_dir/.obsidian/plugins/${pluginId}"
        if [ ! -e "$plugin_dir/main.js" ]; then
          run mkdir -p "$plugin_dir"
          run install -m 0644 ${tasksMainJs} "$plugin_dir/main.js"
          run install -m 0644 ${tasksManifest} "$plugin_dir/manifest.json"
          run install -m 0644 ${tasksStyles} "$plugin_dir/styles.css"
        fi

        run install -m 0644 ${../obsidian/tasks-settings.json} "$plugin_dir/data.json"

        community_plugins="$vault_dir/.obsidian/community-plugins.json"
        if [ ! -e "$community_plugins" ]; then
          run mkdir -p "$vault_dir/.obsidian"
          run bash -c 'echo ${lib.escapeShellArg (builtins.toJSON [ pluginId ])} > "$1"' _ "$community_plugins"
        fi

        theme_dir="$vault_dir/.obsidian/themes/Minimal"
        if [ ! -e "$theme_dir/theme.css" ]; then
          run mkdir -p "$theme_dir"
          run install -m 0644 ${minimalCss} "$theme_dir/theme.css"
          run install -m 0644 ${minimalManifest} "$theme_dir/manifest.json"
        fi
        run ${setIfUnset} "$vault_dir/.obsidian/appearance.json" cssTheme '"Minimal"'
        run ${setIfUnset} "$vault_dir/.obsidian/app.json" readableLineLength false
      '';
    in {
      options.programs.claude-tickets.obsidian = {
        vaults = lib.mkOption {
          type = lib.types.listOf lib.types.str;
          default = [ ];
          example = [ "work" "personal" ];
          description = "Vaults (folder names under vaultRoot) to seed with the Tasks plugin, its settings and the Minimal theme, and register with Obsidian.";
        };
        vaultRoot = lib.mkOption {
          type = lib.types.str;
          default = "${config.home.homeDirectory}/Documents/Obsidian";
          description = "Folder holding the vaults (OBSIDIAN_ROOT for the claude-tickets commands).";
        };
      };

      config = lib.mkIf (vaults != [ ]) {
        home.activation.obsidianVaults =
          lib.hm.dag.entryAfter [ "writeBoundary" ]
            (lib.concatMapStringsSep "\n" seedVaultScript vaults + ''

              run ${registerVaults} ${lib.escapeShellArg root} ${lib.escapeShellArgs vaults}
            '');
      };
    };
}
