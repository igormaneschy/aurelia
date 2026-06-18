package main

import (
	"fmt"
	"os"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/pkg/images"
)

// convertIPCImages converts IPCImage slice to bridge.ImageAttachment slice.
// For images with Path set, it reads the file and base64-encodes it.
// For images with Data set (base64), it uses the data directly.
func convertIPCImages(ipcImages []ipc.IPCImage, maxBytes int) ([]bridge.ImageAttachment, error) {
	if len(ipcImages) == 0 {
		return nil, nil
	}

	var attachments []bridge.ImageAttachment
	for i, img := range ipcImages {
		if img.Path != "" {
			// Read file and encode.
			att, err := images.Encode(img.Path, img.MediaType, maxBytes)
			if err != nil {
				return nil, fmt.Errorf("image[%d]: %w", i, err)
			}
			attachments = append(attachments, att)
		} else if img.Data != "" {
			// Use base64 data directly.
			attachments = append(attachments, bridge.ImageAttachment{
				Data:      img.Data,
				MediaType: img.MediaType,
			})
		}
	}
	return attachments, nil
}

// validateImageFiles checks that all image paths exist and are readable.
func validateImageFiles(ipcImages []ipc.IPCImage) error {
	for i, img := range ipcImages {
		if img.Path != "" {
			if _, err := os.Stat(img.Path); err != nil {
				return fmt.Errorf("image[%d]: %w", i, err)
			}
		}
	}
	return nil
}
