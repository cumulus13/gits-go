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
		},
	}
}

// LoadConfig reads ~/.gits.toml (or the XDG path) and merges with defaults.
func LoadConfig() AppConfig {
	cfg := DefaultConfig()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}
	paths := []string{
		filepath.Join(home, ".gits.toml"),
		filepath.Join(home, ".config", "gits", "config.toml"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if err := toml.Unmarshal(data, &cfg); err == nil {
			break
		}
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
func renderTree(node *treeNode, prefix string, isLast bool, color string, depth int) {
	if depth > 0 {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		label := node.name
		if node.isDir {
			label += "/"
		}
		ct := NewColoredText()
		ct.Append(prefix+connector, Dim)
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
		renderTree(node.children[k], childPrefix, i == len(keys)-1, color, depth+1)
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

		// Hints: lines starting with (use "git ..." — note the regex only checks
		// the prefix because some variants end with text rather than with '")'
		if matched, _ := regexp.MatchString(`^\s*\(use "git `, line); matched {
			// In tree mode, suppress the hint inside the untracked section
			// (it was already printed implicitly via the header block).
			// Outside tree mode, or for other sections, print it dimmed.
			if !inUntracked || !s.cfg.TreeMode {
				fmt.Printf("%s%s%s\n", Dim, line, Reset)
			}
			continue
		}

		// Nothing to commit / nothing added to commit
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "nothing to commit") ||
			strings.HasPrefix(lower, "nothing added to commit") ||
			strings.Contains(lower, "clean working tree") {
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

// expandDir recursively walks a real directory and populates the tree node
// with its contents.  Depth is capped to avoid enormous output.
func expandDir(node *treeNode, absPath string, depth int) {
	const maxDepth = 8
	if depth > maxDepth {
		return
	}
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return
	}
	for _, e := range entries {
		child, ok := node.children[e.Name()]
		if !ok {
			child = newTreeNode(e.Name(), e.IsDir())
			node.children[e.Name()] = child
		}
		if e.IsDir() {
			child.isDir = true
			expandDir(child, filepath.Join(absPath, e.Name()), depth+1)
		}
	}
}

// flushUntrackedTree renders collected untracked paths as an ASCII tree.
// Directories reported by git (e.g. "src/") are expanded from the filesystem
// so their full contents appear in the tree.
func (s *Status) flushUntrackedTree(paths []string, cwd string) {
	color := resolveColor(s.cfg.Colors.Untracked)
	root := newTreeNode(".", true)

	for _, p := range paths {
		insertPath(root, p)

		// Expand directories from disk so their contents show in the tree
		clean := strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(p)), "/")
		absPath := filepath.Join(cwd, filepath.FromSlash(clean))
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			// Navigate to the node we just inserted
			parts := strings.Split(clean, "/")
			cur := root
			for _, part := range parts {
				if part == "" {
					continue
				}
				if child, ok2 := cur.children[part]; ok2 {
					cur = child
				}
			}
			expandDir(cur, absPath, 1)
		}
	}

	// Label + render
	ct := NewColoredText()
	ct.Append("        . (untracked root)", Dim)
	fmt.Println(ct.String())
	renderTree(root, "        ", true, color, 0)
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
