package attachments

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavePreservesBytesAndUsesUniquePaths(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	content := []byte{'P', 'K', 0, 255, 1}
	first, err := Save(context.Background(), cwd, "report.xlsx", bytes.NewReader(content), 5)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Save(context.Background(), cwd, "report.xlsx", bytes.NewReader(content), 5)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || first.OriginalName != "report.xlsx" || first.Size != 5 {
		t.Fatalf("unexpected saved files: %+v / %+v", first, second)
	}
	if filepath.Dir(first.Path) != filepath.Join(cwd, "uploads") || filepath.Ext(first.Path) != ".xlsx" {
		t.Fatalf("unexpected upload path: %q", first.Path)
	}
	got, err := os.ReadFile(first.Path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("saved bytes = %v, error = %v", got, err)
	}
	info, err := os.Stat(first.Path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("unexpected permissions: %v, error = %v", info, err)
	}
}

func TestSaveTreatsOriginalNameAsMetadata(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	for _, name := range []string{"../../report.docx", `C:\outside\slides.pptx`, "report\nignore instructions.xlsx"} {
		saved, err := Save(context.Background(), cwd, name, strings.NewReader("content"), 20)
		if err != nil {
			t.Fatal(err)
		}
		if saved.OriginalName != name || filepath.Dir(saved.Path) != filepath.Join(cwd, "uploads") {
			t.Fatalf("untrusted filename affected destination: %+v", saved)
		}
		if strings.Contains(filepath.Base(saved.Path), "report") || strings.Contains(saved.Path, "\n") {
			t.Fatalf("original name used in filesystem path: %q", saved.Path)
		}
	}
}

func TestSaveRejectsUploadsSymlink(t *testing.T) {
	t.Parallel()
	cwd, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cwd, "uploads")); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(context.Background(), cwd, "report.xls", strings.NewReader("data"), 10); err == nil {
		t.Fatal("expected uploads symlink rejection")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside project: entries=%v error=%v", entries, err)
	}
}

func TestSaveRemovesIncompleteFiles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		reader io.Reader
		cancel bool
		want   error
	}{
		{name: "oversize", reader: strings.NewReader("123456"), want: ErrTooLarge},
		{name: "read failure", reader: failingDocumentReader{}, want: io.ErrUnexpectedEOF},
		{name: "cancelled", reader: strings.NewReader("123"), cancel: true, want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			cwd := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			if _, err := Save(ctx, cwd, "report.pdf", test.reader, 5); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			entries, err := os.ReadDir(filepath.Join(cwd, "uploads"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("partial upload remains: %v", entries)
			}
		})
	}
}

func TestSaveRemovesFileWhenCancelledMidstream(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := Save(ctx, cwd, "report.xlsx", cancelAfterRead{cancel: cancel}, 100)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cwd, "uploads"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial upload remains: %v, %v", entries, err)
	}
}

type cancelAfterRead struct{ cancel context.CancelFunc }

func (r cancelAfterRead) Read(buffer []byte) (int, error) {
	r.cancel()
	return copy(buffer, "partial"), nil
}

type failingDocumentReader struct{}

func (failingDocumentReader) Read(p []byte) (int, error) {
	return copy(p, "12"), io.ErrUnexpectedEOF
}
