// File: main.go
// Author: Hadi Cahyadi <cumulus13@gmail.com>
// Date: 2026-01-03
// Description: git status wrapper with tree view, config, and remote info
// License: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/cumulus13/go-config-get/configget"
)

// ---------------------------------------------------------------------------
// ANSI helpers
// ---------------------------------------------------------------------------

const Reset = "\033[0m"
const Bold = "\033[1m"
const Dim = "\033[2m"

// Standard fallback colors (used when config is absent)
const (
	Red        = "\033[31m"
	Green      = "\033[32m"
	Yellow     = "\033[33m"
	Cyan       = "\033[36m"
	Magenta    = "\033[38;5;201m"
	Purple     = "\033[38;5;135m"
	Blue       = "\033[38;5;27m"
	Pink       = "\033[38;5;219m"
	BrightCyan = "\033[38;5;51m"
	RedPink    = "\033[38;5;198m"
)

// hexToAnsi converts a CSS hex color (#RRGGBB or #RGB) to a 24-bit ANSI
// foreground escape sequence.
func hexToAnsi(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return ""
	}
	r, _ := strconv.ParseInt(hex[0:2], 16, 32)
	g, _ := strconv.ParseInt(hex[2:4], 16, 32)
	b, _ := strconv.ParseInt(hex[4:6], 16, 32)
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// ColorConfig holds the hex (or ANSI) color strings from the config file.
type ColorConfig struct {
	Modified  string `toml:"modified"`
	Deleted   string `toml:"deleted"`
	NewFile   string `toml:"new_file"`
	Renamed   string `toml:"renamed"`
	Added     string `toml:"added"`
	Untracked string `toml:"untracked"`
	Staged    string `toml:"staged"`
	NotStaged string `toml:"not_staged"`
	Header    string `toml:"header"`
	Branch    string `toml:"branch"`
	UpToDate  string `toml:"up_to_date"`
	AheadBehind string `toml:"ahead_behind"`
	Hint      string `toml:"hint"`
	CwdLabel  string `toml:"cwd_label"`
	CwdPath   string `toml:"cwd_path"`
	RemoteURL string `toml:"remote_url"`
	RemotePR  string `toml:"remote_pr"`
	RemoteIssue string `toml:"remote_issue"`
	Arrow     string `toml:"arrow"`
	TreeDir   string `toml:"tree_dir"`
	TreeFile  string `toml:"tree_file"`
}

