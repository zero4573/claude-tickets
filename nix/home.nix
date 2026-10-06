# home-manager module: the claude-tickets commands (with the Claude Code
# plugin and the zsh completion), and the environment they read. Everything
# here is optional; see the README for what each setting is for.
{ config, lib, pkgs, ... }:
let
  cfg = config.programs.claude-tickets;
  settingsFile = pkgs.writeText "claude-tickets-session-settings.json" (builtins.toJSON cfg.sessionSettings);
  # The settings below, built into the commands as defaults (an exported
  # variable still wins): they apply right after a switch, in every shell,
  # tmux window or service, not only after the next login
  env = lib.filterAttrs (_: v: v != null) {
    CLAUDE_TICKETS_CLAUDE = if cfg.claude == "claude" then null else cfg.claude;
    CLAUDE_TICKETS_SESSION_SETTINGS = if cfg.sessionSettings == { } then null else "${settingsFile}";
    CLAUDE_TICKETS_MCP_CONFIG = cfg.mcpConfig;
    CLAUDE_TICKETS_MCP_PREPARE = cfg.mcpPrepare;
    CLAUDE_TICKETS_CONTAINER = cfg.container;
    CLAUDE_TICKETS_LAUNCHER = cfg.launcher;
    CLAUDE_TICKETS_SYSTEMD_SLICE = cfg.systemdSlice;
    CLAUDE_TICKETS_EDITOR = cfg.editor;
    OBSIDIAN_ROOT = config.programs.claude-tickets.obsidian.vaultRoot or null;
  };
  built = import ./tools.nix { inherit pkgs lib env; };
in
{
  options.programs.claude-tickets = {
    enable = lib.mkEnableOption "the claude-tickets ticket workflow";

    package = lib.mkOption {
      type = lib.types.package;
      default = built.default;
      defaultText = lib.literalMD "claude-tickets, built with your pkgs";
      description = "The claude-tickets package (commands, plugin, completion).";
    };

    sessionTools = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = built.sessionTools;
      readOnly = true;
      description = "The commands sessions run themselves (ct: ct ws, ct kb repo, ct new, ct graph mcp, ct vault lock, ct vault links), e.g. for a sandbox's PATH.";
    };

    claude = lib.mkOption {
      type = lib.types.str;
      default = "claude";
      description = "Command sessions run (CLAUDE_TICKETS_CLAUDE), with any extra arguments.";
    };

    sessionSettings = lib.mkOption {
      type = lib.types.attrs;
      default = { };
      example = lib.literalExpression ''{ permissions.deny = [ "Bash(git push:*)" ]; }'';
      description = "Claude settings merged into every ticket and kb session's .claude/settings.json (CLAUDE_TICKETS_SESSION_SETTINGS). See the README for recommended rules.";
    };

    mcpConfig = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Path of a standard MCP config naming the URL of each server the ticket sources use (CLAUDE_TICKETS_MCP_CONFIG).";
    };

    mcpPrepare = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "my-mcp-proxy start";
      description = "Command run before ct sync talks to those servers (CLAUDE_TICKETS_MCP_PREPARE).";
    };

    container = lib.mkOption {
      type = lib.types.nullOr (lib.types.enum [ "podman" "docker" ]);
      default = null;
      description = "Container runtime for the code graph (CLAUDE_TICKETS_CONTAINER); detected when null.";
    };

    launcher = lib.mkOption {
      type = lib.types.nullOr (lib.types.enum [ "tmux" "none" ]);
      default = null;
      description = "Terminal multiplexer for ticket sessions (CLAUDE_TICKETS_LAUNCHER); tmux when installed, else none, when null. With none, ct start runs one ticket in the current terminal.";
    };

    systemdSlice = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "systemd user slice the graph containers run in (CLAUDE_TICKETS_SYSTEMD_SLICE).";
    };

    editor = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Editor ct open uses (CLAUDE_TICKETS_EDITOR); code when null.";
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
  };
}
