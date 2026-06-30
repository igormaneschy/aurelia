package dream

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// consolidationAction describes one consolidation operation.
type consolidationAction struct {
	// MergeFiles: combine source files into a new/updated target file
	MergeFiles *mergeOp `json:"merge_files,omitempty"`
	// UpdateFile: append facts to an existing file
	UpdateFile *updateFileOp `json:"update_file,omitempty"`
	// DeleteFile: remove an obsolete memory file
	DeleteFile string `json:"delete_file,omitempty"`
	// IndexEntry: add/update an entry in MEMORY.md
	IndexEntry *indexEntryOp `json:"index_entry,omitempty"`
}

type mergeOp struct {
	SourceFiles []string `json:"source_files"`
	IntoFile    string   `json:"into_file"`
	Title       string   `json:"title,omitempty"`
	Facts       []string `json:"facts"`
}

type updateFileOp struct {
	Filename string   `json:"filename"`
	Title    string   `json:"title,omitempty"`
	Facts    []string `json:"facts"`
}

type indexEntryOp struct {
	Filename string `json:"filename"`
	Title    string `json:"title"`
	Remove   bool   `json:"remove,omitempty"`
}

type consolidationExtraction struct {
	Actions []consolidationAction `json:"actions"`
}

const (
	maxConsolidationActions = 5
	maxMergeSources         = 5
)

// parseConsolidationJSON parses model JSON output for consolidation actions.
// Uses extractJSONObject for tolerant extraction.
// Returns nil if parsing fails or the result is empty.
func parseConsolidationJSON(raw string) *consolidationExtraction {
	ext, _ := parseConsolidationJSONWithError(raw)
	return ext
}

// parseConsolidationJSONWithError is like parseConsolidationJSON but also
// returns the underlying parse/extraction error for diagnostics.
func parseConsolidationJSONWithError(raw string) (*consolidationExtraction, error) {
	jsonStr, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	var ext consolidationExtraction
	if err := json.Unmarshal([]byte(jsonStr), &ext); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	// Cap actions
	if len(ext.Actions) > maxConsolidationActions {
		log.Printf("[dream] capping %d consolidation actions to %d", len(ext.Actions), maxConsolidationActions)
		ext.Actions = ext.Actions[:maxConsolidationActions]
	}

	if len(ext.Actions) == 0 {
		return nil, nil
	}
	return &ext, nil
}

// applyConsolidationActions applies consolidation actions through the safe memory writer.
// Returns count of successfully applied actions.
func applyConsolidationActions(w *safeMemoryWriter, actions []consolidationAction, chatID int64, threadID int, cwd string) int {
	applied := 0
	for _, a := range actions {
		if err := applyOneConsolidation(w, a, chatID, threadID, cwd); err != nil {
			log.Printf("[dream] rejected consolidation action: %v", err)
		} else {
			applied++
		}
	}
	return applied
}

func applyOneConsolidation(w *safeMemoryWriter, a consolidationAction, chatID int64, threadID int, cwd string) error {
	switch {
	case a.MergeFiles != nil:
		return applyMerge(w, a.MergeFiles, chatID, threadID, cwd)
	case a.UpdateFile != nil:
		return applyConsolidationUpdate(w, a.UpdateFile, chatID, threadID, cwd)
	case a.DeleteFile != "":
		return applyDelete(w, a.DeleteFile, chatID, threadID, cwd)
	case a.IndexEntry != nil:
		return applyIndexEntry(w, a.IndexEntry, chatID, threadID, cwd)
	default:
		return fmt.Errorf("empty consolidation action")
	}
}