type AppConfig struct {
	TreeMode bool        `toml:"tree_mode"`
	Colors   ColorConfig `toml:"colors"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() AppConfig {
	return AppConfig{
		TreeMode: true,
		Colors: ColorConfig{
			Modified:    "#FF00FF",
			Deleted:     "#FF4444",
			NewFile:     "#00FF88",
			Renamed:     "#00FFFF",
			Added:       "#00FF88",
			Untracked:   "#AA55FF",
			Staged:      "#00FF88",
			NotStaged:   "#00FFFF",
			Header:      "#FFFF00",
			Branch:      "#00FFFF",
			UpToDate:    "#FFFF00",
			AheadBehind: "#FFFF00",
			Hint:        "", // dim
			CwdLabel:    "#0055FF",
			CwdPath:     "#FFAAFF",
			RemoteURL:   "#00FFFF",
			RemotePR:    "#00FF88",
			RemoteIssue: "#FFAA00",
			Arrow:       "#FFFFFF",
			TreeDir:	 "#0055FF",
			TreeFile:    "#00FFFF",
		},
	}
}

// getFileEmoji returns an emoji based on file extension
func getFileEmoji(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "🔵"
	case ".py":
		return "🐍"
	case ".js", ".jsx", ".ts", ".tsx":
		return "💛"
	case ".rs":
		return "🦀"
	case ".rb":
		return "💎"
	case ".java", ".kt":
		return "☕"
	case ".c", ".cpp", ".h", ".hpp":
		return "⚙️"
	case ".md", ".txt", ".rst":
		return "📝"
	case ".json":
		return "📋"
	case ".yaml", ".yml":
		return "⚡"
	case ".toml":
		return "🔧"
	case ".xml":
		return "📰"
	case ".html", ".htm":
		return "🌐"
	case ".css", ".scss", ".sass", ".less":
		return "🎨"
	case ".svg":
		return "🖼️"
	case ".png", ".jpg", ".jpeg", ".gif", ".ico", ".bmp", ".webp":
		return "🖼️"
	case ".mp3", ".wav", ".ogg", ".flac":
		return "🎵"
	case ".mp4", ".avi", ".mkv", ".mov":
		return "🎬"
	case ".zip", ".tar", ".gz", ".bz2", ".7z", ".rar":
		return "📦"
	case ".sh", ".bash", ".zsh":
		return "💻"
	case ".lock":
		return "🔒"
	case ".gitignore", ".dockerignore":
		return "🙈"
	case "dockerfile", ".dockerfile":
		return "🐳"
	case "makefile", ".makefile":
		return "🔨"
	case "license", ".license":
		return "📜"
	default:
		// Default emoji for directories vs files
		if filename == "" || strings.HasSuffix(filename, "/") {
			return "📁"
		}
		return "📄"
	}
}

// getDirEmoji returns folder emoji (can be extended later)
func getDirEmoji() string {
	return "📁"
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

// LoadConfig reads ~/.gits.toml (or the XDG path) and merges with defaults.
// func LoadConfig() AppConfig {
// 	cfg := DefaultConfig()

// 	home, err := os.UserHomeDir()
// 	if err != nil {
// 		return cfg
// 	}

// 	paths := []string{
// 		filepath.Join(home, ".gits.toml"),
// 		filepath.Join(home, ".config", "gits", "config.toml"),
// 	}
// 	for _, p := range paths {
// 		data, err := os.ReadFile(p)
// 		if err != nil {
// 			continue
// 		}
// 		if err := toml.Unmarshal(data, &cfg); err == nil {
// 			break
// 		}
// 	}
// 	return cfg
// }

func LoadConfig() AppConfig {
    cfg := DefaultConfig()

    path, err := configget.GetConfigFile(".gits.toml", "gits", configget.Options{Create: true})
    if err != nil {
        return cfg
    }

    fmt.Printf("Load Config File: %s\n", path)

    if !IsFile(path) {
        return cfg
    }

    // Read TOML files directly with BurntSushi/toml
    data, err := os.ReadFile(path)
    if err != nil {
        fmt.Printf("Error reading config: %v\n", err)
        return cfg
    }

    if err := toml.Unmarshal(data, &cfg); err != nil {
        fmt.Printf("Error parsing config: %v\n", err)
        return cfg
    }

    return cfg
}

// resolveColor returns a Bold + hex-based ANSI code.  If the value is empty
// it returns an empty string (no colour).
func resolveColor(hex string) string {
	if hex == "" {
		return ""
	}
	return hexToAnsi(hex)
}

// ---------------------------------------------------------------------------
// Icons
// ---------------------------------------------------------------------------

var Icons = struct {
	FOLDER  string
	ERROR   string
	INFO    string
	GIT     string
	SUCCESS string
	WARNING string
	REMOTE  string
	PR      string
	ISSUE   string
}{
	FOLDER:  "📁",
	ERROR:   "❌",
	INFO:    "ℹ️",
	GIT:     "🌿",
	SUCCESS: "✅",
	WARNING: "⚠️",
	REMOTE:  "🔗",
	PR:      "🔀",
	ISSUE:   "🐛",
}

// ---------------------------------------------------------------------------
// ColoredText builder
// ---------------------------------------------------------------------------

type textSegment struct {
	text  string
	style string
}

type ColoredText struct {
	segments []textSegment
}

func NewColoredText() *ColoredText {
	return &ColoredText{segments: make([]textSegment, 0)}
}

func (ct *ColoredText) Append(text, style string) {
	ct.segments = append(ct.segments, textSegment{text, style})
}

func (ct *ColoredText) String() string {
	var sb strings.Builder
	for _, seg := range ct.segments {
		if seg.style != "" {
			sb.WriteString(seg.style)
		}
		sb.WriteString(seg.text)
		if seg.style != "" {
			sb.WriteString(Reset)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Tree builder for untracked files
// ---------------------------------------------------------------------------

type treeNode struct {
	name     string
	children map[string]*treeNode
	isDir    bool
}

func newTreeNode(name string, isDir bool) *treeNode {
	return &treeNode{name: name, children: map[string]*treeNode{}, isDir: isDir}
}

// insertPath adds a slash-separated path into the tree.
func insertPath(root *treeNode, path string) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	isDir := strings.HasSuffix(path, "/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")

	cur := root
	for i, part := range parts {
		if part == "" {
			continue
		}
		child, ok := cur.children[part]
		if !ok {
			childIsDir := isDir || i < len(parts)-1
			child = newTreeNode(part, childIsDir)
			cur.children[part] = child
		}
		cur = child
	}
}

// renderTree prints the tree recursively.
// func renderTree(node *treeNode, prefix string, isLast bool, color string, depth int) {
// 	if depth > 0 {
// 		connector := "├── "
// 		if isLast {
// 			connector = "└── "
// 		}
// 		label := node.name
// 		if node.isDir {
// 			label += "/"
// 		}
// 		ct := NewColoredText()
// 		ct.Append(prefix+connector, Dim)
// 		ct.Append(label, Bold+color)
// 		fmt.Println(ct.String())
// 	}

// 	// Sort children: directories first, then files
// 	keys := make([]string, 0, len(node.children))
// 	for k := range node.children {
// 		keys = append(keys, k)
// 	}
// 	sort.Slice(keys, func(i, j int) bool {
// 		a, b := node.children[keys[i]], node.children[keys[j]]
// 		if a.isDir != b.isDir {
// 			return a.isDir
// 		}
// 		return keys[i] < keys[j]
// 	})

// 	childPrefix := prefix
// 	if depth > 0 {
// 		if isLast {
// 			childPrefix += "    "
// 		} else {
// 			childPrefix += "│   "
// 		}
// 	}

// 	for i, k := range keys {
// 		renderTree(node.children[k], childPrefix, i == len(keys)-1, color, depth+1)
// 	}
// }

// renderTree prints the tree recursively with separate colors for files/dirs and emojis
func renderTree(node *treeNode, prefix string, isLast bool, dirColor, fileColor string, depth int) {
	if depth > 0 {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		
		label := node.name
		emoji := ""
		color := fileColor
		
		if node.isDir {
			emoji = getDirEmoji() + " "
			color = dirColor
			if !strings.HasSuffix(label, "/") {
				label += "/"
			}
		} else {
			emoji = getFileEmoji(node.name) + " "
		}
		
		ct := NewColoredText()
		ct.Append(prefix+connector, Dim)
		ct.Append(emoji, "") // emoji without color styling
		ct.Append(label, Bold+color)
		fmt.Println(ct.String())
	}

	// Sort children: directories first, then files
	keys := make([]string, 0, len(node.children))
	for k := range node.children {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := node.children[keys[i]], node.children[keys[j]]
		if a.isDir != b.isDir {
			return a.isDir
		}
		return keys[i] < keys[j]
	})

	childPrefix := prefix
	if depth > 0 {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, k := range keys {
		renderTree(node.children[k], childPrefix, i == len(keys)-1, dirColor, fileColor, depth+1)
	}
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

type Status struct {
	cfg AppConfig
}

func NewStatus(cfg AppConfig) *Status {
	return &Status{cfg: cfg}
}

func (s *Status) fileStyles() map[string]string {
	c := s.cfg.Colors
	return map[string]string{
		"modified": Bold + resolveColor(c.Modified),
		"deleted":  Bold + resolveColor(c.Deleted),
		"new file": Bold + resolveColor(c.NewFile),
		"renamed":  Bold + resolveColor(c.Renamed),
		"added":    Bold + resolveColor(c.Added),
	}
}

// colorHeader returns (ColoredText, headerKey)
func (s *Status) colorHeader(line string) (*ColoredText, string) {
	headerStyle := Bold + resolveColor(s.cfg.Colors.Header)

	patterns := []struct {
		regex string
		key   string
	}{
		{`^\s*Changes to be committed:`, "staged"},
		{`^\s*Changes not staged for commit:`, "not_staged"},
		{`^\s*Untracked files:`, "untracked"},
		{`^\s*no changes added to commit`, ""},
		{`^\s*.+:$`, ""},
	}

	for _, p := range patterns {
		if matched, _ := regexp.MatchString(p.regex, line); matched {
			ct := NewColoredText()
			ct.Append("    "+line, headerStyle)
			return ct, p.key
		}
	}
	return nil, ""
}

// colorFileLine styles file status lines
func (s *Status) colorFileLine(line, context string) *ColoredText {
	ct := NewColoredText()
	c := s.cfg.Colors
	styles := s.fileStyles()

	re := regexp.MustCompile(`^(\s*)(modified|deleted|new file|renamed|added):\s+(.+)$`)
	if matches := re.FindStringSubmatch(line); matches != nil {
		indent, status, rest := matches[1], matches[2], matches[3]
		ct.Append(indent, "")
		ct.Append("      "+status+": ", Bold+resolveColor(c.Header))

		if strings.Contains(rest, "->") {
			parts := strings.SplitN(rest, "->", 2)
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			ct.Append(left, styles[status])
			ct.Append(" -> ", Bold+resolveColor(c.Arrow))
			ct.Append(right, Bold+resolveColor(c.Renamed))
		} else {
			ct.Append(rest, styles[status])
		}
		return ct
	}

	re2 := regexp.MustCompile(`^(\s+)(.+)$`)
	if matches := re2.FindStringSubmatch(line); matches != nil {
		indent, payload := matches[1], matches[2]
		ct.Append(indent, "")
		switch context {
		case "untracked":
			ct.Append("      "+payload, Bold+resolveColor(c.Untracked))
		case "staged":
			ct.Append("      "+payload, Bold+resolveColor(c.Staged))
		case "not_staged":
			ct.Append("      "+payload, Bold+resolveColor(c.NotStaged))
		default:
			ct.Append("      "+payload, "")
		}
		return ct
	}

	ct.Append(line, "")
	return ct
}

// ColorizeGitStatus runs git status and prints colorized output.
func (s *Status) ColorizeGitStatus(cwd, remoteName string) bool {
	c := s.cfg.Colors

	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
	}

	fmt.Printf("%s %schdir:%s %s%s%s\n",
		Icons.FOLDER,
		Bold+resolveColor(c.CwdLabel), Reset,
		Bold+resolveColor(c.CwdPath), cwd, Reset)

	cmd := exec.Command("git", "-c", "color.status=never", "status")
	if cwd != "" {
		cmd.Dir = cwd
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s %s%s%s\n", Icons.ERROR, Bold+resolveColor(c.Deleted), err.Error(), Reset)
		return false
	}

	lines := strings.Split(string(output), "\n")
	context := ""
	var untrackedFiles []string
	inUntracked := false

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		// Branch line
		if matched, _ := regexp.MatchString(`^On branch (.+)$`, line); matched {
			re := regexp.MustCompile(`^On branch (.+)$`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				fmt.Printf("%s On branch %s%s %s%s\n",
					Icons.INFO,
					Bold+resolveColor(c.Branch), Icons.GIT,
					matches[1], Reset)
				context = ""
				inUntracked = false
				continue
			}
		}

		// "HEAD detached" line
		if strings.Contains(line, "HEAD detached") {
			fmt.Printf("%s %s%s%s\n", Icons.WARNING, Bold+resolveColor(c.AheadBehind), line, Reset)
			continue
		}

		// No commits yet
		if strings.Contains(line, "No commits yet") {
			fmt.Printf("%s %s%s%s\n", Icons.WARNING, Bold+resolveColor(c.AheadBehind), line, Reset)
			continue
		}

		// Up to date
		if strings.Contains(line, "Your branch is up to date") {
			fmt.Printf("%s %s%s%s\n", Icons.SUCCESS, resolveColor(c.UpToDate), line, Reset)
			context = ""
			inUntracked = false
			continue
		}

		// Ahead/behind/diverged
		if strings.Contains(line, "ahead") || strings.Contains(line, "behind") || strings.Contains(line, "diverged") {
			fmt.Printf("%s%s%s\n", resolveColor(c.AheadBehind), line, Reset)
			context = ""
			inUntracked = false
			continue
		}

		// Header detection
		if headerText, key := s.colorHeader(line); headerText != nil {
			// Before switching away from untracked, flush tree
			if inUntracked && s.cfg.TreeMode {
				s.flushUntrackedTree(untrackedFiles, cwd)
				untrackedFiles = nil
			}
			fmt.Println(headerText.String())
			context = key
			inUntracked = key == "untracked"
			continue
		}

		// Hints: any line starting with (use "git ..."
		// The prefix check is intentionally loose — some variants end with
		// plain text (e.g. "...to include in what will be committed)") rather
		// than with '")' so we cannot anchor to the end.
		if matched, _ := regexp.MatchString(`^\s*\(use "git `, line); matched {
			if inUntracked && s.cfg.TreeMode {
				// Inside the untracked tree block: suppress — the tree speaks for itself.
				continue
			}
			// All other contexts: print dimmed with consistent 4-space indent.
			trimmed := strings.TrimSpace(line)
			fmt.Printf("    %s%s%s\n", Dim, trimmed, Reset)
			continue
		}

		// Terminal status lines — any of the three "nothing to do" variants:
		//   "nothing to commit, working tree clean"
		//   "nothing added to commit but untracked files present ..."
		//   "no changes added to commit (use "git add" and/or "git commit -a")"
		lower := strings.ToLower(strings.TrimSpace(line))
		isTerminalStatus := strings.HasPrefix(lower, "nothing to commit") ||
			strings.HasPrefix(lower, "nothing added to commit") ||
			strings.HasPrefix(lower, "no changes added to commit") ||
			strings.Contains(lower, "clean working tree")
		if isTerminalStatus {
			if inUntracked && s.cfg.TreeMode {
				s.flushUntrackedTree(untrackedFiles, cwd)
				untrackedFiles = nil
				inUntracked = false
			}
			fmt.Printf("%s %s%s%s\n", Icons.SUCCESS, resolveColor(c.UpToDate), line, Reset)
			context = ""
			continue
		}

		// Collect untracked file paths for tree rendering
		if context == "untracked" && s.cfg.TreeMode {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				untrackedFiles = append(untrackedFiles, trimmed)
			}
			continue
		}

		// Normal file line
		fileText := s.colorFileLine(line, context)
		fmt.Println(fileText.String())
	}

	// Flush any remaining untracked files
	if inUntracked && s.cfg.TreeMode && len(untrackedFiles) > 0 {
		s.flushUntrackedTree(untrackedFiles, cwd)
	}

	return true
}

