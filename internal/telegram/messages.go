package telegram

const (
	msgUnsupportedDocument = "⚠️ **Formato não suportado**\n\n" +
		"No momento eu consigo processar:\n" +
		"- arquivos `.md`\n" +
		"- arquivos `.pdf`\n" +
		"- imagens em `.jpg`, `.png`, `.gif` ou `.webp`\n" +
		"- áudio e voz\n\n" +
		"💡 Dica: converta para `.pdf` ou copie o texto diretamente."

	msgDownloadFailure = "❌ **Falha no download**\n\n" +
		"Não consegui baixar o arquivo enviado pelo Telegram. Tente novamente."

	msgAudioNotConfigured = "⚠️ **Áudio indisponível**\n\n" +
		"Meu módulo de transcrição não está configurado.\n\n" +
		"Configure `groq_api_key` no arquivo `~/.aurelia/config/app.json`."

	msgAudioProcessingFailure = "❌ **Falha na transcrição**\n\n" +
		"Não consegui compreender o áudio. Tente falar mais claro ou mais perto do microfone."

	msgEmptyAudio = "⚠️ **Áudio vazio**\n\n" +
		"Não captei conteúdo útil. Pode reenviar?"

	msgAlreadyConfigured = "✅ **Aurelia online**\n\n" +
		"Já estou configurado e pronto. Como posso ajudar?"

	msgBootstrapWelcome = "# Boas-vindas\n\n" +
		"Eu sou o **Aurelia** recém-iniciado.\n\n" +
		"Escolha como você quer que eu atue primariamente hoje."

	msgBootstrapFailure = "❌ **Falha no bootstrap**\n\n" +
		"Não consegui criar os arquivos base de persona."

	msgBootstrapAssistant = "✅ **Modo inicial selecionado**\n\n" +
		"Agora descreva como você quer que eu seja: personalidade, tom, estilo.\n\n" +
		"Exemplo: `Quero um assistente direto, sem floreios, que use humor seco quando apropriado.`"

	msgBootstrapProfile = "✅ **Personalidade configurada**\n\n" +
		"Agora me diga seu nome e como prefere que eu trabalhe com você.\n\n" +
		"Exemplo: `Me chamo Igor, sou dev e quero respostas diretas.`"

	msgBootstrapSuccess = "✅ **Personas criadas**\n\n" +
		"Suas configurações base foram salvas em `~/.aurelia/memory/personas/`.\n\n" +
		"Você já pode conversar comigo ou editar:\n" +
		"- `IDENTITY.md`\n" +
		"- `SOUL.md`\n" +
		"- `USER.md`\n\n" +
		"para refinar nosso comportamento."
)

func unsupportedDocumentMessage() string     { return msgUnsupportedDocument }
func downloadFailureMessage() string         { return msgDownloadFailure }
func audioNotConfiguredMessage() string      { return msgAudioNotConfigured }
func audioProcessingFailureMessage() string  { return msgAudioProcessingFailure }
func emptyAudioMessage() string              { return msgEmptyAudio }
func alreadyConfiguredMessage() string       { return msgAlreadyConfigured }
func bootstrapWelcomeMessage() string      { return msgBootstrapWelcome }
func bootstrapFailureMessage() string       { return msgBootstrapFailure }
func bootstrapAssistantMessage() string    { return msgBootstrapAssistant }
func bootstrapProfileMessage() string      { return msgBootstrapProfile }
func bootstrapSuccessMessage() string      { return msgBootstrapSuccess }