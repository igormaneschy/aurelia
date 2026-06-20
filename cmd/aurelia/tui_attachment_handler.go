package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// debugAttach controls per-attachment diagnostic logging.
// Must be set before starting the daemon; changes after start
// require a restart to take effect.
var debugAttach = os.Getenv("AURELIA_ATTACH_DEBUG") == "1"

// copiedAttachment records the result of copying one attachment file.
type copiedAttachment struct {
	// OriginalName is the user-visible name (from Name field or path base).
	OriginalName string
	// FinalName is the actual filename on disk (may differ due to dedup).
	FinalName string
	// Size is the number of bytes copied.
	Size int64
}

// copyAttachmentsToCWD copies document attachments into <cwd>/uploads/.
// Each attachment is validated against symlinks, path traversal, and size
// limits (MaxAttachmentBytes). On any failure, the function rolls back any
// files already copied and returns the error.
func copyAttachmentsToCWD(ctx context.Context, cwd string, attachments []ipc.IPCAttachment) ([]copiedAttachment, error) {
	uploadsDir := filepath.Join(cwd, "uploads")

	if err := os.MkdirAll(uploadsDir, 0o750); err != nil {
		return nil, fmt.Errorf("create uploads dir: %w", err)
	}

	var totalSize int64
	var copied []copiedAttachment
	var created []string // tracks destination paths for rollback on error

	for _, att := range attachments {
		// Determine destination name: prefer Name field, fall back to basename.
		destName := att.Name
		if destName == "" {
			destName = filepath.Base(att.Path)
		}

		// Defense against path traversal: filepath.Base("..") returns "..".
		if destName == "." || destName == ".." {
			rollbackCreated(created)
			return nil, fmt.Errorf("attachment %s: invalid name", filepath.Base(att.Path))
		}

		// Pre-copy stat — detect TOCTOU races where the source file is
		// deleted or moved between /attach and send, and accumulate size.
		// This is an additional defense before copyFileNoFollow's own
		// fstat-on-open, providing earlier, clearer error messages for
		// common failure modes (deleted file, oversized, non-regular).
		fi, err := os.Lstat(att.Path)
		if err != nil {
			rollbackCreated(created)
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("attachment %q: no longer available (was it deleted or moved between /attach and send?)", filepath.Base(att.Path))
			}
			return nil, fmt.Errorf("attachment %q: cannot stat source: %w", filepath.Base(att.Path), err)
		}
		if !fi.Mode().IsRegular() {
			// Let copyFileNoFollow produce its own error for non-regular
			// files (symlinks, dirs) — Lstat succeeds but the file may be
			// replaced before open. This check catches the obvious case
			// early with a clear user-facing message.
			rollbackCreated(created)
			return nil, fmt.Errorf("attachment %q: source is not a regular file: %s", filepath.Base(att.Path), att.Path)
		}

		totalSize += fi.Size()
		if totalSize > ipc.MaxTotalAttachmentBytes {
			rollbackCreated(created)
			return nil, fmt.Errorf("total attachment size %d bytes exceeds %d byte limit",
				totalSize, ipc.MaxTotalAttachmentBytes)
		}

		dst, err := uniqueUploadPath(uploadsDir, destName)
		if err != nil {
			rollbackCreated(created)
			return nil, fmt.Errorf("attachment %s: %w", filepath.Base(att.Path), err)
		}

		if debugAttach {
			log.Printf("tui: attach debug: copying attachment path=%q cwd=%q name=%q", att.Path, cwd, destName)
		}

		n, err := copyFileNoFollow(ctx, att.Path, dst, ipc.MaxAttachmentBytes)
		if err != nil {
			rollbackCreated(created)
			return nil, fmt.Errorf("attachment %s: %w", filepath.Base(att.Path), err)
		}

		created = append(created, dst)
		copied = append(copied, copiedAttachment{
			OriginalName: destName,
			FinalName:    filepath.Base(dst),
			Size:         n,
		})
	}

	return copied, nil
}

// rollbackCreated removes all tracked destination files, logging any errors.
// Used to clean up partial copies when an attachment in the batch fails.
func rollbackCreated(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("tui: attach: rollback remove %s: %v", filepath.Base(p), err)
		}
	}
}

// uniqueUploadPath returns a non-existent path under dir for the given name.
// If the name already exists, it appends _1, _2, ... before the extension
// (e.g. doc_1.md, doc_2.md). After 1000 conflicts it returns an error.
// The name is sanitized via filepath.Base first, and path traversal
// (. / ..) is explicitly rejected.
func uniqueUploadPath(dir, name string) (string, error) {
	base := filepath.Base(name)
	if base == "." || base == ".." {
		return "", fmt.Errorf("invalid name: %s", base)
	}

	candidate := filepath.Join(dir, base)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil {
		return "", errNoPath("stat", base, err)
	}

	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i <= 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", errNoPath("stat", fmt.Sprintf("%s_%d%s", stem, i, ext), err)
		}
	}

	return "", fmt.Errorf("could not find unique name for %s", name)
}