// gitUntrackedUnder runs `git ls-files --others --exclude-standard` inside
// subDir (relative to repoRoot) and returns paths relative to repoRoot.
// This respects .gitignore exactly the same way `git status` does.
func gitUntrackedUnder(repoRoot, subDir string) []string {
	cmd := exec.Command(
		"git", "ls-files",
		"--others",
		"--exclude-standard",
		"--",
		subDir,
	)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line != "" {
			result = append(result, filepath.ToSlash(line))
		}
	}
	return result
}

// flushUntrackedTree renders collected untracked paths as an ASCII tree.
// Directories reported by git (e.g. "src/") are expanded via
// `git ls-files --others --exclude-standard` so .gitignore is respected.
// func (s *Status) flushUntrackedTree(paths []string, cwd string) {
// 	color := resolveColor(s.cfg.Colors.Untracked)
// 	root := newTreeNode(".", true)

// 	for _, p := range paths {
// 		clean := filepath.ToSlash(strings.TrimSpace(p))
// 		isDir := strings.HasSuffix(clean, "/")
// 		clean = strings.TrimSuffix(clean, "/")

// 		if isDir {
// 			// Ask git for the real untracked contents under this directory,
// 			// honouring .gitignore — never walk the filesystem directly.
// 			subPaths := gitUntrackedUnder(cwd, clean)
// 			if len(subPaths) == 0 {
// 				// git gave us the dir name but returned nothing — insert the
// 				// dir node alone so it still appears in the tree.
// 				insertPath(root, clean+"/")
// 			} else {
// 				for _, sp := range subPaths {
// 					insertPath(root, sp)
// 				}
// 			}
// 		} else {
// 			insertPath(root, clean)
// 		}
// 	}

