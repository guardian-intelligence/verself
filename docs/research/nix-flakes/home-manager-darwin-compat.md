# Nix Flakes: Home Manager, nix-darwin, and flake-compat

Covers: Home Manager as a flake consumer (`homeConfigurations`, activation internals, standalone vs NixOS module mode), `nix-darwin` for declarative macOS system configuration (`darwinConfigurations`, Homebrew integration, `system.defaults`), and `flake-compat` for bridging flake repos to legacy `nix-shell`/`nix-build` users.

---

## Topic 1: Home Manager as a Flake Consumer

[Home Manager](https://github.com/nix-community/home-manager) provides NixOS-style declarative configuration for user environments. It manages dotfiles, packages, environment variables, and shell configuration in a reproducible, rollback-able way.

### The `homeConfigurations` Output Type

The canonical flake output key for Home Manager is `homeConfigurations`. Each value is produced by `home-manager.lib.homeManagerConfiguration`:

```nix
# flake.nix — standalone Home Manager
{
  description = "User home configuration";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    home-manager = {
      url = "github:nix-community/home-manager/release-25.05";
      inputs.nixpkgs.follows = "nixpkgs";   # critical — see below
    };
  };

  outputs = { self, nixpkgs, home-manager, ... }: {
    homeConfigurations."alice@workstation" = home-manager.lib.homeManagerConfiguration {
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
      modules = [ ./home.nix ];
      extraSpecialArgs = { inherit inputs; };  # inject flake inputs into modules
    };
  };
}
```

**`homeManagerConfiguration` arguments:**

| Argument | Type | Purpose |
|----------|------|---------|
| `pkgs` | package set | The nixpkgs instance. Use `nixpkgs.legacyPackages.${system}` — avoids double-eval of nixpkgs |
| `modules` | list of modules | Same module protocol as NixOS: paths, attrsets, or functions. Merged with `lib.evalModules` |
| `extraSpecialArgs` | attrset | Extra arguments injected into every module alongside `lib`, `config`, `pkgs`, `osConfig`. Used to pass flake `inputs` or other custom values |

**The `inputs.nixpkgs.follows` trick.** Without this line, Home Manager pulls in its own pinned nixpkgs — meaning the system has *two* copies of nixpkgs evaluated. Binary cache hits fail because the store paths diverge. With `inputs.home-manager.inputs.nixpkgs.follows = "nixpkgs"`, both the root flake and Home Manager share the same nixpkgs instance and the same store paths.

### Configuration Name Format

`homeConfigurations` keys can be any string. The common convention is `"username"` or `"username@hostname"`:

```bash
# Activate by key name
home-manager switch --flake .#alice
home-manager switch --flake .#alice@workstation

# If only one homeConfiguration exists, this also works:
home-manager switch --flake .
```

The `@hostname` suffix is purely a naming convention — Home Manager does not enforce hostname matching. It is useful when one flake manages multiple machines with different configurations for the same user.

### What `home-manager switch` Does

```
1. nix build .#homeConfigurations."alice".activationPackage
   → produces /nix/store/<hash>-home-manager-generation/activate

2. Run the activate script:
   a. checkFilesChanged        — detect conflicting existing files
   b. writeBoundary            — scripts that modify files run after this
   c. createHomeFiles          — symlink dotfiles from store into ~/
   d. linkGeneration           — update /nix/var/nix/profiles/per-user/$USER
   e. createXdgUserDirs        — XDG_*_HOME directories
   f. (program-specific steps) — e.g., fontconfig rebuild, shell reload

3. GC root: /nix/var/nix/gcroots/per-user/$USER → current generation
   (prevents nix-collect-garbage from reclaiming active configuration)

4. ~/.nix-profile updated (symlink → per-user profile → current generation)
```

**Collision detection:** If a file that Home Manager wants to manage already exists and was *not* placed there by Home Manager, activation aborts with an error. You must manually delete or back up the conflicting file. This protects existing data — Home Manager never silently overwrites.

**Generations and rollback:**

```bash
home-manager generations          # list all generations with timestamps
home-manager switch --rollback    # activate the previous generation
# or activate a specific generation directly:
/nix/store/<hash>-home-manager-generation/activate
```

Each generation's activation script is permanently in the store until GC removes it. As long as the GC root points to the current generation, all previous listed generations are preserved only if you explicitly add them as GC roots — `nix-collect-garbage` will otherwise prune them.

### `home.stateVersion`

```nix
home.stateVersion = "24.11";  # set to the version when you FIRST installed HM
```

This is the **most important footgun in Home Manager.** `stateVersion` tells Home Manager which backwards-incompatible migrations have already been applied to your stateful data (shell history files, database formats, etc.). Changing it to a newer version causes Home Manager to re-run those migrations, potentially corrupting or overwriting data.

**Rule:** set `stateVersion` once on first install, then **never change it** unless you explicitly want to run migrations after reading the release notes.

This is conceptually identical to `system.stateVersion` in NixOS but scoped to user-level stateful data. The two are independent values.

### `programs.<name>` vs `home.packages`

| Mechanism | What it does |
|-----------|-------------|
| `home.packages = [ pkgs.ripgrep ]` | Adds `ripgrep` to `$PATH`. No dotfiles generated. Equivalent to `nix-env -iA nixpkgs.ripgrep` but declarative |
| `programs.git.enable = true` | Installs `git` AND generates `~/.config/git/config` from the `programs.git.*` options. The package is implicit |

`programs.<name>` modules are Home Manager's primary value proposition: they encode the correct config file format for each program so you don't have to. Examples:

```nix
programs.git = {
  enable = true;
  userName = "Alice";
  userEmail = "alice@example.com";
  extraConfig.pull.rebase = true;
};
# → generates ~/.config/git/config with [user], [pull] sections

programs.zsh = {
  enable = true;
  enableCompletion = true;
  history.size = 50000;
  shellAliases = { ll = "ls -la"; };
};
# → generates ~/.zshrc, ~/.zprofile; manages compinit setup
```

When a `programs.<name>` module exists, prefer it over manually writing `home.file` entries — the module handles quoting, ordering, and option merging correctly.

### `home.file` — Managed File Links

`home.file` places arbitrary files in `$HOME` as symlinks to `/nix/store`:

```nix
home.file = {
  # Inline text content
  ".inputrc".text = ''
    set editing-mode vi
    set show-mode-in-prompt on
  '';

  # Source from a path in the repo
  ".config/htop/htoprc".source = ./config/htoprc;

  # Executable script
  "bin/my-script" = {
    source = ./scripts/my-script.sh;
    executable = true;
  };
};
```

**Implementation:** Home Manager writes the content to the store and then symlinks `~/path` → `/nix/store/<hash>/path`. The symlink is read-only. If a program requires a writable config file (e.g., uses `inotify` to watch for changes, or writes back to the config), the symlink will fail. In that case, either use `home.file."path".onChange` to react to rebuilds, or manage the file outside Home Manager.

**`xdg.configFile`** is a namespace alias for `home.file.".config"` — both accept the same sub-options.

### `home.activation` — Custom Activation Scripts

`home.activation` is a DAG (Directed Acyclic Graph) of shell script fragments executed during `home-manager switch`. Each entry declares ordering constraints:

```nix
home.activation = {
  # Simple script after writeBoundary (safe to modify files)
  setupVimPlug = lib.hm.dag.entryAfter ["writeBoundary"] ''
    if [ ! -d "$HOME/.vim/autoload" ]; then
      ${pkgs.curl}/bin/curl -fLo ~/.vim/autoload/plug.vim \
        https://raw.githubusercontent.com/junegunn/vim-plug/master/plug.vim
    fi
  '';

  # Script that must run before another step
  makeConfigDir = lib.hm.dag.entryBefore ["setupVimPlug"] ''
    mkdir -p "$HOME/.vim"
  '';
};
```

**Key ordering constants:**

- `entryAfter ["writeBoundary"]` — run after all file links are created. Scripts that write or modify files **must** be placed after `writeBoundary`, not before it.
- `entryBefore ["linkGeneration"]` — run before the generation symlink is updated.
- `entryAnywhere` — no ordering constraint (rare; use only for read-only inspection scripts).

Activation scripts are shell fragments, not derivations. They run with `$PATH` containing all `home.packages` and `programs.*` packages for the current generation.

### `home.sessionVariables` — A Common Confusion

```nix
home.sessionVariables = {
  EDITOR = "nvim";
  GOPATH = "$HOME/go";
};
```

`sessionVariables` are written into a sourced shell file (`~/.nix-profile/etc/profile.d/hm-session-vars.sh`). They apply **only in new shells** started after the switch — not in the current terminal session, and not in GUI apps launched before the switch. This trips up first-time users who set a variable and immediately expect it to work.

For variables needed by systemd user services (Linux), use `systemd.user.sessionVariables` instead, which writes to the service environment.

### Combining NixOS + Home Manager in One Flake

The NixOS module mode integrates Home Manager into `nixos-rebuild`, eliminating the need to run `home-manager switch` separately:

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    home-manager = {
      url = "github:nix-community/home-manager/release-25.05";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, home-manager, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./hardware-configuration.nix
        ./configuration.nix
        home-manager.nixosModules.home-manager    # ← import HM NixOS module
        {
          home-manager.useGlobalPkgs = true;      # ← use system nixpkgs
          home-manager.useUserPackages = true;    # ← packages go to /etc/profiles/per-user
          home-manager.users.alice = import ./home.nix;  # ← per-user HM config
          # extraSpecialArgs injection:
          home-manager.extraSpecialArgs = { inherit inputs; };
        }
      ];
    };
  };
}
```

**`home-manager.useGlobalPkgs = true`** — Home Manager uses `pkgs` from the NixOS configuration rather than evaluating its own nixpkgs instance. This saves an evaluation and ensures binary cache consistency. Enables the NixOS `nixpkgs.config` settings (like `allowUnfree`) to apply to Home Manager packages too. Disables the `nixpkgs.*` Home Manager options since nixpkgs is now inherited.

**`home-manager.useUserPackages = true`** — Installs user packages to `/etc/profiles/per-user/$USER` instead of `~/.nix-profile`. This is required for `nixos-rebuild build-vm` to work correctly and for some display managers that source `/etc/profiles` but not `~/.nix-profile`.

**Deployment:** `sudo nixos-rebuild switch --flake .#myhost` rebuilds both the system and all per-user Home Manager configurations atomically.

