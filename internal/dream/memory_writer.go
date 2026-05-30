package dream

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const maxMemoryFileSize = 1 * 1024 * 1024 // 1MB hard limit for memory fact files

var (
	allowedLayers = map[string]bool{
		"user_global":  true,
		"topic":        true,
		"cwd_overlay":  true,
		"project_team": true,
	}

	errPersonasPath  = fmt.Errorf("path targets personas directory")
	errInvalidLayer  = fmt.Errorf("unknown memory layer")
	errBadFilename   = fmt.Errorf("invalid filename")
	errPathTraversal = fmt.Errorf("path traversal detected")
)

// safeMemoryWriter writes memory fact files under a validated memory root.
// It enforces path containment, persona exclusion, and file naming rules.
type safeMemoryWriter struct {
	memoryDir   string // global memory root (~/.aurelia/memory), symlink-resolved
	resolvedDir string // memoryDir after EvalSymlinks
	resolver    memoryDirResolver
}

// memoryDirResolver provides layer-specific subdirectories and the
// instance root directory for containment boundary resolution.
type memoryDirResolver interface {
	Root() string
	TopicMemoryDir(chatID int64, threadID int) string
	TopicCwdOverlayDir(chatID int64, threadID int) string
	TeamMemoryDir(cwd string) string
}

// newSafeMemoryWriter creates a writer. Returns error if memoryDir is not absolute.
func newSafeMemoryWriter(memoryDir string, resolver memoryDirResolver) (*safeMemoryWriter, error) {
	if !filepath.IsAbs(memoryDir) {
		return nil, fmt.Errorf("memoryDir must be absolute, got %q", memoryDir)
	}
	resolvedDir, err := filepath.EvalSymlinks(memoryDir)
	if err != nil {
		return nil, fmt.Errorf("resolve memoryDir symlinks: %w", err)
	}
	return &safeMemoryWriter{memoryDir: memoryDir, resolvedDir: resolvedDir, resolver: resolver}, nil
}

// layerTarget describes a resolved layer's base directory and containment root.
type layerTarget struct {
	base           string // where files are created
	root           string // containment boundary (symlink-resolved at use)
	blocksPersonas bool   // whether to reject paths relative to root/personas/
}

// resolveLayerTarget resolves the base directory and containment root for a layer.
func (w *safeMemoryWriter) resolveLayerTarget(layer string, chatID int64, threadID int, cwd string) (layerTarget, error) {
	switch layer {
	case "user_global":
		return layerTarget{base: w.memoryDir, root: w.memoryDir, blocksPersonas: true}, nil
	case "topic":
		if threadID <= 0 {
			return layerTarget{}, fmt.Errorf("topic layer requires threadID > 0")
		}
		dir := w.resolver.TopicMemoryDir(chatID, threadID)
		if dir == "" {
			return layerTarget{}, fmt.Errorf("topic memory directory not available")
		}
		// Use instance root (~/.aurelia/) as containment root so that topic
		// dirs (~/.aurelia/topics/...) pass the isSubDirLexical check.
		instanceRoot := w.resolver.Root()
		return layerTarget{base: dir, root: instanceRoot, blocksPersonas: true}, nil
	case "cwd_overlay":
		if cwd == "" || w.resolver == nil {
			return layerTarget{}, fmt.Errorf("cwd_overlay layer requires /cwd active")
		}
		dir := w.resolver.TopicCwdOverlayDir(chatID, threadID)
		if dir == "" {
			return layerTarget{}, fmt.Errorf("cwd_overlay directory not available (no /cwd or threadID <= 0)")
		}
		instanceRoot := w.resolver.Root()
		return layerTarget{base: dir, root: instanceRoot, blocksPersonas: true}, nil
	case "project_team":
		if cwd == "" || w.resolver == nil {
			return layerTarget{}, fmt.Errorf("project_team layer requires cwd")
		}
		dir := w.resolver.TeamMemoryDir(cwd)
		if dir == "" {
			return layerTarget{}, fmt.Errorf("team memory directory not available (no project context)")
		}
		return layerTarget{base: dir, root: dir, blocksPersonas: false}, nil
	default:
		return layerTarget{}, errInvalidLayer
	}
}

// validateFilename checks that the filename is a safe .md basename.
func validateFilename(name string) error {
	if name == "" {
		return errBadFilename
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return errBadFilename
	}
	if filepath.Base(name) != name {
		return errBadFilename
	}
	if strings.Contains(name, "..") {
		return errBadFilename
	}
	if !strings.HasSuffix(name, ".md") {
		return errBadFilename
	}
	if strings.HasPrefix(name, ".") {
		return errBadFilename
	}
	return nil
}

// applyUpdates writes one or more memory updates with full validation.
// It logs each rejected update but continues processing the rest.
// Returns the number of successfully applied updates.
func (w *safeMemoryWriter) applyUpdates(updates []memoryUpdate, chatID int64, threadID int, cwd string) int {
	applied := 0
	for _, u := range updates {
		if err := w.applyOne(u, chatID, threadID, cwd); err != nil {
			log.Printf("[nudge] rejected update %s/%s: %v", u.Layer, u.Filename, err)
		} else {
			applied++
		}
	}
	return applied
}