// 	// Label + render
// 	ct := NewColoredText()
// 	ct.Append("        . (untracked root)", Dim)
// 	fmt.Println(ct.String())
// 	renderTree(root, "        ", true, color, 0)
// }

// flushUntrackedTree renders collected untracked paths as an ASCII tree.
func (s *Status) flushUntrackedTree(paths []string, cwd string) {
	dirColor := resolveColor(s.cfg.Colors.TreeDir)
	fileColor := resolveColor(s.cfg.Colors.TreeFile)
	
	root := newTreeNode(".", true)

	for _, p := range paths {
		clean := filepath.ToSlash(strings.TrimSpace(p))
		isDir := strings.HasSuffix(clean, "/")
		clean = strings.TrimSuffix(clean, "/")

		if isDir {
			// Ask git for the real untracked contents under this directory,
			// honouring .gitignore — never walk the filesystem directly.
			subPaths := gitUntrackedUnder(cwd, clean)
			if len(subPaths) == 0 {
				// git gave us the dir name but returned nothing — insert the
				// dir node alone so it still appears in the tree.
				insertPath(root, clean+"/")
			} else {
				for _, sp := range subPaths {
					insertPath(root, sp)
				}
			}
		} else {
			insertPath(root, clean)
		}
	}

	// Label + render
	ct := NewColoredText()
	ct.Append("        . (untracked root)", Dim)
	fmt.Println(ct.String())
	renderTree(root, "        ", true, dirColor, fileColor, 0)
}