// errNoPath wraps an OS error with a descriptive prefix while stripping the
// full path from the OS error message. Only the basename is retained in
// the output, preventing path leakage in logs and user-facing messages.
func errNoPath(prefix, name string, err error) error {
	s := err.Error()
	if idx := strings.LastIndex(s, ": "); idx >= 0 {
		return fmt.Errorf("%s %s: %s", prefix, name, s[idx+2:])
	}
	return fmt.Errorf("%s %s: %v", prefix, name, err)
}

// copyFileNoFollow copies a regular file from src to dst, refusing symlinks
// on both sides. The source is opened with O_RDONLY|O_NOFOLLOW and verified to
// be a regular file via fstat. The destination is created with O_WRONLY|
// O_CREATE|O_EXCL|O_NOFOLLOW and mode 0o640. A TOCTOU-resistant size check
// is performed: the file is stat'd before copy, and a LimitReader rejects
// files larger than maxBytes. The context can cancel long copies: on ctx
// cancellation the destination file is removed and ctx.Err() is returned.
func copyFileNoFollow(ctx context.Context, src, dst string, maxBytes int64) (int64, error) {
	// Open source with O_NOFOLLOW — rejects symlinks at open time.
	srcFd, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, errNoPath("open source", filepath.Base(src), err)
	}
	defer func() {
		if err := srcFd.Close(); err != nil {
			log.Printf("tui: attach: error closing source %s: %v", filepath.Base(src), err)
		}
	}()

	// Verify it is a regular file via fstat on the opened fd.
	fi, err := srcFd.Stat()
	if err != nil {
		return 0, errNoPath("stat source", filepath.Base(src), err)
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("source is not a regular file: %s", filepath.Base(src))
	}
	if fi.Size() > maxBytes {
		return 0, fmt.Errorf("file %s exceeds max size (%d > %d bytes)",
			filepath.Base(src), fi.Size(), maxBytes)
	}

	// Open destination with O_EXCL|O_NOFOLLOW — refuses to overwrite
	// existing files and rejects symlink targets.
	dstFd, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return 0, errNoPath("open destination", filepath.Base(dst), err)
	}
	defer func() {
		if err := dstFd.Close(); err != nil {
			log.Printf("tui: attach: error closing destination %s: %v", filepath.Base(dst), err)
		}
	}()

	// Check for pre-cancelled context before starting the copy goroutine.
	// Without this check, a cancelled context races with io.Copy on fast
	// filesystems (both ctx.Done() and ch may be ready simultaneously).
	if err := ctx.Err(); err != nil {
		_ = os.Remove(dst)
		return 0, err
	}

	// Copy with LimitReader(maxBytes+1) to detect size changes (TOCTOU).
	// Run the copy in a goroutine so we can select on ctx.Done().
	type copyResult struct {
		n   int64
		err error
	}
	ch := make(chan copyResult, 1)
	go func() {
		n, err := io.Copy(dstFd, io.LimitReader(srcFd, maxBytes+1))
		ch <- copyResult{n, err}
	}()

	var written int64
	select {
	case res := <-ch:
		written, err = res.n, res.err
	case <-ctx.Done():
		// Close fds to unblock the goroutine's Copy immediately.
		_ = srcFd.Close()
		_ = dstFd.Close()
		_ = os.Remove(dst)
		<-ch // drain channel — wait for goroutine to finish before returning
		return 0, ctx.Err()
	}
	if err != nil {
		// Best-effort cleanup of partial file.
		if rmErr := os.Remove(dst); rmErr != nil {
			log.Printf("tui: attach: cleanup remove of partial %s: %v", filepath.Base(dst), rmErr)
		}
		return 0, fmt.Errorf("copy %s: %w", filepath.Base(src), err)
	}

	if written > maxBytes {
		if rmErr := os.Remove(dst); rmErr != nil {
			log.Printf("tui: attach: cleanup remove of oversized %s: %v", filepath.Base(dst), rmErr)
		}
		return 0, fmt.Errorf("file %s exceeds max size after copy", filepath.Base(src))
	}

	return written, nil
}

// buildAttachmentNote returns a markdown-formatted note describing the copied
// attachments. Returns "" when copied is empty.
// The note is appended to the user's message text so the agent can reference
// files by their uploads/ name.
func buildAttachmentNote(copied []copiedAttachment) string {
	if len(copied) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n[Attached files copied to ./uploads/]\n")
	for _, c := range copied {
		b.WriteString("- ")
		b.WriteString(c.FinalName)
		b.WriteString(" (")
		b.WriteString(humanBytes(c.Size))
		b.WriteString(")\n")
	}
	return b.String()
}

// humanBytes formats a byte count as a human-readable string (e.g. "12 B",
// "1.5 KB", "2.3 MB").
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
