# The claude-tickets commands, the Claude Code plugin and the graphify image
# sources, for one pkgs. Each command is tools/<name>.sh with
# tools/tickets-lib.sh prepended, built with writeShellApplication (which
# runs shellcheck). Linux and macOS.
{ pkgs, lib }:
let
  inherit (pkgs.stdenv.hostPlatform) isLinux;

  # The Claude Code plugin (skills, role agents, hooks), with the agent-state
  # hook given its tools
  plugin = pkgs.runCommand "claude-tickets-plugin" { } ''
    cp -r ${../plugin} $out
    chmod -R u+w $out
    substituteInPlace $out/hooks/agent-state \
      --replace-fail '#@PATH@' 'export PATH=${lib.makeBinPath ([ pkgs.jq pkgs.coreutils ] ++ lib.optional isLinux pkgs.libnotify)}:$PATH'
    patchShebangs $out/hooks
  '';

  # Sources of the graphify image (ticket-graph build)
  graphSrc = pkgs.runCommand "claude-tickets-graph" { } ''
    cp -r ${../graph} $out
  '';

  scaffold = ../tools/vault-scaffold;

  lib' = builtins.readFile ../tools/tickets-lib.sh;

  # tickets-lib.sh needs these (jq for every JSON file, flock for
  # json_update, GNU coreutils/sed/awk/find)
  libInputs = with pkgs; [ coreutils git jq gnused gnugrep gawk findutils ]
    ++ [ (if isLinux then pkgs.util-linux else pkgs.flock) ];

  # Where the commands find their parts unless the environment says
  # otherwise
  defaults = ''
    export CLAUDE_TICKETS_PLUGIN="''${CLAUDE_TICKETS_PLUGIN:-${plugin}}"
    export CLAUDE_TICKETS_GRAPH_DIR="''${CLAUDE_TICKETS_GRAPH_DIR:-${graphSrc}}"
  '';

  script = name: runtimeInputs: extraText: pkgs.writeShellApplication {
    inherit name;
    runtimeInputs = libInputs ++ runtimeInputs;
    text = defaults + extraText + lib' + builtins.readFile (../tools + "/${name}.sh");
  };

  notify = lib.optional isLinux pkgs.libnotify;

  ticketGraph = script "ticket-graph" [ ] "";
  ticketWs = script "ticket-ws" [ pkgs.tmux ] "";
  ticketStart = script "ticket-start" [ ticketWs ticketGraph pkgs.tmux ] "";
  graphifyIndex = script "graphify-index" [ ticketGraph ] "";

  tools = {
    ticket-graph = ticketGraph;
    ticket-ws = ticketWs;
    ticket-start = ticketStart;
    graphify-index = graphifyIndex;
    ticket-status = script "ticket-status" [ pkgs.tmux ] "";
    ticket-attach = script "ticket-attach" [ pkgs.tmux ] "";
    ticket-open = script "ticket-open" [ ] "";
    ticket-feedback = script "ticket-feedback" [ ticketStart ] "";
    # ticket-sync-mcp.sh (MCP client) and ticket-sync-jira.sh (the Jira
    # planner and applier) are function libraries for it
    ticket-sync = script "ticket-sync" ([ pkgs.curl graphifyIndex ] ++ notify)
      (builtins.readFile ../tools/ticket-sync-mcp.sh + builtins.readFile ../tools/ticket-sync-jira.sh);
    ticket-new = script "ticket-new" (lib.optional isLinux pkgs.xdg-utils) "";
    claude-vault = script "claude-vault" [ ] "";
    kb = script "kb" [ ticketWs ticketGraph ] "";
    kb-repo = script "kb-repo" [ ] "";
    vault-default = script "vault-default" [ pkgs.fzf ] "";
    vault-configure = script "vault-configure" [ pkgs.fzf ] "";
    vault-init = script "vault-init" [ pkgs.fzf tools.vault-configure ] ''
      export VAULT_SCAFFOLD=${scaffold}
    '';
    repo-layout = script "repo-layout" [ ] "";
    vault-lock = script "vault-lock" [ ] "";
    vault-links = pkgs.writers.writePython3Bin "vault-links" {
      flakeIgnore = [ "E501" ];
    } (builtins.readFile ../tools/vault-links.py);
  };

  # zsh completion of ticket IDs (ticket-start, ticket-feedback,
  # ticket-attach, ticket-open)
  completion = pkgs.writeTextFile {
    name = "claude-tickets-zsh-completion";
    destination = "/share/zsh/site-functions/_tickets";
    text = builtins.readFile ../tools/tickets-completion.zsh;
  };

  # Everything, plus the plugin and image sources under share/ (where a
  # manual install puts them too)
  default = pkgs.symlinkJoin {
    name = "claude-tickets";
    paths = builtins.attrValues tools ++ [ completion ];
    postBuild = ''
      mkdir -p $out/share/claude-tickets
      ln -s ${plugin} $out/share/claude-tickets/plugin
      ln -s ${graphSrc} $out/share/claude-tickets/graph
      ln -s ${scaffold} $out/share/claude-tickets/vault-scaffold
    '';
  };

  # What sessions run themselves (on PATH inside a sandbox, for instance)
  sessionTools = with tools; [ ticket-ws kb-repo ticket-new ticket-graph vault-lock vault-links ];
in
{
  inherit tools plugin graphSrc completion default sessionTools;
}