// ---------------------------------------------------------------------------
// Remote info via GitHub API
// ---------------------------------------------------------------------------

// isPathLike returns true when s looks like a filesystem path rather than a
// remote name or URL.  Handles: ".", "..", "./foo", "..\foo", absolute paths,
// and any string that resolves to an existing directory on disk.
func isPathLike(s string) bool {
	if s == "." || s == ".." {
		return true
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, ".\\") ||
		strings.HasPrefix(s, "../") || strings.HasPrefix(s, "..\\") {
		return true
	}
	// Absolute path (Unix or Windows)
	if filepath.IsAbs(s) {
		return true
	}
	// Exists as a directory on disk
	if info, err := os.Stat(s); err == nil && info.IsDir() {
		return true
	}
	return false
}

// parseRemote tries to extract owner/repo from various input formats.
// Supported:
//   - "." / ".." / any path  → treated as cwd; resolves origin from that dir
//   - https://github.com/owner/repo[.git]
//   - git@github.com:owner/repo[.git]
//   - owner/repo
//   - reponame / remote-name  (resolved via `git remote get-url`)
func parseRemote(input, cwd string) (owner, repo string, ok bool) {
	// --- Path-as-cwd shortcut -------------------------------------------
	// "gits -r ."  or  "gits -r /some/dir"  means: use that dir as cwd and
	// resolve the remote from its `origin`.
	if isPathLike(input) {
		resolvedCwd := input
		if abs, err := filepath.Abs(input); err == nil {
			resolvedCwd = abs
		}
		// Recurse with empty input so we fall through to the origin lookup,
		// but use the path as the working directory.
		return parseRemote("", resolvedCwd)
	}

	// Strip trailing .git for all patterns below
	input = strings.TrimSuffix(input, ".git")

	// Full HTTPS
	re := regexp.MustCompile(`https://github\.com/([^/]+)/([^/]+)`)
	if m := re.FindStringSubmatch(input); m != nil {
		return m[1], m[2], true
	}
	// SSH
	re2 := regexp.MustCompile(`git@github\.com:([^/]+)/(.+)`)
	if m := re2.FindStringSubmatch(input); m != nil {
		return m[1], m[2], true
	}
	// owner/repo  (two slash-separated tokens that are NOT a filesystem path)
	if strings.Contains(input, "/") {
		parts := strings.SplitN(input, "/", 2)
		return parts[0], parts[1], true
	}
	// Plain remote name (or empty → "origin") — resolve via git
	remotes := []string{input, "origin"}
	for _, r := range remotes {
		if r == "" {
			continue
		}
		cmd := exec.Command("git", "remote", "get-url", r)
		if cwd != "" {
			cmd.Dir = cwd
		}
		out, err := cmd.Output()
		if err == nil {
			url := strings.TrimSpace(string(out))
			if o, rp, ok2 := parseRemote(url, ""); ok2 {
				return o, rp, true
			}
		}
	}
	return "", "", false
}

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	HTMLURL string `json:"html_url"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Draft bool `json:"draft"`
}

type ghIssue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	HTMLURL  string `json:"html_url"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

