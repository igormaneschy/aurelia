// Package attachments persists opaque documents for the agent to inspect with its tools.
package attachments

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrTooLarge = errors.New("document exceeds attachment size limit")

// File identifies the original document without embedding its contents in a prompt.
type File struct {
	OriginalName string
	Path         string
	Size         int64
}

// Save keeps original bytes under cwd/uploads, using a generated name to avoid
// traversal and collisions. It never converts documents or deletes successful uploads.
// For example: Save(ctx, cwd, "report.xlsx", response.Body, 25*1024*1024).
func Save(ctx context.Context, cwd, originalName string, source io.Reader, maxBytes int64) (saved File, err error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if !filepath.IsAbs(cwd) || maxBytes <= 0 {
		return File{}, fmt.Errorf("attachment requires an absolute project path and positive byte limit")
	}
	project, err := os.OpenRoot(cwd)
	if err != nil {
		return File{}, fmt.Errorf("open attachment project: %w", err)
	}
	defer func() { _ = project.Close() }()
	uploads, err := openUploads(project)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = uploads.Close() }()
	name := "document-" + rand.Text() + documentExtension(originalName)
	destination, err := uploads.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return File{}, fmt.Errorf("create attachment: %w", err)
	}
	defer func() {
		err = errors.Join(err, destination.Close())
		if err != nil {
			err = errors.Join(err, uploads.Remove(name))
		}
	}()
	size, err := io.Copy(destination, io.LimitReader(contextReader{ctx, source}, maxBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("save attachment: %w", err)
	}
	if size > maxBytes {
		return File{}, fmt.Errorf("%w: maximum %d bytes", ErrTooLarge, maxBytes)
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	return File{OriginalName: originalName, Path: filepath.Join(cwd, "uploads", name), Size: size}, nil
}

func openUploads(project *os.Root) (*os.Root, error) {
	if err := project.Mkdir("uploads", 0o750); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create uploads directory: %w", err)
	}
	info, err := project.Lstat("uploads")
	if err != nil {
		return nil, fmt.Errorf("inspect uploads directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("uploads must be a directory, not a symlink")
	}
	// Root operations keep even a concurrently replaced symlink inside the project.
	uploads, err := project.OpenRoot("uploads")
	if err != nil {
		return nil, fmt.Errorf("open uploads directory: %w", err)
	}
	return uploads, nil
}

func documentExtension(name string) string {
	extension := strings.ToLower(filepath.Ext(name))
	if len(extension) > 17 {
		return ""
	}
	for _, character := range strings.TrimPrefix(extension, ".") {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return ""
		}
	}
	return extension
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(buffer)
}
