# The claude-tickets package, for one pkgs: the `ct` binary (Go), the
# Claude Code plugin and the graphify image sources. Linux and macOS.
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

  # --- ct (Go) ---
  ctBin = pkgs.buildGoModule {
    pname = "ct";
    version = "0.1.0";
    src = lib.fileset.toSource {
      root = ../.;
      fileset = lib.fileset.unions [
        ../go.mod ../go.sum ../cmd ../internal ../assets ../ct_test.go ../ct_sync_test.go ../testdata
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

  # Defaults ct sees unless the environment says otherwise
  defaultsEnv = { CLAUDE_TICKETS_PLUGIN = plugin; CLAUDE_TICKETS_GRAPH_DIR = graphSrc; } // env;

  # The tools ct runs: git, tmux (ticket windows), tar (unpacking the graph
  # image), fzf (the vault picker), bash (CLAUDE_TICKETS_MCP_PREPARE) and
  # notify-send (ct sync)
  tools = with pkgs; [ git tmux coreutils gnutar fzf bash ] ++ lib.optional isLinux pkgs.libnotify;

  # ct, with the defaults and the tools it runs
  ct = pkgs.runCommand "claude-tickets"
    {
      nativeBuildInputs = [ pkgs.makeWrapper ];
      meta.mainProgram = "ct";
    } ''
    mkdir -p $out/bin $out/share/claude-tickets
    ln -s ${plugin} $out/share/claude-tickets/plugin
    ln -s ${graphSrc} $out/share/claude-tickets/graph
    makeWrapper ${lib.getExe ctBin} $out/bin/ct \
      ${lib.concatStrings (lib.mapAttrsToList (k: v: "--set-default ${k} ${lib.escapeShellArg (toString v)} ") defaultsEnv)} \
      --suffix PATH : ${lib.makeBinPath tools}
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
