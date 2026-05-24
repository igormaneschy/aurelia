package memoryux

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/igormaneschy/aurelia/internal/runtime"
)

// LayerInfo describes a memory layer and its current state.
type LayerInfo struct {
	Name          string
	Scope         string
	Dir           string
	Exists        bool
	MarkdownFiles int
	LatestMod     time.Time
}

// Status represents the complete memory status response.
type Status struct {
	CWD             string
	CheckpointLayer string
	Layers          []LayerInfo
	LatestReceipt   *Receipt
}

// CheckpointResult describes what was written or updated.
type CheckpointResult struct {
	Layer     string
	Dir       string
	Path      string
	Created   bool
	UpdatedAt time.Time
}

// Service provides memory operations for command handlers.
// It computes layer directories and delegates I/O to checkpoint helpers.
type Service struct {
	MemoryDir string
	Resolver  *runtime.PathResolver
}

// New creates a new memory UX service.
func New(memoryDir string, resolver *runtime.PathResolver) *Service {
	return &Service{
		MemoryDir: memoryDir,
		Resolver:  resolver,
	}
}

func (s *Service) topicMemoryDir(chatID int64, threadID int) string {
	if threadID <= 0 {
		return ""
	}
	if s.Resolver == nil {
		return ""
	}
	return filepath.Join(s.Resolver.Root(), "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

func (s *Service) projectMemoryDir(cwd string, chatID int64, threadID int) string {
	if cwd == "" || s.Resolver == nil {
		return ""
	}
	return s.Resolver.ConversationProjectMemoryDir(cwd, chatID, threadID)
}

func (s *Service) teamMemoryDir(cwd string) string {
	if cwd == "" || s.Resolver == nil {
		return ""
	}
	return s.Resolver.ProjectTeamMemoryDir(cwd)
}

// Status returns the current memory layer status without creating anything.
func (s *Service) Status(chatID int64, threadID int, cwd string) (Status, error) {
	var layers []LayerInfo

	// Global: always present
	layers = append(layers, s.layerInfo("Global", "global", s.MemoryDir))

	// Topic: only when in a thread
	if threadID > 0 {
		layers = append(layers, s.layerInfo("Topic", "topic", s.topicMemoryDir(chatID, threadID)))
	}

	// Project-private: only when cwd is set
	if cwd != "" && s.Resolver != nil {
		dir := s.projectMemoryDir(cwd, chatID, threadID)
		layers = append(layers, s.layerInfo("Project", "project", dir))
	}

	// Team: only when cwd is set
	if cwd != "" && s.Resolver != nil {
		dir := s.teamMemoryDir(cwd)
		layers = append(layers, s.layerInfo("Team", "team", dir))
	}

	layer, _ := s.checkpointTarget(cwd, chatID, threadID)

	// Load latest receipt (non-fatal; missing/corrupt file is tolerable)
	latestReceipt, _ := LatestReceipt(s.MemoryDir)

	return Status{
		CWD:             cwd,
		CheckpointLayer: layer,
		Layers:          layers,
		LatestReceipt:   latestReceipt,
	}, nil
}

// WriteCheckpoint writes a checkpoint to the safest available layer.
func (s *Service) WriteCheckpoint(chatID int64, threadID int, cwd string, note string) (CheckpointResult, error) {
	layer, dir := s.checkpointTarget(cwd, chatID, threadID)
	if dir == "" {
		return CheckpointResult{}, fmt.Errorf("no writable memory layer available")
	}
	return writeCheckpoint(dir, layer, cwd, note, chatID, threadID)
}

// layerInfo reads directory metadata without creating anything.
func (s *Service) layerInfo(name, scope, dir string) LayerInfo {
	info := LayerInfo{Name: name, Scope: scope, Dir: dir}
	if dir == "" {
		return info
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return info
	}
	info.Exists = true
	var latestMod time.Time
	mdCount := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".md" {
			continue
		}
		mdCount++
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(latestMod) {
			latestMod = fi.ModTime()
		}
	}
	info.MarkdownFiles = mdCount
	info.LatestMod = latestMod
	return info
}

// checkpointTarget selects the safest writeable layer for checkpoint data.
// Priority: project-private > topic > global.
func (s *Service) checkpointTarget(cwd string, chatID int64, threadID int) (string, string) {
	if cwd != "" && s.Resolver != nil {
		dir := s.projectMemoryDir(cwd, chatID, threadID)
		if dir != "" {
			return "project", dir
		}
	}
	if threadID > 0 {
		dir := s.topicMemoryDir(chatID, threadID)
		return "topic", dir
	}
	return "global", s.MemoryDir
}
