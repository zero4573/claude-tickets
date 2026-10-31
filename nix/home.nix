# home-manager module: the tix commands (with the plugin
# for Claude Code, rendered for kiro at each start and the zsh completion), and the environment they read. Everything
# here is optional; see the README for what each setting is for.
{ config, lib, pkgs, ... }:
let
  cfg = config.programs.tix;
  settingsFile = pkgs.writeText "tix-session-settings.json" (builtins.toJSON cfg.sessionSettings);
  # The settings below, built into the commands as defaults (an exported
  # variable still wins): they apply right after a switch, in every shell,
  # terminal or service, not only after the next login
  env = lib.filterAttrs (_: v: v != null) {
    TIX_CLAUDE = if cfg.claude == "claude" then null else cfg.claude;
    TIX_KIRO = if cfg.kiro == "kiro-cli" then null else cfg.kiro;
    TIX_AGENT = cfg.agent;
    TIX_SESSION_SETTINGS = if cfg.sessionSettings == { } then null else "${settingsFile}";
    TIX_MCP_CONFIG = cfg.mcpConfig;
    TIX_MCP_PREPARE = cfg.mcpPrepare;
    TIX_EDITOR = cfg.editor;
    TIX_TASKS_URL = cfg.tasksUrl;
    OBSIDIAN_ROOT = config.programs.tix.obsidian.vaultRoot or null;
  };
  built = import ./tools.nix { inherit pkgs lib env; };
in
{
  options.programs.tix = {
    enable = lib.mkEnableOption "the tix ticket workflow";

    package = lib.mkOption {
      type = lib.types.package;
      default = built.default;
      defaultText = lib.literalMD "tix, built with your pkgs";
      description = "The tix package (commands, plugin, completion).";
    };

    sessionTools = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = built.sessionTools;
      readOnly = true;
      description = "The commands sessions run themselves (tix: tix ws, tix spec repo, tix new, tix vault lock, tix vault links), e.g. for a sandbox's PATH.";
    };

    claude = lib.mkOption {
      type = lib.types.str;
      default = "claude";
      description = "Command Claude Code sessions run (TIX_CLAUDE), with any extra arguments.";
    };

    kiro = lib.mkOption {
      type = lib.types.str;
      default = "kiro-cli";
      example = "kiro";
      description = "Command kiro sessions run (TIX_KIRO), with any extra arguments; tix adds `chat --agent tix ...`.";
    };

    agent = lib.mkOption {
      type = lib.types.nullOr (lib.types.enum [ "claude" "kiro" ]);
      default = null;
      description = "The agent CLI every vault's sessions run (TIX_AGENT), over each vault's .workflow.json \"agent\"; null leaves it to the vaults (default claude).";
    };

    sessionSettings = lib.mkOption {
      type = lib.types.attrs;
      default = { };
      example = lib.literalExpression ''{ permissions.deny = [ "Bash(git push:*)" ]; }'';
      description = "Claude settings merged into every ticket and spec session's .claude/settings.json (TIX_SESSION_SETTINGS). See the README for recommended rules.";
    };

    mcpConfig = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Path of a standard MCP config naming the URL of each server the ticket sources use (TIX_MCP_CONFIG).";
    };

    mcpPrepare = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "my-mcp-proxy start";
      description = "Command run before tix sync talks to those servers (TIX_MCP_PREPARE).";
    };

    container = lib.mkOption {
      type = lib.types.nullOr (lib.types.enum [ "podman" "docker" ]);
      default = null;
      description = "Unused: tix no longer runs containers. Kept so existing configurations still evaluate; setting it only warns.";
    };

    systemdSlice = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Unused: tix no longer runs containers. Kept so existing configurations still evaluate; setting it only warns.";
    };

    editor = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Editor tix open uses (TIX_EDITOR); code when null.";
    };

    tasksUrl = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "https://mirror.example.com/obsidian-tasks";
      description = "Where tix vault init downloads the Tasks plugin release from (TIX_TASKS_URL): a mirror holding <version>/<file>; the GitHub releases when null. The checksums are always the pinned ones.";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    warnings =
      lib.optional (cfg.container != null) "programs.tix.container is unused (tix no longer runs containers); remove it."
      ++ lib.optional (cfg.systemdSlice != null) "programs.tix.systemdSlice is unused (tix no longer runs containers); remove it.";
  };
}
