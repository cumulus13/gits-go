// File: main.go
// Author: Hadi Cahyadi <cumulus13@gmail.com>
// Date: 2026-01-03
// Description: git status wrapper
// License: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ANSI color codes
const (
	Reset      = "\033[0m"
	Bold       = "\033[1m"
	Dim        = "\033[2m"
	
	// Colors
	Red        = "\033[31m"
	Green      = "\033[32m"
	Yellow     = "\033[33m"
	Cyan       = "\033[36m"
	
	// Custom colors (using 256-color mode)
	Magenta    = "\033[38;5;201m"    // #FF00FF
	Purple     = "\033[38;5;135m"    // #AA55FF
	Blue       = "\033[38;5;27m"     // #0055FF
	Pink       = "\033[38;5;219m"    // #FFAAFF
	BrightCyan = "\033[38;5;51m"     // #00FFFF
	RedPink    = "\033[38;5;198m"    // #FF007F
)

// Icons (using Unicode symbols)
var Icons = struct {
	FOLDER  string
	ERROR   string
	INFO    string
	GIT     string
	SUCCESS string
}{
	FOLDER:  "📁",
	ERROR:   "❌",
	INFO:    "ℹ️",
	GIT:     "🌿",
	SUCCESS: "✅",
}

// FileStyles maps git status to ANSI color codes
var FileStyles = map[string]string{
	"modified": Bold + Magenta,
	"deleted":  Bold + Red,
	"new file": Bold + Green,
	"renamed":  Bold + Cyan,
	"added":    Bold + Green,
}

type Status struct{}

type ColoredText struct {
	segments []textSegment
}

type textSegment struct {
	text  string
	style string
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

// colorHeader returns (ColoredText, headerKey)
func (s *Status) colorHeader(line string) (*ColoredText, string) {
	headerStyle := Bold + Yellow
	
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
	
	// Match "    modified:   file/path"
	re := regexp.MustCompile(`^(\s*)(modified|deleted|new file|renamed|added):\s+(.+)$`)
	if matches := re.FindStringSubmatch(line); matches != nil {
		indent, status, rest := matches[1], matches[2], matches[3]
		
		ct.Append(indent, "")
		ct.Append("      "+status+": ", Bold+Yellow)
		
		// Handle rename with "->"
		if strings.Contains(rest, "->") {
			parts := strings.SplitN(rest, "->", 2)
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			
			style := FileStyles[status]
			if style == "" {
				style = ""
			}
			ct.Append(left, style)
			ct.Append(" -> ", "")
			ct.Append(right, Cyan)
		} else {
			style := FileStyles[status]
			if style == "" {
				style = ""
			}
			ct.Append(rest, style)
		}
		return ct
	}
	
	// Indented filename lines
	re2 := regexp.MustCompile(`^(\s+)(.+)$`)
	if matches := re2.FindStringSubmatch(line); matches != nil {
		indent, payload := matches[1], matches[2]
		ct.Append(indent, "")
		
		switch context {
		case "untracked":
			ct.Append("      "+payload, Bold+Purple)
		case "staged":
			ct.Append("      "+payload, Green)
		case "not_staged":
			ct.Append("      "+payload, BrightCyan)
		default:
			ct.Append("      "+payload, "")
		}
		return ct
	}
	
	// Fallback plain
	ct.Append(line, "")
	return ct
}

// ColorizeGitStatus runs git status and prints colorized output
func (s *Status) ColorizeGitStatus(cwd, remoteName string) bool {
	isGitignoreBackup := false
	
	if remoteName != "" {
		workingDir := ""
		if info, err := os.Stat(cwd); err == nil && info.IsDir() {
			workingDir = filepath.Base(cwd)
		}
		// Placeholder for GitIgnoreManager functionality
		// isGitignoreBackup = CheckGitignore(remoteName, workingDir)
		_ = workingDir
	}
	
	if cwd != "" {
		if abs, err := filepath.Abs(cwd); err == nil {
			cwd = abs
		}
	}
	
	fmt.Printf("%s %schdir:%s %s%s%s\n", 
		Icons.FOLDER, Bold+Blue, Reset, Bold+Pink, cwd, Reset)
	
	cmd := exec.Command("git", "-c", "color.status=never", "status")
	if cwd != "" {
		cmd.Dir = cwd
	}
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("%s %s%s%s\n", Icons.ERROR, Bold+RedPink, err.Error(), Reset)
		return false
	}
	
	lines := strings.Split(string(output), "\n")
	context := ""
	
	for _, line := range lines {
		// Branch line
		if matched, _ := regexp.MatchString(`^On branch (.+)$`, line); matched {
			re := regexp.MustCompile(`^On branch (.+)$`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				fmt.Printf("%s On branch %s%s %s%s\n", 
					Icons.INFO, Bold+Cyan, Icons.GIT, matches[1], Reset)
				context = ""
				continue
			}
		}
		
		// Up to date
		if strings.Contains(line, "Your branch is up to date") {
			fmt.Printf("%s %s%s%s\n", Icons.SUCCESS, Yellow, line, Reset)
			context = ""
			continue
		}
		
		// Ahead/behind
		if strings.Contains(line, "ahead") || strings.Contains(line, "behind") || strings.Contains(line, "diverged") {
			fmt.Printf("%s%s%s\n", Yellow, line, Reset)
			context = ""
			continue
		}
		
		// Header detection
		if headerText, key := s.colorHeader(line); headerText != nil {
			fmt.Println(headerText.String())
			context = key
			continue
		}
		
		// Hints
		if matched, _ := regexp.MatchString(`^\s*\(use "git .*"\)`, line); matched {
			fmt.Printf("%s%s%s\n", Dim, line, Reset)
			continue
		}
		
		// Nothing to commit
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "nothing to commit") || strings.Contains(lower, "clean working tree") {
			fmt.Printf("%s %s%s%s\n", Icons.SUCCESS, Yellow, line, Reset)
			context = ""
			continue
		}
		
		// File lines
		fileText := s.colorFileLine(line, context)
		fmt.Println(fileText.String())
	}
	
	if isGitignoreBackup && remoteName != "" {
		// Placeholder for restore functionality
		// RestoreGitignore(remoteName, workingDir)
	}
	
	return true
}

func main() {
	status := &Status{}
	status.ColorizeGitStatus(".", "")
}