// applyOne writes one memory update with validation.
func (w *safeMemoryWriter) applyOne(u memoryUpdate, chatID int64, threadID int, cwd string) error {
	// 0. Sanitize title and facts at the shared writer layer so that both
	// nudge and dream consolidation paths are protected consistently.
	u.Title = sanitizeTitle(u.Title)
	var sanitized []string
	for _, f := range u.Facts {
		s := sanitizeFact(f)
		if s != "" {
			sanitized = append(sanitized, s)
		}
	}
	u.Facts = sanitized
	if len(u.Facts) > maxFactsPerFile {
		u.Facts = u.Facts[:maxFactsPerFile]
	}
	u.Facts = dedupeStrings(u.Facts)
	if len(u.Facts) == 0 {
		return fmt.Errorf("no valid facts after sanitization")
	}

	// 1. Validate layer
	if !allowedLayers[u.Layer] {
		return errInvalidLayer
	}

	// 2. Validate filename (basename .md only, no separators)
	if err := validateFilename(u.Filename); err != nil {
		return err
	}

	// 3. Resolve layer target: base + containment root + persona policy
	lt, err := w.resolveLayerTarget(u.Layer, chatID, threadID, cwd)
	if err != nil {
		return err
	}

	// 4. Build target path
	target := filepath.Join(lt.base, u.Filename)

	// 5. Lexical containment: ensure base is relative to layer root.
	// This catches obvious escapes before any I/O.
	if !isSubDirLexical(lt.root, lt.base) {
		return errPathTraversal
	}

	// 6. Create the base directory with private permissions.
	// If a malicious symlink already exists at this path, MkdirAll follows it
	// (no-op if target exists). The symlink escape is detected in step 7.
	if err := os.MkdirAll(lt.base, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	// 7. Symlink-resolved containment: resolve layer root and base symlinks,
	// then check that base and target parent stay inside the resolved root.
	rootResolved, err := filepath.EvalSymlinks(lt.root)
	if err != nil {
		return fmt.Errorf("resolve layer root symlinks: %w", err)
	}
	baseResolved, err := filepath.EvalSymlinks(lt.base)
	if err != nil {
		return fmt.Errorf("resolve base symlinks: %w", err)
	}
	targetParentResolved, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("resolve target dir symlinks: %w", err)
	}

	if !isSubDirLexical(rootResolved, baseResolved) {
		return errPathTraversal
	}
	if !isSubDirLexical(rootResolved, targetParentResolved) {
		return errPathTraversal
	}

	// 8. Personas exclusion (only for layers that block personas).
	if lt.blocksPersonas {
		rel, err := filepath.Rel(rootResolved, baseResolved)
		if err != nil || isPersonasRelPath(rel) {
			return errPersonasPath
		}
		rel, err = filepath.Rel(rootResolved, targetParentResolved)
		if err != nil || isPersonasRelPath(rel) {
			return errPersonasPath
		}
	}

	// 9. Resolve existing target symlink (H-01 residual): if the target file
	// already exists and is a symlink, EvalSymlinks reveals where it actually
	// points. Reject if it escapes the resolved root or targets personas.
	if err := w.checkTargetSymlink(lt, rootResolved, target); err != nil {
		return err
	}

	// 10-11. Write facts and update MEMORY.md index.
	if err := writeFactsAndIndex(w, lt, target, u.Facts, u.Filename, u.Title, rootResolved); err != nil {
		return err
	}

	return nil
}

// checkTargetSymlink resolves an existing file via EvalSymlinks and rejects it
// if the resolved path escapes the layer's containment root or targets personas/.
// If the file does not exist (new file), no check is needed.
func (w *safeMemoryWriter) checkTargetSymlink(lt layerTarget, rootResolved string, target string) error {
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		// File doesn't exist — new file, no symlink to check
		return nil
	}
	if !isSubDirLexical(rootResolved, resolved) {
		return errPathTraversal
	}
	if lt.blocksPersonas {
		rel, err := filepath.Rel(rootResolved, resolved)
		if err != nil || isPersonasRelPath(rel) {
			return errPersonasPath
		}
	}
	return nil
}

// writeFactsAndIndex writes facts to the target file and updates the MEMORY.md index.
// This combines steps 10-11 of applyOne to reduce its size.
func writeFactsAndIndex(w *safeMemoryWriter, lt layerTarget, target string, facts []string, filename string, title string, rootResolved string) error {
	if err := appendUniqueFacts(target, facts); err != nil {
		return fmt.Errorf("write facts: %w", err)
	}

	baseResolved, err := filepath.EvalSymlinks(lt.base)
	if err != nil {
		return fmt.Errorf("resolve base symlinks: %w", err)
	}
	if err := w.checkTargetSymlink(lt, rootResolved, filepath.Join(baseResolved, "MEMORY.md")); err != nil {
		return fmt.Errorf("MEMORY.md symlink: %w", err)
	}
	if err := updateMemoryIndex(baseResolved, filename, title); err != nil {
		return fmt.Errorf("update MEMORY.md index: %w", err)
	}
	return nil
}