type ghRepo struct {
	FullName        string `json:"full_name"`
	Description     string `json:"description"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
	OpenIssuesCount int    `json:"open_issues_count"`
	DefaultBranch   string `json:"default_branch"`
	HTMLURL         string `json:"html_url"`
}

func ghGet(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gits-go/1.0")
	// If GITHUB_TOKEN is set, use it to avoid rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ShowRemoteInfo fetches and prints GitHub repo info, open PRs and issues.
func (s *Status) ShowRemoteInfo(input, cwd string) {
	c := s.cfg.Colors

	// If a path was given as input (or input is empty meaning "use cwd"),
	// show which directory we are resolving the remote from.
	if input == "" || isPathLike(input) {
		resolvedCwd := cwd
		if abs, err := filepath.Abs(cwd); err == nil {
			resolvedCwd = abs
		}
		fmt.Printf("%s %sResolving remote from:%s %s%s%s\n",
			Icons.FOLDER,
			Bold+resolveColor(c.CwdLabel), Reset,
			Bold+resolveColor(c.CwdPath), resolvedCwd, Reset)
	}

	owner, repo, ok := parseRemote(input, cwd)
	if !ok {
		if input == "" || isPathLike(input) {
			fmt.Printf("%s No GitHub remote found (is there an `origin` with a github.com URL?)\n", Icons.ERROR)
		} else {
			fmt.Printf("%s Cannot resolve remote from %q\n", Icons.ERROR, input)
		}
		return
	}

	repoSlug := owner + "/" + repo
	fmt.Printf("%s %s%s%s\n", Icons.REMOTE,
		Bold+resolveColor(c.RemoteURL), "https://github.com/"+repoSlug, Reset)

	// Repo info
	data, err := ghGet("https://api.github.com/repos/" + repoSlug)
	if err != nil {
		fmt.Printf("%s GitHub API error: %v\n", Icons.ERROR, err)
		return
	}
	var ghR ghRepo
	if err := json.Unmarshal(data, &ghR); err == nil && ghR.FullName != "" {
		fmt.Printf("   %s★ Stars:%s %d   %s⑂ Forks:%s %d   %s● Open issues:%s %d   %sDefault branch:%s %s\n",
			Bold+resolveColor(c.UpToDate), Reset, ghR.StargazersCount,
			Bold+resolveColor(c.RemotePR), Reset, ghR.ForksCount,
			Bold+resolveColor(c.RemoteIssue), Reset, ghR.OpenIssuesCount,
			Bold+resolveColor(c.Branch), Reset, ghR.DefaultBranch,
		)
		if ghR.Description != "" {
			fmt.Printf("   %s%s%s\n", Dim, ghR.Description, Reset)
		}
	}

	// Open PRs
	fmt.Printf("\n%s %sOpen Pull Requests%s\n", Icons.PR,
		Bold+resolveColor(c.RemotePR), Reset)
	prData, err := ghGet("https://api.github.com/repos/" + repoSlug + "/pulls?state=open&per_page=10")
	if err == nil {
		var prs []ghPR
		if json.Unmarshal(prData, &prs) == nil {
			if len(prs) == 0 {
				fmt.Printf("   %s(none)%s\n", Dim, Reset)
			}
			for _, pr := range prs {
				draft := ""
				if pr.Draft {
					draft = Dim + " [draft]" + Reset
				}
				fmt.Printf("   %s#%d%s %s%s%s%s %s— %s@%s%s\n",
					Bold+resolveColor(c.RemotePR), pr.Number, Reset,
					Bold, pr.Title, Reset,
					draft,
					Dim, resolveColor(c.Branch), pr.User.Login, Reset,
				)
				fmt.Printf("      %s%s%s\n", Dim, pr.HTMLURL, Reset)
			}
		}
	}

	// Open Issues (exclude PRs)
	fmt.Printf("\n%s %sOpen Issues%s\n", Icons.ISSUE,
		Bold+resolveColor(c.RemoteIssue), Reset)
	issData, err := ghGet("https://api.github.com/repos/" + repoSlug + "/issues?state=open&per_page=10")
	if err == nil {
		var issues []ghIssue
		if json.Unmarshal(issData, &issues) == nil {
			count := 0
			for _, iss := range issues {
				if iss.PullRequest != nil {
					continue // skip PRs listed as issues
				}
				count++
				fmt.Printf("   %s#%d%s %s%s%s\n",
					Bold+resolveColor(c.RemoteIssue), iss.Number, Reset,
					Bold, iss.Title, Reset,
				)
				fmt.Printf("      %s%s%s\n", Dim, iss.HTMLURL, Reset)
			}
			if count == 0 {
				fmt.Printf("   %s(none)%s\n", Dim, Reset)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  gits [path]                    - show git status (colorized, tree mode)")
	fmt.Println("  gits -r [remote] [path]        - show GitHub remote info for a repo")
	fmt.Println("")
	fmt.Println("  [remote] can be:")
	fmt.Println("    .                   (current dir — resolves origin automatically)")
	fmt.Println("    /path/to/repo       (any dir — resolves origin automatically)")
	fmt.Println("    owner/repo")
	fmt.Println("    reponame            (resolved via git remote get-url)")
	fmt.Println("    https://github.com/owner/repo")
	fmt.Println("    git@github.com:owner/repo")
	fmt.Println("")
	fmt.Println("Config: ~/.gits.toml  (see --dump-config for example)")
	fmt.Println("")
	fmt.Println("Env: GITHUB_TOKEN   - set to avoid rate limits on -r")
}

func dumpConfig(cfg AppConfig) {
	data, _ := toml.Marshal(cfg)
	fmt.Print(string(data))
}

func main() {
	cfg := LoadConfig()
	status := NewStatus(cfg)

	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			printUsage()
			return
		case "--dump-config":
			dumpConfig(cfg)
			return
		case "--tree":
			cfg.TreeMode = true
			status = NewStatus(cfg)
			args = args[1:]
		case "--no-tree":
			cfg.TreeMode = false
			status = NewStatus(cfg)
			args = args[1:]
		case "-r", "--remote":
			// Accepted forms:
			//   gits -r                        -> origin of cwd "."
			//   gits -r .                      -> origin of "." (path-as-cwd)
			//   gits -r /some/dir              -> origin of that dir
			//   gits -r owner/repo             -> explicit slug
			//   gits -r reponame               -> git remote get-url reponame
			//   gits -r owner/repo /some/dir   -> slug + explicit cwd
			remoteInput := ""
			cwd := "."
			if len(args) >= 2 {
				arg1 := args[1]
				if isPathLike(arg1) {
					// path given as first arg: use it as cwd, resolve origin from there
					cwd = arg1
					// remoteInput stays "", parseRemote will fall through to origin
				} else {
					remoteInput = arg1
					if len(args) >= 3 {
						cwd = args[2]
					}
				}
			}
			status.ShowRemoteInfo(remoteInput, cwd)
			return
		}
	}

	targetDir := "."
	if len(args) > 0 {
		targetDir = args[0]
	}
	remoteName := ""
	if len(args) > 1 {
		remoteName = args[1]
	}

	status.ColorizeGitStatus(targetDir, remoteName)
}