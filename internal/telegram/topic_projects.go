package telegram

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"gopkg.in/telebot.v3"
)

const (
	maxTopicProjectSlugLength = 40
	topicProjectPrefix        = "aurelia-"
	topicProjectMarkerName    = ".ai-memory.toml"
)

var (
	topicProjectSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	memoryWorkspacePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

const topicProjectCaptureIgnorePaths = `
[capture]
ignore_paths = [
  "~/.ssh/**",
  "~/.config/**",
  "~/.local/**",
  "~/.pi/**",
  "~/.cache/**",
  "~/.npm/**",
  "~/.docker/**",
  "~/.aurelia/**",
  "~/.agents/**",
  "~/DevCodes/**",
  "~/FotosBkp/**",
  "~/Raws/**",
  "~/#recycle/**",
  "~/lost+found/**",
]
`

// normalizeTopicSlug turns a human topic name into a safe project slug.
// It accepts common Portuguese accents, spaces and underscores but never
// path separators or traversal sequences.
func normalizeTopicSlug(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", fmt.Errorf("uso: /topic init <slug>")
	}
	value = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c",
	).Replace(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-")
	if len(value) > maxTopicProjectSlugLength {
		value = strings.TrimRight(value[:maxTopicProjectSlugLength], "-")
	}
	if value == "" || !topicProjectSlugPattern.MatchString(value) {
		return "", fmt.Errorf("slug inválido; use letras a-z, números e hífen (ex: finanças -> financas)")
	}
	return value, nil
}

func topicProjectName(slug string) string {
	return topicProjectPrefix + slug
}

// readWorkspaceFromMarker reads workspace = "..." from an ai-memory marker.
func readWorkspaceFromMarker(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "workspace") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != "workspace" {
			continue
		}
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if !memoryWorkspacePattern.MatchString(value) {
			return "", fmt.Errorf("workspace inválido no marker %s", path)
		}
		return value, nil
	}
	return "", nil
}

func writeTopicMarker(dir, workspace, project string) error {
	marker := filepath.Join(dir, topicProjectMarkerName)
	content := fmt.Sprintf("workspace = %q\nproject = %q\n%s", workspace, project, topicProjectCaptureIgnorePaths)
	tmp, err := os.CreateTemp(dir, ".ai-memory.toml.tmp-*")
	if err != nil {
		return fmt.Errorf("criar marker temporário: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("ajustar permissões do marker: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("escrever marker: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fechar marker: %w", err)
	}
	if err := os.Rename(tmpName, marker); err != nil {
		return fmt.Errorf("publicar marker: %w", err)
	}
	return nil
}

// provisionTopicProject creates base/topics/<slug> and its ai-memory marker.
// An existing directory is only accepted when its marker names the same
// workspace/project; the operation is idempotent.
func provisionTopicProject(base, slug, workspace string) (string, string, error) {
	if !filepath.IsAbs(base) {
		return "", "", fmt.Errorf("workspace base must be absolute: %q", base)
	}
	if !topicProjectSlugPattern.MatchString(slug) || len(slug) > maxTopicProjectSlugLength {
		return "", "", fmt.Errorf("slug inválido: %q", slug)
	}
	if !memoryWorkspacePattern.MatchString(workspace) {
		return "", "", fmt.Errorf("workspace inválido: %q", workspace)
	}
	project := topicProjectName(slug)
	dir := filepath.Join(filepath.Clean(base), "topics", slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", fmt.Errorf("criar diretório do tópico: %w", err)
	}
	marker := filepath.Join(dir, topicProjectMarkerName)
	if _, err := os.Stat(marker); err == nil {
		existingWorkspace, err := readWorkspaceFromMarker(marker)
		if err != nil {
			return "", "", err
		}
		existingProject, err := readProjectFromMarker(marker)
		if err != nil {
			return "", "", err
		}
		if existingWorkspace != workspace || existingProject != project {
			return "", "", fmt.Errorf("tópico já aponta para outro projeto (%s/%s)", existingWorkspace, existingProject)
		}
		return dir, project, nil
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("verificar marker do tópico: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("ler diretório do tópico: %w", err)
	}
	if len(entries) > 0 {
		return "", "", fmt.Errorf("diretório já existe sem marker; escolha outro slug")
	}
	if err := writeTopicMarker(dir, workspace, project); err != nil {
		return "", "", err
	}
	return dir, project, nil
}

func readProjectFromMarker(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "project") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != "project" {
			continue
		}
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if !topicProjectSlugPattern.MatchString(value) {
			return "", fmt.Errorf("project inválido no marker %s", path)
		}
		return value, nil
	}
	return "", nil
}

func (bc *BotController) topicWorkspace(chatID int64) (string, error) {
	if bc.bindings != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		binding, err := bc.bindings.Get(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: 0})
		if err != nil {
			return "", err
		}
		if binding != nil && binding.CWD != "" {
			workspace, err := readWorkspaceFromMarker(filepath.Join(binding.CWD, topicProjectMarkerName))
			if err != nil {
				return "", err
			}
			if workspace != "" {
				return workspace, nil
			}
		}
	}
	return "personal", nil
}

func (bc *BotController) handleTopicCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	userID := safeSenderID(c.Sender())
	args := strings.TrimSpace(c.Message().Payload)

	if c.Chat().Type == telebot.ChatPrivate {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Use `/topic init <slug>` dentro de um tópico de grupo.", threadID)
	}
	if threadID <= 0 {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Este comando deve ser usado dentro de um tópico, não no General.", threadID)
	}
	fields := strings.Fields(args)
	if len(fields) != 2 || strings.ToLower(fields[0]) != "init" {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Uso: `/topic init <slug>` (ex: `/topic init financas`).", threadID)
	}
	slug, err := normalizeTopicSlug(fields[1])
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ "+err.Error(), threadID)
	}
	if userID == 0 {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Não foi possível identificar o usuário.", threadID)
	}
	if bc.userStore == nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Perfil de usuário indisponível.", threadID)
	}
	profile, err := bc.userStore.Get(userID)
	if err != nil {
		log.Printf("topic init: load profile user=%d: %v", userID, err)
		return SendErrorWithThread(bc.bot, c.Chat(), "Não foi possível carregar seu perfil.", threadID)
	}
	if profile == nil || strings.TrimSpace(profile.DefaultCWD) == "" {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Configure seu `default_cwd` antes de usar `/topic init`.", threadID)
	}
	base, err := runtime.ResolveProjectCwd(profile.DefaultCWD)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ default_cwd inválido: "+err.Error(), threadID)
	}
	workspace, err := bc.topicWorkspace(chatID)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Marker do grupo inválido: "+err.Error(), threadID)
	}
	cwd, project, err := provisionTopicProject(base, slug, workspace)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ "+err.Error(), threadID)
	}
	oldCwd := bc.currentCwdForContext(chatID, threadID, userID, c.Chat().Type == telebot.ChatPrivate)
	if _, err := bc.setCurrentCwd(chatID, threadID, userID, cwd); err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ Diretório criado, mas o binding falhou: "+err.Error(), threadID)
	}
	bc.resetSessionOnProjectChange(chatID, threadID, userID, oldCwd, cwd)
	reply := fmt.Sprintf("✅ Tópico isolado.\n\nWorkspace: `%s`\nProjeto: `%s/%s`\nCaminho: `%s`\n\nA memória deste tópico agora é separada do DM e dos outros tópicos.", workspace, workspace, project, cwd)
	return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
}