// isPersonasRelPath checks if a relative path starts with "personas/".
func isPersonasRelPath(rel string) bool {
	return strings.HasPrefix(rel, "personas") && (len(rel) == 8 || rel[8] == filepath.Separator)
}

// needsLeadingNewline checks if an open file needs a leading newline before appending content.
func needsLeadingNewline(f *os.File) (bool, error) {
	stat, err := f.Stat()
	if err != nil {
		return false, err
	}
	if stat.Size() == 0 {
		return false, nil
	}
	buf := make([]byte, 1)
	if _, err := f.ReadAt(buf, stat.Size()-1); err != nil {
		return false, err
	}
	return buf[0] != '\n', nil
}

// calculateWriteBudget checks whether appending newContentSize bytes to a file of existingSize
// would exceed the hard size limit. If needsNewline is true, the leading newline byte is
// included in the budget.
func calculateWriteBudget(existingSize int64, newContentSize int64, needsNewline bool, path string) error {
	total := existingSize + newContentSize
	if needsNewline {
		total++
	}
	if total > maxMemoryFileSize {
		return fmt.Errorf("memory file size limit exceeded: %s (%d bytes)", filepath.Base(path), existingSize)
	}
	return nil
}

// appendUniqueFacts appends facts to a file only if not already present.
// Enforces a hard size limit (maxMemoryFileSize) to prevent unbounded writes.
func appendUniqueFacts(path string, facts []string) error {
	// Pre-check: file already exceeds the limit.
	fi, err := os.Stat(path)
	if err == nil && fi.Size() > maxMemoryFileSize {
		return fmt.Errorf("memory file size limit exceeded: %s (%d bytes)", filepath.Base(path), fi.Size())
	}

	existing := readLines(path)
	existingSet := make(map[string]struct{}, len(existing))
	for _, l := range existing {
		existingSet[l] = struct{}{}
	}

	var toWrite []string
	var newSize int64
	for _, f := range facts {
		line := "- " + f
		if _, seen := existingSet[line]; !seen {
			toWrite = append(toWrite, line)
			newSize += int64(len(line) + 1) // +1 for trailing newline
		}
	}

	if len(toWrite) == 0 {
		return nil
	}

	// Determine if a leading newline separator will be needed,
	// so the budget check can include the extra byte.
	needsNL := false
	if fi != nil && fi.Size() > 0 {
		fd, fderr := os.Open(path)
		if fderr == nil {
			needsNL, fderr = needsLeadingNewline(fd)
			if closeErr := fd.Close(); closeErr != nil && fderr == nil {
				fderr = closeErr
			}
		}
		if fderr != nil {
			log.Printf("memory writer: failed to inspect trailing newline for %s: %v", filepath.Base(path), fderr)
		}
	}

	// Budget check including possible leading newline.
	if fi != nil {
		if err := calculateWriteBudget(fi.Size(), newSize, needsNL, path); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if needsNL {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	for _, line := range toWrite {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// readLines reads a file and returns non-empty trimmed lines.
func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// updateMemoryIndex ensures MEMORY.md has a "- [Title](filename.md)" entry.
// If MEMORY.md does not exist, it creates one with a header.
// Uses line-by-line scanning to avoid false matches inside fact content.
// Before reading/writing, resolves symlinks to prevent escape via existing
// MEMORY.md symlink (H-01 residual).
func updateMemoryIndex(dir, filename, title string) error {
	if title == "" {
		title = strings.TrimSuffix(filename, ".md")
	}
	indexPath := filepath.Join(dir, "MEMORY.md")

	// Resolve existing MEMORY.md symlink and verify containment.
	resolvedPath, err := filepath.EvalSymlinks(indexPath)
	if err == nil {
		// File exists — verify resolved path stays within dir
		if !isSubDirLexical(dir, resolvedPath) {
			return errPathTraversal
		}
		rel, err := filepath.Rel(dir, resolvedPath)
		if err != nil || isPersonasRelPath(rel) {
			return errPersonasPath
		}
		// Use resolved path for subsequent I/O
		indexPath = resolvedPath
	}
	// If file doesn't exist (EvalSymlinks error), indexPath stays as-is
	// and os.WriteFile will create it freshly.

	entryLine := fmt.Sprintf("- [%s](%s)", title, filename)

	data, err := os.ReadFile(indexPath)
	if err != nil {
		// Create new MEMORY.md
		header := "# Memory Index\n\n"
		return os.WriteFile(indexPath, []byte(header+entryLine+"\n"), 0600)
	}

	// Check line by line if entry already exists
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entryLine {
			return nil
		}
	}

	// Append to existing file; ensure trailing newline
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entryLine + "\n"
	return os.WriteFile(indexPath, []byte(content), 0600)
}

// isSubDirLexical checks containment via clean + relative path comparison.
// Does NOT resolve symlinks — only verifies lexical containment.
// Safe for paths that don't exist yet (used in tests).
func isSubDirLexical(parent, sub string) bool {
	parent = filepath.Clean(parent)
	sub = filepath.Clean(sub)
	if parent == sub {
		return true
	}
	rel, err := filepath.Rel(parent, sub)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}
