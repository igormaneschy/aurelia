package planning

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// Observer watches bridge events and extracts artifacts from tool calls.
type Observer struct {
	cwd string
}

// NewObserver creates an observer scoped to a working directory.
func NewObserver(cwd string) *Observer {
	return &Observer{cwd: filepath.Clean(cwd)}
}

// ObserveEvent processes a single bridge event and returns any artifacts found.
// Returns nil if the event is not a relevant tool_use or has no parseable input.
func (o *Observer) ObserveEvent(event bridge.Event) []Artifact {
	if event.Type != "tool_use" {
		return nil
	}
	switch event.Name {
	case "Write", "Edit":
		return o.observeSingle(event)
	case "MultiEdit":
		return o.observeMulti(event)
	default:
		return nil
	}
}

func (o *Observer) observeSingle(event bridge.Event) []Artifact {
	input, ok := event.Input.(map[string]interface{})
	if !ok || input == nil {
		log.Printf("observer: unparseable input for %s event", event.Name)
		return nil
	}
	rawPath, err := extractPath(input)
	if err != nil {
		log.Printf("observer: %s: %v", event.Name, err)
		return nil
	}
	return []Artifact{o.newArtifact(rawPath, event.Name)}
}

func (o *Observer) observeMulti(event bridge.Event) []Artifact {
	input, ok := event.Input.(map[string]interface{})
	if !ok || input == nil {
		log.Printf("observer: unparseable input for MultiEdit event")
		return nil
	}
	// Single-file MultiEdit with direct path field
	if rawPath, err := extractPath(input); err == nil {
		return []Artifact{o.newArtifact(rawPath, event.Name)}
	}
	// Multi-file MultiEdit with edits array
	rawEdits, ok := input["edits"]
	if !ok {
		log.Printf("observer: no path or edits in MultiEdit input")
		return nil
	}
	edits, ok := rawEdits.([]interface{})
	if !ok {
		log.Printf("observer: edits is not an array in MultiEdit input")
		return nil
	}
	var artifacts []Artifact
	for i, raw := range edits {
		edit, ok := raw.(map[string]interface{})
		if !ok {
			log.Printf("observer: MultiEdit edit[%d] is not a map", i)
			continue
		}
		rawPath, err := extractPath(edit)
		if err != nil {
			log.Printf("observer: MultiEdit edit[%d]: %v", i, err)
			continue
		}
		artifacts = append(artifacts, o.newArtifact(rawPath, event.Name))
	}
	return artifacts
}

// newArtifact creates a single artifact from a raw path and tool name.
func (o *Observer) newArtifact(rawPath, tool string) Artifact {
	resolved := o.resolve(rawPath)
	return Artifact{
		Path:      resolved,
		Tool:      tool,
		InsideCWD: o.insideCWD(resolved),
		CreatedAt: time.Now(),
	}
}

// extractPath extracts "path" or "file_path" from a map.
func extractPath(m map[string]interface{}) (string, error) {
	if raw, ok := m["path"].(string); ok && raw != "" {
		return raw, nil
	}
	if raw, ok := m["file_path"].(string); ok && raw != "" {
		return raw, nil
	}
	return "", fmt.Errorf("no path or file_path in input")
}

// resolve converts a raw path to an absolute path relative to cwd.
func (o *Observer) resolve(raw string) string {
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(o.cwd, raw)
}

// insideCWD checks whether resolved path is inside the observer's cwd.
func (o *Observer) insideCWD(resolved string) bool {
	rel, err := filepath.Rel(o.cwd, resolved)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// ReconcileArtifacts marks artifacts as Confirmed by checking os.Stat.
// Returns a new slice; original artifacts are not modified.
func ReconcileArtifacts(artifacts []Artifact) []Artifact {
	result := make([]Artifact, len(artifacts))
	for i, a := range artifacts {
		_, err := os.Stat(a.Path)
		a.Confirmed = err == nil
		result[i] = a
	}
	return result
}
