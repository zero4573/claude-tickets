# The claude-tickets package, for one pkgs: the `ct` binary (Go), the
# Claude Code plugin, the graphify image sources, and the bash scripts some
# `ct` subcommands still run (libexec/claude-tickets; each moves to Go in
# turn). Linux and macOS.
{ pkgs, lib, env ? { } }:
let
  inherit (pkgs.stdenv.hostPlatform) isLinux;

  # The Claude Code plugin (skills, role agents, hooks). Its hooks run
  # `ct hook ...`: here the bare binary (no cycle with the ct wrapper,
  # which points at this plugin), with notify-send for questions
  ctHook = pkgs.runCommand "ct-hook" { nativeBuildInputs = [ pkgs.makeWrapper ]; } ''
    makeWrapper ${lib.getExe ctBin} $out/bin/ct \
      ${lib.optionalString isLinux "--suffix PATH : ${lib.makeBinPath [ pkgs.libnotify ]}"}
  '';
  plugin = pkgs.runCommand "claude-tickets-plugin" { } ''
    cp -r ${../plugin} $out
    chmod -R u+w $out
    substituteInPlace $out/hooks/hooks.json \
      --replace-fail '"ct hook ' '"${ctHook}/bin/ct hook '
  '';

  # Sources of the graphify image (ct graph build)
  graphSrc = pkgs.runCommand "claude-tickets-graph" { } ''
    cp -r ${../graph} $out
  '';

  scaffold = ../tools/vault-scaffold;

  # --- ct (Go) ---
  ctBin = pkgs.buildGoModule {
    pname = "ct";
    version = "0.1.0";
    src = lib.fileset.toSource {
      root = ../.;
      fileset = lib.fileset.unions [
        ../go.mod ../go.sum ../cmd ../internal ../ct_test.go ../testdata
      ];
    };
    vendorHash = "sha256-Ld0QpIlwZQH3hGdu761fJAeBqere+q6RLPxE1ReQj/Q=";
    subPackages = [ "cmd/ct" ];
    # Every package, and the CLI tests (stub tmux, podman, editor; real
    # git, with an SSH signing key for ct ws sign)
    nativeCheckInputs = with pkgs; [ coreutils git gnutar openssh ];
    checkPhase = ''
      runHook preCheck
      go test ./...
      runHook postCheck
    '';
    meta.mainProgram = "ct";
  };

  # --- the bash scripts not ported yet ---
  lib' = builtins.readFile ../tools/tickets-lib.sh;

  # tickets-lib.sh needs these (jq for every JSON file, flock for
  # json_update, GNU coreutils/sed/awk/find)
  libInputs = with pkgs; [ coreutils git jq gnused gnugrep gawk findutils gnutar ]
    ++ [ (if isLinux then pkgs.util-linux else pkgs.flock) ];

  # Defaults the scripts (and ct) see unless the environment says otherwise
  defaultsEnv = { CLAUDE_TICKETS_PLUGIN = plugin; CLAUDE_TICKETS_GRAPH_DIR = graphSrc; } // env;
  defaults = lib.concatStrings (lib.mapAttrsToList
    (k: v: "export ${k}=\"\${${k}:-${lib.escape [ "\"" "\\" "\$" "`" ] (toString v)}}\"\n")
    defaultsEnv);

  script = name: runtimeInputs: extraText: pkgs.writeShellApplication {
    inherit name;
    runtimeInputs = libInputs ++ runtimeInputs;
    text = defaults + extraText + lib' + builtins.readFile (../tools + "/${name}.sh");
  };

  notify = lib.optional isLinux pkgs.libnotify;

  # The scripts call `ct ws`, `ct graph`, ...: the bare binary, which finds
  # the scripts through CLAUDE_TICKETS_LIBEXEC, passed down by the ct that
  # ran them
  ct' = ctBin;
  scripts = {
    # ticket-sync-mcp.sh (MCP client) and ticket-sync-jira.sh (the Jira
    # planner and applier) are function libraries for it
    ticket-sync = script "ticket-sync" ([ pkgs.curl ct' ] ++ notify)
      (builtins.readFile ../tools/ticket-sync-mcp.sh + builtins.readFile ../tools/ticket-sync-jira.sh);
    vault-configure = script "vault-configure" [ pkgs.fzf ] "";
    vault-init = script "vault-init" [ pkgs.fzf ct' ] ''
      export VAULT_SCAFFOLD=${scaffold}
    '';
    vault-lock = script "vault-lock" [ ] "";
    vault-links = pkgs.writers.writePython3Bin "vault-links" {
      flakeIgnore = [ "E501" ];
    } (builtins.readFile ../tools/vault-links.py);
  };

  libexec = pkgs.linkFarm "claude-tickets-libexec"
    (lib.mapAttrsToList (name: drv: { inherit name; path = lib.getExe' drv name; }) scripts);

  # ct, with the defaults and the tools it runs; libexec holds the scripts
  # it hands the not-yet-ported subcommands to
  ct = pkgs.runCommand "claude-tickets"
    {
      nativeBuildInputs = [ pkgs.makeWrapper ];
      meta.mainProgram = "ct";
    } ''
    mkdir -p $out/bin $out/libexec $out/share/claude-tickets
    ln -s ${libexec} $out/libexec/claude-tickets
    ln -s ${plugin} $out/share/claude-tickets/plugin
    ln -s ${graphSrc} $out/share/claude-tickets/graph
    ln -s ${scaffold} $out/share/claude-tickets/vault-scaffold
    makeWrapper ${lib.getExe ctBin} $out/bin/ct \
      --set-default CLAUDE_TICKETS_LIBEXEC ${libexec} \
      ${lib.concatStrings (lib.mapAttrsToList (k: v: "--set-default ${k} ${lib.escapeShellArg (toString v)} ") defaultsEnv)} \
      --suffix PATH : ${lib.makeBinPath (with pkgs; [ git tmux coreutils gnutar ])}
    mkdir -p $out/share/zsh/site-functions $out/share/bash-completion/completions $out/share/fish/vendor_completions.d
    ${lib.getExe ctBin} completion zsh > $out/share/zsh/site-functions/_ct
    ${lib.getExe ctBin} completion bash > $out/share/bash-completion/completions/ct
    ${lib.getExe ctBin} completion fish > $out/share/fish/vendor_completions.d/ct.fish
  '';
in
{
  inherit plugin graphSrc;
  default = ct;
  # What sessions run themselves (on PATH inside a sandbox, for instance)
  sessionTools = [ ct ];
}
