package pipeline

import "errors"

var ErrDocumentProjectChanged = errors.New("document project differs from the current execution project")

// ResolveDocumentCWD uses the same authorized profile and project precedence as
// execution. For example: ResolveDocumentCWD(chatID, threadID, userID, true, caption).
func (s *Service) ResolveDocumentCWD(chatID int64, threadID int, userID int64, isPrivateChat bool, caption string) (string, error) {
	if s == nil {
		return "", errors.New("pipeline service is nil")
	}
	profile, _, err := s.resolveEffectiveProfile(caption, s.userActiveMode(userID), userID, s.isOwnerUser(userID))
	if err != nil {
		return "", err
	}
	return s.effectiveCwdForContext(profile, chatID, threadID, userID, isPrivateChat), nil
}

// ProcessDocument sends an opaque document reference as input, never as a
// cancellation/status command. For example: ProcessDocument(chatID, 0, messageID, prompt, cwd, userID, true).
func (s *Service) ProcessDocument(chatID int64, threadID, messageID int, text, cwd string, userID int64, isPrivateChat bool) error {
	if cwd == "" {
		return ErrDocumentProjectChanged
	}
	currentCWD, err := s.ResolveDocumentCWD(chatID, threadID, userID, isPrivateChat, text)
	if err != nil {
		return err
	}
	if currentCWD != cwd {
		return ErrDocumentProjectChanged
	}
	return s.process(chatID, threadID, messageID, text, nil, userID, isPrivateChat, cwd)
}

func (s *Service) documentRunTarget(key, cwd string) string {
	activeRunSlotMu.Lock()
	defer activeRunSlotMu.Unlock()
	value, ok := s.activeSessions.Load(key)
	if !ok {
		return ""
	}
	run, ok := value.(*activeRun)
	if !ok {
		return ""
	}
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.cwd != cwd || run.superseded || run.finalized {
		return ""
	}
	return run.requestID
}
