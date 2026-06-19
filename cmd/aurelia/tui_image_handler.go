package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/pkg/images"
)

// aggregateIPCImageSize returns the total byte size of all path-based and
// inline images in the slice, reading file sizes via Lstat (not full reads).
// Paths that are symlinks or non-regular files are reported as errors;
// the caller must not proceed with them.
func aggregateIPCImageSize(ipcImages []ipc.IPCImage) (int, error) {
	var total int
	for _, img := range ipcImages {
		if img.Path != "" {
			fi, err := os.Lstat(img.Path)
			if err != nil {
				return 0, fmt.Errorf("image %s: %w", img.Path, err)
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				return 0, fmt.Errorf("image %s: %w", img.Path, images.ErrSymlinkRejected)
			}
			if !fi.Mode().IsRegular() {
				return 0, fmt.Errorf("image %s: not a regular file", img.Path)
			}
			total += int(fi.Size())
		}
		// Inline Data is already in memory — count its raw bytes.
		// (The base64 data size is a loose proxy for decoded image size,
		// but we use raw string length to be consistent with IPC validation.)
		total += len(img.Data)
	}
	return total, nil
}

// convertIPCImages converts IPCImage slice to bridge.ImageAttachment slice.
// For images with Path set, it validates the file (symlink check, content
// detection, size limit) using TOCTOU-safe open, then reads and base64-encodes.
// For images with Data set (base64), it uses the data directly.
// Aggregate size is checked before any file is fully read.
func convertIPCImages(ipcImages []ipc.IPCImage, maxBytes int) ([]bridge.ImageAttachment, error) {
	if len(ipcImages) == 0 {
		return nil, nil
	}

	if maxBytes <= 0 {
		maxBytes = images.DefaultMaxBytes
	}

	// Pre-check aggregate size for all path images before any full read.
	aggSize, err := aggregateIPCImageSize(ipcImages)
	if err != nil {
		return nil, fmt.Errorf("image check: %w", err)
	}
	if aggSize > ipc.MaxTotalImageBytes {
		return nil, fmt.Errorf("total image size %d bytes exceeds %d byte limit",
			aggSize, ipc.MaxTotalImageBytes)
	}

	var attachments []bridge.ImageAttachment
	for i, img := range ipcImages {
		if img.Path != "" {
			// Use ReadImageSafely: opens with O_NOFOLLOW, validates
			// structure via image decode, and reads content from the
			// same fd — eliminating TOCTOU races between validation
			// and read. The client-supplied MediaType is ignored;
			// MIME is detected from actual content.
			data, mimeType, err := images.ReadImageSafely(img.Path, maxBytes)
			if err != nil {
				return nil, fmt.Errorf("image[%d]: %w", i, err)
			}
			encoded := base64.StdEncoding.EncodeToString(data)
			attachments = append(attachments, bridge.ImageAttachment{
				Path:      img.Path,
				Data:      encoded,
				MediaType: mimeType,
			})
		} else if img.Data != "" {
			attachments = append(attachments, bridge.ImageAttachment{
				Data:      img.Data,
				MediaType: img.MediaType,
			})
		}
	}
	return attachments, nil
}
