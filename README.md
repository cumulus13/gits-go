# gits-go

A colorized `git status` wrapper with tree view for untracked files, hex color
configuration, and GitHub remote info (`-r` flag).

## Features

| Feature | Description |
|---|---|
| **Tree view** | Untracked files are shown as a directory tree instead of a flat list |
| **Hex colors** | All colors configurable via `~/.gits.toml` using `#RRGGBB` values |
| **24-bit color** | Uses true-color ANSI escape sequences for exact color matching |
| **`-r` flag** | Fetch GitHub repo stats, open PRs, and open issues |
| **`--dump-config`** | Print default config to stdout so you can customize it |

## Install

```bash
go install github.com/cumulus13/gits-go@latest
```

Or build locally:

```bash
go mod tidy
go build -o gits .
```

## Usage

```
gits [path]                    show git status (default: current dir)
gits --tree [path]             force tree mode on
gits --no-tree [path]          force tree mode off
gits -r [remote] [path]        show GitHub info for the repo
gits --dump-config             print the current config (defaults + overrides)
gits -h / --help               show help
```

### `-r` remote flag

`[remote]` can be any of:

```
gits -r owner/repo
gits -r reponame           # resolved via `git remote get-url reponame`
gits -r                    # resolved via `git remote get-url origin`
gits -r https://github.com/owner/repo
gits -r git@github.com:owner/repo
gits -r owner/repo /path/to/local/clone
```

Set `GITHUB_TOKEN` in your environment to avoid GitHub API rate limits.

## Tree view example

```
    Untracked files:
        (use "git add <file>..." to include in what will be committed)
        . (untracked root)
        ├── .github/
        │   └── workflows/
        │       └── ci.yml
        ├── src/
        │   ├── main.rs
        │   └── lib.rs
        ├── tests/
        │   └── integration.rs
        ├── .gitignore
        ├── Cargo.toml
        ├── LICENSE
        ├── README.md
        ├── install.sh
        └── rustfmt.toml
```

## Config

Copy `.gits.toml.example` to `~/.gits.toml` (or `~/.config/gits/config.toml`)
and edit as needed:

```toml
tree_mode = true

[colors]
modified     = "#FF00FF"
deleted      = "#FF4444"
new_file     = "#00FF88"
untracked    = "#AA55FF"
# ... etc.
```

Run `gits --dump-config` to see all available keys with your current values.

## License

MIT

## 👤 Author
        
[Hadi Cahyadi](mailto:cumulus13@gmail.com)
    

[![Buy Me a Coffee](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/cumulus13)

[![Donate via Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/cumulus13)
 
[Support me on Patreon](https://www.patreon.com/cumulus13)