package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// skipDirs contains directory names to skip during disk scan.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".cache": true, ".local": true,
	".npm": true, ".cargo": true, ".rustup": true, "vendor": true,
	".vscode": true, ".idea": true, "__pycache__": true, ".tox": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true,
	".gradle": true, ".m2": true, "target": true, ".docker": true,
	".virtualenvs": true, ".pyenv": true, ".nvm": true, ".sdkman": true,
}

// stopWords contains common words that shouldn't match project names.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"that": true, "this": true, "into": true, "have": true, "been": true,
	"will": true, "can": true, "are": true, "was": true, "were": true,
	"uma": true, "que": true, "com": true, "para": true,
	"por": true, "como": true, "mas": true, "mais": true, "esse": true,
	"essa": true, "isto": true, "isso": true, "aqui": true, "ali": true,
	"nos": true, "vou": true, "vamos": true, "dar": true, "olhada": true,
	"olhar": true, "ver": true, "projeto": true, "project": true,
	"look": true, "let": true, "check": true, "open": true,
}

func isStopWord(w string) bool {
	return stopWords[w]
}

// looksLikeProjectName returns true if the word looks like a project/repo name.
func looksLikeProjectName(w string) bool {
	if strings.ContainsAny(w, "-_.0123456789") {
		return true
	}
	for i := 1; i < len(w); i++ {
		if w[i-1] >= 'a' && w[i-1] <= 'z' && w[i] >= 'A' && w[i] <= 'Z' {
			return true
		}
	}
	return false
}

// extractFrontmatterField extracts a field value from YAML frontmatter.
func extractFrontmatterField(content string, field string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" && !inFrontmatter {
			inFrontmatter = true
			continue
		}
		if trimmed == "---" && inFrontmatter {
			break
		}
		if inFrontmatter && strings.HasPrefix(trimmed, field+":") {
			return strings.TrimSpace(trimmed[len(field)+1:])
		}
	}
	return ""
}

// scanForProject walks home + mounted volumes looking for a directory
// whose name fuzzy-matches a word from the user's message.
func scanForProject(ctx context.Context, text string) string {
	var candidates []string
	for _, word := range strings.Fields(strings.ToLower(text)) {
		clean := strings.Trim(word, ".,!?;:()\"'/")
		if len(clean) < 3 || isStopWord(clean) {
			continue
		}
		if looksLikeProjectName(clean) {
			candidates = append(candidates, clean)
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	var roots []string
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, home)
	}
	for _, media := range []string{"/media", "/mnt"} {
		if userDirs, err := os.ReadDir(media); err == nil {
			for _, u := range userDirs {
				if u.IsDir() {
					roots = append(roots, filepath.Join(media, u.Name()))
				}
			}
		}
	}

	const maxDepth = 4

	for _, root := range roots {
		if ctx.Err() != nil {
			return ""
		}
		rootDepth := strings.Count(root, string(filepath.Separator))
		var result string

		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return filepath.SkipAll
			}
			if err != nil || !d.IsDir() {
				if err != nil && d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()

			if path != root {
				if strings.HasPrefix(name, ".") || skipDirs[name] {
					return filepath.SkipDir
				}
			}

			depth := strings.Count(path, string(filepath.Separator)) - rootDepth
			if depth > maxDepth {
				return filepath.SkipDir
			}
			if depth < 2 {
				return nil
			}

			if home != "" && path == home {
				return nil
			}

			lowerName := strings.ToLower(name)
			for _, c := range candidates {
				if lowerName == c || strings.Contains(lowerName, c) || strings.Contains(c, lowerName) {
					entries, readErr := os.ReadDir(path)
					if readErr != nil || len(entries) < 2 {
						continue
					}
					result = path
					return filepath.SkipAll
				}
			}
			return nil
		})

		if result != "" {
			return result
		}
	}

	return ""
}

// detectProjectPath tries to find a project path from the user message.
// ctx controls timeout — the function checks ctx.Err() between I/O operations.
func (bc *Service) detectProjectPath(ctx context.Context, text string) string {
	// 1. Absolute path in text
	for _, word := range strings.Fields(text) {
		if ctx.Err() != nil {
			return ""
		}
		if !filepath.IsAbs(word) {
			continue
		}
		clean := filepath.Clean(word)
		if len(strings.Split(clean, string(filepath.Separator))) < 4 {
			continue
		}
		info, err := os.Stat(clean)
		if err == nil && info.IsDir() {
			return clean
		}
	}

	// 2. Match project names from memory files
	if bc.memoryDir != "" {
		if found := bc.detectFromMemoryFiles(text); found != "" {
			return found
		}
	}

	// 3. Check project index (fast, cached)
	if bc.projectIndex != nil {
		indexMiss := false
		for _, word := range strings.Fields(text) {
			clean := strings.Trim(strings.ToLower(word), ".,!?;:()\"'/")
			if len(clean) >= 3 && !isStopWord(clean) && looksLikeProjectName(clean) {
				indexMiss = true
				if path := bc.projectIndex.Lookup(clean); path != "" {
					return path
				}
			}
		}
		if indexMiss {
			bc.projectIndex.ScheduleRebuild(30 * time.Minute)
		}
	}

	// 4. Disk-walking fallback. Off by default — adds up to 3s of latency on
	// session start and the projectIndex (step 3) already covers the common
	// cases. Opt in via config.disk_scan_enabled when fuzzy disk discovery
	// is really wanted.
	if bc.config != nil && bc.config.DiskScanEnabled && ctx.Err() == nil {
		if found := scanForProject(ctx, text); found != "" {
			return found
		}
	}

	return ""
}

// detectFromMemoryFiles searches memory files for projects mentioned in text.
func (bc *Service) detectFromMemoryFiles(text string) string {
	entries, err := os.ReadDir(bc.memoryDir)
	if err != nil {
		return ""
	}

	lower := strings.ToLower(text)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(bc.memoryDir, name))
		if err != nil {
			continue
		}
		content := string(data)

		projectName := extractFrontmatterField(content, "name")
		if projectName == "" {
			continue
		}

		lowerName := strings.ToLower(projectName)
		if !strings.Contains(lower, lowerName) && !strings.Contains(lowerName, lower) {
			found := false
			for _, word := range strings.Fields(lower) {
				clean := strings.Trim(word, ".,!?;:()\"'")
				if len(clean) >= 4 && strings.Contains(lowerName, clean) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Caminho:") || strings.HasPrefix(line, "Path:") {
				path := strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					return path
				}
			}
		}
	}
	return ""
}