func applyMerge(w *safeMemoryWriter, op *mergeOp, chatID int64, threadID int, cwd string) error {
	if len(op.SourceFiles) > maxMergeSources {
		op.SourceFiles = op.SourceFiles[:maxMergeSources]
	}
	// Validate into_file
	if err := validateFilename(op.IntoFile); err != nil {
		return fmt.Errorf("merge target: %w", err)
	}
	// Use resolvedDir for all operations (handles macOS /var → /private/var symlinks).
	// This is consistent with applyOne for global layer: resolveLayerTarget("global")
	// returns w.memoryDir which resolves to the same path after EvalSymlinks in step 7.
	// Here we use the pre-resolved path directly, which is equivalent.
	base := w.resolvedDir
	target := filepath.Join(base, op.IntoFile)
	lt := layerTarget{base: base, root: base, blocksPersonas: true}

	// Verify containment
	if !isSubDirLexical(base, target) {
		return errPathTraversal
	}

	// Create directory if needed
	if err := os.MkdirAll(base, 0700); err != nil {
		return fmt.Errorf("create merge dir: %w", err)
	}

	// Sanitize and write merged facts
	var mergedFacts []string
	for _, f := range op.Facts {
		s := sanitizeFact(f)
		if s != "" {
			mergedFacts = append(mergedFacts, s)
		}
	}
	if len(mergedFacts) > maxFactsPerFile {
		mergedFacts = mergedFacts[:maxFactsPerFile]
	}
	mergedFacts = dedupeStrings(mergedFacts)

	// Check target file symlink before writing (H-01 residual)
	if err := w.checkTargetSymlink(lt, base, target); err != nil {
		return fmt.Errorf("merge target symlink: %w", err)
	}
	if err := appendUniqueFacts(target, mergedFacts); err != nil {
		return fmt.Errorf("merge write: %w", err)
	}
	// updateMemoryIndex has its own symlink resolution for MEMORY.md
	if err := updateMemoryIndex(base, op.IntoFile, sanitizeTitle(op.Title)); err != nil {
		return fmt.Errorf("merge index: %w", err)
	}

	// Remove source files
	for _, src := range op.SourceFiles {
		if err := validateFilename(src); err != nil {
			continue
		}
		srcPath := filepath.Join(base, src)
		if isSubDirLexical(base, srcPath) {
			_ = os.Remove(srcPath)
		}
	}
	return nil
}

func applyConsolidationUpdate(w *safeMemoryWriter, op *updateFileOp, chatID int64, threadID int, cwd string) error {
	// Reuse the nudge update contract through the writer
	up := memoryUpdate{
		Layer:    "global",
		Filename: op.Filename,
		Title:    op.Title,
		Facts:    op.Facts,
	}
	_, err := w.applyOne(up, chatID, threadID, cwd)
	return err
}

func applyDelete(w *safeMemoryWriter, filename string, chatID int64, threadID int, cwd string) error {
	if err := validateFilename(filename); err != nil {
		return fmt.Errorf("delete filename: %w", err)
	}
	target := filepath.Join(w.resolvedDir, filename)
	if !isSubDirLexical(w.resolvedDir, target) {
		return errPathTraversal
	}
	// Resolve and check for symlink escape before deletion (consistency with applyOne).
	lt := layerTarget{base: w.resolvedDir, root: w.resolvedDir, blocksPersonas: true}
	if err := w.checkTargetSymlink(lt, w.resolvedDir, target); err != nil {
		return fmt.Errorf("delete symlink: %w", err)
	}
	if err := os.Remove(target); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

func applyIndexEntry(w *safeMemoryWriter, op *indexEntryOp, chatID int64, threadID int, cwd string) error {
	if err := validateFilename(op.Filename); err != nil {
		return fmt.Errorf("index filename: %w", err)
	}
	if op.Remove {
		return removeMemoryIndexEntry(w.resolvedDir, op.Filename)
	}
	return updateMemoryIndex(w.resolvedDir, op.Filename, sanitizeTitle(op.Title))
}

// removeMemoryIndexEntry removes a file entry from MEMORY.md.
// Resolves symlinks before reading/writing to prevent escape via
// existing MEMORY.md symlink (H-01 residual).
func removeMemoryIndexEntry(dir, filename string) error {
	indexPath := filepath.Join(dir, "MEMORY.md")
	// Resolve existing symlink and verify containment
	resolvedPath, err := filepath.EvalSymlinks(indexPath)
	if err == nil {
		if !isSubDirLexical(dir, resolvedPath) {
			return errPathTraversal
		}
		rel, err := filepath.Rel(dir, resolvedPath)
		if err != nil || isPersonasRelPath(rel) {
			return errPersonasPath
		}
		indexPath = resolvedPath
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil // file doesn't exist, nothing to remove
	}
	lines := strings.Split(string(data), "\n")
	var filtered []string
	for _, line := range lines {
		if strings.Contains(line, "("+filename+")") {
			continue
		}
		filtered = append(filtered, line)
	}
	return os.WriteFile(indexPath, []byte(strings.Join(filtered, "\n")), 0600)
}
