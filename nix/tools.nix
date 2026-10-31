# The tix package, for one pkgs: the `tix` binary (Go), the
# Claude Code plugin. Linux and macOS.
{ pkgs, lib, env ? { } }:
let
  inherit (pkgs.stdenv.hostPlatform) isLinux;

  # The Claude Code plugin (skills, task and review agents, hooks). Its hooks run
  # `tix hook ...`: here the bare binary (no cycle with the tix wrapper,
  # which points at this plugin), with notify-send for questions
  ctHook = pkgs.runCommand "tix-hook" { nativeBuildInputs = [ pkgs.makeWrapper ]; } ''
    makeWrapper ${lib.getExe tixBin} $out/bin/tix \
      ${lib.optionalString isLinux "--suffix PATH : ${lib.makeBinPath [ pkgs.libnotify ]}"}
  '';
  plugin = pkgs.runCommand "tix-plugin" { } ''
    cp -r ${../assets/plugin} $out
    chmod -R u+w $out
    substituteInPlace $out/hooks/hooks.json \
      --replace-fail '"tix hook ' '"${ctHook}/bin/tix hook '
  '';

  version = "0.1.0";
  tixBin = pkgs.buildGoModule {
    pname = "tix";
    inherit version;
    src = lib.fileset.toSource {
      root = ../.;
      fileset = lib.fileset.unions [
        ../go.mod ../go.sum ../cmd ../internal ../assets ../tix_test.go ../tix_sync_test.go ../testdata
        # The assets tests check the docs, README and AGENTS.md the skills refer to
        ../docs ../README.md ../AGENTS.md
      ];
    };
    # Refresh whenever go.mod/go.sum change: set lib.fakeHash, run `nix build .#default` on the host, paste the hash it reports
    vendorHash = "sha256-mTX4/p9otTdny5gQRO/jmlv0TS9w4BhWv49YXVCEohA=";
    subPackages = [ "cmd/tix" ];
    ldflags = [ "-s" "-w" "-X github.com/zero4573/tix/internal/version.Version=${version}" ]
      # The prebuilt llama.cpp library needs libstdc++ and libgomp, which NixOS keeps out of the
      # default search path: tix preloads them from here (TIX_LIB_PATH goes first)
      ++ lib.optional isLinux "-X github.com/zero4573/tix/internal/embed.DefaultLibPath=${lib.makeLibraryPath [ pkgs.stdenv.cc.cc.lib ]}";
    # Every package, and the CLI tests (stub claude, editor; real git,
    # openssh for the tests' SSH-signed commits)
    nativeCheckInputs = with pkgs; [ coreutils git openssh ];
    checkPhase = ''
      runHook preCheck
      go test ./...
      runHook postCheck
    '';
    meta.mainProgram = "tix";
  };

  # Defaults tix sees unless the environment says otherwise
  defaultsEnv = { TIX_PLUGIN = plugin; } // env;

  # The tools tix runs: git, fzf (the vault picker), bash
  # (TIX_MCP_PREPARE) and notify-send (tix sync)
  tools = with pkgs; [ git coreutils fzf bash ] ++ lib.optional isLinux pkgs.libnotify;

  tix = pkgs.runCommand "tix"
    {
      nativeBuildInputs = [ pkgs.makeWrapper ];
      meta.mainProgram = "tix";
    } ''
    mkdir -p $out/bin
    makeWrapper ${lib.getExe tixBin} $out/bin/tix \
      ${lib.concatStrings (lib.mapAttrsToList (k: v: "--set-default ${k} ${lib.escapeShellArg (toString v)} ") defaultsEnv)} \
      --suffix PATH : ${lib.makeBinPath tools}
    mkdir -p $out/share/zsh/site-functions $out/share/bash-completion/completions $out/share/fish/vendor_completions.d
    ${lib.getExe tixBin} completion zsh > $out/share/zsh/site-functions/_tix
    ${lib.getExe tixBin} completion bash > $out/share/bash-completion/completions/tix
    ${lib.getExe tixBin} completion fish > $out/share/fish/vendor_completions.d/tix.fish
  '';
  # libgomp and libstdc++ of the prebuilt llama.cpp in one directory (Linux): go test and make
  # bench find them through embed.RuntimeLibDir when TIX_LIB_PATH is unset
  llamaRuntimeLibs = pkgs.runCommand "llama-runtime-libs" { } ''
    mkdir -p $out
    ln -s ${pkgs.stdenv.cc.cc.lib}/lib/libstdc++.so.6 $out/
    ln -s ${lib.getLib pkgs.stdenv.cc.cc}/lib/libgomp.so.1 $out/
  '';
in
{
  inherit plugin llamaRuntimeLibs;
  default = tix;
  # What sessions run themselves (on PATH inside a sandbox, for instance)
  sessionTools = [ tix ];
}