### Standalone vs NixOS Module Mode

| Property | Standalone | NixOS Module |
|----------|-----------|--------------|
| Command | `home-manager switch --flake .#alice` | `nixos-rebuild switch` |
| Requires NixOS | No (works on any Linux or macOS) | Yes |
| Error messages | More descriptive | Less descriptive (buried in nixos-rebuild output) |
| pkgs source | Explicit `pkgs` arg in `homeManagerConfiguration` | Inherited from NixOS via `useGlobalPkgs` |
| Activation timing | Independent of system | Atomic with system switch |
| nix-darwin | Use `home-manager.darwinModules.home-manager` | N/A |

A common pattern for macOS is: nix-darwin for system config + Home Manager standalone for user config, since the NixOS module mode's unified rebuild isn't available on macOS without `darwin-rebuild`.

---

## Topic 2: `nix-darwin` for macOS System Configuration

[nix-darwin](https://github.com/LnL7/nix-darwin) (now at `github:nix-darwin/nix-darwin`) is the macOS equivalent of NixOS configuration. It provides a declarative module system for macOS system settings, Homebrew, LaunchDaemons, and more.

### `darwinConfigurations` Output Type

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nix-darwin = {
      url = "github:nix-darwin/nix-darwin/master";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = inputs@{ self, nix-darwin, nixpkgs, home-manager }: {
    darwinConfigurations."Alices-MacBook-Pro" = nix-darwin.lib.darwinSystem {
      modules = [
        ./darwin-configuration.nix
        home-manager.darwinModules.home-manager
        {
          home-manager.useGlobalPkgs = true;
          home-manager.useUserPackages = true;
          home-manager.users.alice = import ./home.nix;
        }
      ];
      specialArgs = { inherit inputs; };
    };
  };
}
```

**`darwinSystem` vs `nixosSystem` differences:**

| `nixosSystem` | `darwinSystem` |
|---------------|----------------|
| `system` arg required (or `pkgs`) | `system` inferred from `nixpkgs.hostPlatform` in modules (or explicit `system` arg) |
| `hardware-configuration.nix` required | No hardware config; macOS hardware is opaque |
| `nixpkgs.hostPlatform` = `x86_64-linux` | `nixpkgs.hostPlatform` = `"aarch64-darwin"` or `"x86_64-darwin"` |
| `system.stateVersion` is a NixOS release string | `system.stateVersion` is an integer (e.g., `4`) |

The configuration name in `darwinConfigurations` must match the machine's hostname (as returned by `scutil --get LocalHostName` or `hostname -s`). `darwin-rebuild switch` reads `$HOST` to determine which configuration to activate.

### What nix-darwin Can Manage

#### Environment and System Packages

```nix
environment.systemPackages = [ pkgs.curl pkgs.git pkgs.neovim ];
environment.variables.EDITOR = "nvim";
environment.shellAliases.ll = "ls -la";
```

These are available to all users system-wide via `/run/current-system/sw`.

#### Homebrew Integration

```nix
homebrew = {
  enable = true;

  # Formulae (CLI tools): equivalent to `brew install`
  brews = [ "mas" "wakeonlan" ];

  # Casks (GUI apps): equivalent to `brew install --cask`
  casks = [
    "1password"
    "firefox"
    { name = "discord"; greedy = true; }  # greedy: upgrade even if minor version pinned
  ];

  # Mac App Store apps (requires signed-in Apple ID)
  masApps = {
    "Tailscale" = 1475387142;
  };

  # Homebrew taps
  taps = [ "homebrew/cask-fonts" ];

  onActivation = {
    # How to handle packages installed via brew but NOT in this config:
    # "none"       — leave them alone (default)
    # "check"      — fail activation if unlisted packages are installed
    # "uninstall"  — remove unlisted packages only
    # "zap"        — remove unlisted packages AND their associated files/prefs/caches
    cleanup = "uninstall";  # "zap" is more thorough but potentially destructive

    autoUpdate = false;    # nix-darwin pins brew's auto-update; keep false for idempotency
    upgrade = false;       # same: let nix-darwin control versions, not `brew upgrade`
  };
};
```

**Critical gotcha: `cleanup = "zap"` vs `"uninstall"`.** `zap` runs `brew bundle --cleanup --zap`, which invokes each cask's `zap` stanza — this deletes app support files, preferences, and caches. A cask's zap stanza can remove files in `~/Library/Application Support`, `~/Library/Preferences`, and elsewhere. Use `"uninstall"` unless you want complete teardown of unlisted apps.

**Apple Silicon vs Intel Homebrew prefix:**

```nix
# nix-darwin sets homebrew.prefix automatically based on architecture:
# aarch64-darwin: /opt/homebrew
# x86_64-darwin:  /usr/local
```

This matters for shell configurations that need to add Homebrew to `$PATH`. nix-darwin's Homebrew module handles this automatically by prepending the correct prefix to shell `$PATH` when `homebrew.enable = true`.

#### `system.defaults` — macOS Preferences via `defaults write`

`system.defaults` provides declarative wrappers around macOS's `defaults` system. nix-darwin runs `defaults write` during activation to apply these.

```nix
system.defaults = {
  # Global macOS preferences (NSGlobalDomain)
  NSGlobalDomain = {
    AppleInterfaceStyle = "Dark";           # dark mode
    ApplePressAndHoldEnabled = false;       # disable press-and-hold for accents
    KeyRepeat = 2;                          # fast key repeat (lower = faster)
    InitialKeyRepeat = 15;
    NSAutomaticSpellingCorrectionEnabled = false;
    NSDocumentSaveNewDocumentsToCloud = false;
    "com.apple.keyboard.fnState" = true;   # use F1-F12 as function keys
  };

  # Dock preferences
  dock = {
    autohide = true;
    autohide-delay = 0.0;
    orientation = "bottom";
    show-recents = false;
    tilesize = 48;
    static-only = true;  # show only running apps
  };

  # Finder preferences
  finder = {
    AppleShowAllExtensions = true;
    ShowPathbar = true;
    FXEnableExtensionChangeWarning = false;
    _FXShowPosixPathInTitle = true;
  };

  # Login window
  loginwindow = {
    GuestEnabled = false;
    SHOWFULLNAME = true;
  };

  # Trackpad
  trackpad = {
    Clicking = true;   # tap-to-click
    TrackpadThreeFingerDrag = true;
  };
};
```

**Caveat:** `system.defaults` changes only take effect after `darwin-rebuild switch` AND (for some settings) logging out and back in. Some Dock and Finder settings require killing and restarting the affected process. nix-darwin runs `killall Dock` / `killall Finder` automatically for known-affected keys, but not all settings trigger this.

#### LaunchDaemons and LaunchAgents

```nix
# System-level service (root, starts at boot)
launchd.daemons.my-service = {
  command = "/run/current-system/sw/bin/my-binary";
  serviceConfig = {
    KeepAlive = true;
    RunAtLoad = true;
    StandardErrorPath = "/var/log/my-service.log";
  };
};

# User-level service (starts at login)
launchd.agents.my-user-service = {
  serviceConfig = {
    Label = "com.example.my-service";
    ProgramArguments = [ "${pkgs.my-pkg}/bin/my-binary" "--flag" ];
    RunAtLoad = true;
  };
};
```

Services are written to `/Library/LaunchDaemons/` (daemons) or `~/Library/LaunchAgents/` (agents) as plist files. nix-darwin runs `launchctl` to load/unload them during activation.

#### Users

```nix
users.users.alice = {
  name = "alice";
  home = "/Users/alice";
  shell = pkgs.zsh;
};
```

nix-darwin can declare users, but creating them from scratch requires macOS APIs that are not fully scriptable. The `users.users` module is best for ensuring existing users have the correct configuration, not for provisioning new admin users.

### What nix-darwin CANNOT Manage

| Limitation | Reason |
|-----------|--------|
| Kernel extensions (kexts/dext) | Require manual user approval via System Preferences; SIP protects the kext loading process |
| `/System` and `/usr` paths | Read-only system volume (APFS sealed system snapshot) on Catalina and later |
| Full Disk Access grants | Requires interactive macOS authorization; not scriptable without MDM |
| iCloud settings, Apple ID | Tied to Apple account; no `defaults` interface |
| App Store installation without signed-in Apple ID | `mas` requires an active session |
| FileVault, SIP, SecureBoot | Low-level security; requires explicit user interaction |

#### The `/run` Symlink Problem on Catalina+

macOS Catalina (10.15) introduced a read-only system volume. `/run` does not exist by default. nix-darwin requires `/run` to exist (it creates `/run/current-system` → the active system profile).

**Solution:** nix-darwin uses `/etc/synthetic.conf` to declare a synthetic mountpoint for `/run` that macOS creates at early boot:

```nix
# This is set automatically by nix-darwin; shown here for understanding:
# /etc/synthetic.conf:
# run    private/var/run
```

This tells macOS to create a synthetic symlink `/run` → `/private/var/run`. However, the `/private/var/run` directory must exist before `/run` is needed. On fresh installs, the first `darwin-rebuild switch` invocation creates the synthetic.conf entry, and the symlink is active after the next reboot.

**Issue:** at early boot, launchd may try to start the nix daemon before the Nix APFS volume is mounted, causing `/nix` to be unavailable. The fix is `services.activate-system.enable = true`, which registers a LaunchDaemon to re-activate the nix-darwin system after the Nix volume is mounted.

### Apple Silicon vs Intel Platform in Flakes

`darwinConfigurations` is **not** per-system in the `packages.${system}` sense. Each machine is a named configuration, and the system architecture is declared inside the module:

```nix
# In your darwin-configuration.nix module:
nixpkgs.hostPlatform = "aarch64-darwin";  # Apple Silicon
# or:
nixpkgs.hostPlatform = "x86_64-darwin";  # Intel

# Alternative: pass system to darwinSystem directly:
darwinConfigurations."my-mac" = nix-darwin.lib.darwinSystem {
  system = "aarch64-darwin";  # deprecated form; prefer nixpkgs.hostPlatform in modules
  modules = [ ... ];
};
```

**Rosetta 2 cross-execution:** You can allow x86_64 packages on Apple Silicon by enabling `nix.extraOptions = "extra-platforms = x86_64-darwin aarch64-darwin"`. This allows the Nix daemon to build and run x86_64 derivations under Rosetta, but it adds complexity and is not needed unless you explicitly need Intel-only binaries.

**Multi-machine flake with mixed architectures:**

```nix
darwinConfigurations = {
  "m1-laptop"  = nix-darwin.lib.darwinSystem { modules = [ { nixpkgs.hostPlatform = "aarch64-darwin"; } ./laptop.nix ]; };
  "intel-work" = nix-darwin.lib.darwinSystem { modules = [ { nixpkgs.hostPlatform = "x86_64-darwin";  } ./work.nix   ]; };
};
```

Each configuration is independently evaluated; they do not share a `system` variable.

### `darwin-rebuild switch` Activation Flow

```
1. Evaluate darwinConfigurations."hostname".system
2. Build the system profile derivation
3. Run activation script:
   a. Set up /etc/synthetic.conf (synthetic filesystem entries)
   b. Write /etc/nix/nix.conf (from nix.settings)
   c. Write /etc/shells (add Nix-managed shells)
   d. Apply system.defaults (runs `defaults write` for each changed setting)
   e. Load/unload LaunchDaemons and LaunchAgents (launchctl)
   f. Apply homebrew bundle (if homebrew.enable)
   g. Update /run/current-system symlink
4. Optionally: killall Dock/Finder if dock/finder settings changed
```

---

## Topic 3: `flake-compat` — Non-Flake Access to Flake Repos

`flake-compat` allows projects with a `flake.nix` to also support traditional `nix-shell` and `nix-build` workflows, without requiring the `nix-command flakes` experimental features. Originally at `github:edolstra/flake-compat`, it is now the official NixOS project at `github:NixOS/flake-compat`.

### The Bridge Pattern

Create two shim files in your repo root:

```nix
# default.nix — for `nix-build`
(import
  (
    let
      lock = builtins.fromJSON (builtins.readFile ./flake.lock);
      nodeName = lock.nodes.root.inputs.flake-compat;
    in
    fetchTarball {
      url = lock.nodes.${nodeName}.locked.url
        or "https://github.com/edolstra/flake-compat/archive/${lock.nodes.${nodeName}.locked.rev}.tar.gz";
      sha256 = lock.nodes.${nodeName}.locked.narHash;
    }
  )
  { src = ./.; }
).defaultNix
```

```nix
# shell.nix — for `nix-shell`
(import
  (
    let
      lock = builtins.fromJSON (builtins.readFile ./flake.lock);
      nodeName = lock.nodes.root.inputs.flake-compat;
    in
    fetchTarball {
      url = lock.nodes.${nodeName}.locked.url
        or "https://github.com/edolstra/flake-compat/archive/${lock.nodes.${nodeName}.locked.rev}.tar.gz";
      sha256 = lock.nodes.${nodeName}.locked.narHash;
    }
  )
  { src = ./.; }
).shellNix
```

**Add to `flake.nix` inputs:**

```nix
inputs.flake-compat = {
  url = "github:NixOS/flake-compat";
  flake = false;   # ← important: flake-compat is not itself a flake; don't evaluate it as one
};
```

The `flake = false` prevents Nix from trying to call `flake-compat`'s outputs function (it has none — it's a plain Nix file). It is included purely to lock a specific revision of `flake-compat` in `flake.lock`.

### What flake-compat Evaluates

When `{ src = ./.; }` is passed, `flake-compat` does the following:

1. Reads `src + "/flake.lock"` as JSON
2. For each input declared in `flake.nix`, fetches the locked version using an internal `fetchTree` polyfill that handles `github:`, `git:`, `path:`, `tarball:`, `gitlab:`, and `sourcehut:` input types
3. Resolves `follows` chains by tracing through the lock file's node graph
4. Calls `(import (src + "/flake.nix")).outputs { self = result; <inputs...> }`
5. Returns an attrset:

```
{
  defaultNix  → outputs.defaultPackage.${system}  (legacy) or outputs.packages.${system}.default
  shellNix    → outputs.devShell.${system}         (legacy) or outputs.devShells.${system}.default
  outputs     → the full flake outputs attrset (access any output directly)
}
```

`defaultNix` and `shellNix` each include a `.default` attribute pointing to the current platform's package/shell, plus the full `outputs` attrset merged in.

### Caveats and Limitations

**`self.rev` and `self.shortRev` are unavailable.** flake-compat constructs a `self` object from the source directory but does not have access to git metadata. Any code that uses `self.rev or "dev"` will always get `"dev"`. This is a fundamental limitation: `self.rev` requires the flake evaluator to query git, which flake-compat's polyfill does not do.

**`follows` are resolved from `flake.lock`, not re-applied.** flake-compat reads the lock file as it exists — it does not re-interpret `follows` declarations from scratch. If the lock file is up-to-date (produced by `nix flake update`), the resolved inputs match what the real flake evaluator would use. If the lock is stale or inconsistent, flake-compat uses the stale version.

**Inputs are fetched fresh on each evaluation.** Unlike `nix develop` or `nix build`, which use the Nix daemon's store cache, `nix-shell` with flake-compat runs `fetchTarball` which has its own separate cache (`~/.cache/nix/tarballs/`). On the first run, all inputs are downloaded. Subsequent runs use the tarball cache, which is not the Nix store and is not managed by `nix-collect-garbage`.

**No `nixConfig` support.** The `nixConfig` output in `flake.nix` (which configures extra binary caches, public keys, etc.) is ignored by flake-compat. Legacy `nix-shell` uses `~/.config/nix/nix.conf` exclusively.

**No `@hostname` or flake registry.** flake-compat only understands `{ src = ./.; }` — absolute or relative paths. Flake references like `github:user/repo` or registry shortcuts cannot be used as the `src` argument.

**`nix-systems` interaction.** The `nix-systems` pattern (using an input that exposes `lib.toList` of systems) works fine with flake-compat as long as the `nix-systems` input is listed in `flake.nix` and locked in `flake.lock`. flake-compat fetches all declared inputs including `nix-systems`.

### `NixOS/flake-compat` vs `edolstra/flake-compat`

The project migrated from `github:edolstra/flake-compat` to `github:NixOS/flake-compat` (official NixOS org). The community fork at `github:nix-community/flake-compat` is no longer maintained — it redirects to the NixOS version.

**Functional changes in the current version vs the original:**

| Feature | Original (`edolstra`) | Current (`NixOS`) |
|---------|----------------------|-------------------|
| Lock file versions | v4 only | v4, v5, v6, v7 |
| Input type support | github, git, path, tarball | + gitlab, sourcehut |
| `follows` resolution | Partial (root follows only) | Full transitive follows via `allNodes` graph |
| `outputs` attribute | Not exposed | Exposed directly |
| `flake = false` convention | Required | Required |

The current version handles modern lock file formats (Nix 2.19+ generates v7 lock files) correctly. The original version would fail silently on newer lock formats.

### Using `outputs` Directly

For accessing non-default outputs (not `packages.default` or `devShells.default`):

```nix
# default.nix — access a specific named output
let
  compat = import (fetchTarball "...") { src = ./.; };
in
compat.outputs.packages.x86_64-linux.my-specific-package
```

This is useful for repos that expose multiple packages or have non-standard output shapes.

### `builtins.getFlake` vs flake-compat

`builtins.getFlake` (available since Nix 2.4 with `experimental-features = nix-command flakes`) is **not** flake-compat. It is the native Nix implementation. It:

- Honors the flake registry and `flake.lock`
- Provides `self.rev` and `self.shortRev` correctly
- Respects `nixConfig` (with user confirmation)
- Is NOT available in legacy `nix-shell` without `nix.conf` enabling flakes

flake-compat is the shim for users who have not enabled flakes — its entire purpose is to run in environments where `builtins.getFlake` is unavailable or blocked.

---

## Summary: When to Use Each

| Tool | Use when |
|------|---------|
| **Home Manager standalone** | Non-NixOS systems (macOS, non-NixOS Linux); want independent user config deploys; better error messages during debugging |
| **Home Manager NixOS module** | NixOS host; want atomic system+user rebuilds; `useGlobalPkgs` is mandatory for consistency |
| **Home Manager darwin module** | macOS with nix-darwin; use `home-manager.darwinModules.home-manager` |
| **nix-darwin** | macOS system config: Homebrew casks, `system.defaults`, LaunchDaemons; the NixOS equivalent for macOS |
| **flake-compat** | Repo is flake-based but must also work with `nix-shell`/`nix-build` for users without `nix-command flakes` enabled; CI systems using legacy Nix; editor integrations that don't support flakes |
