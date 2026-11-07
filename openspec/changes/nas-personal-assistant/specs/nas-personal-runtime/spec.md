## Purpose

Permitir que o Aurelia opere como assistente pessoal persistente no NAS, com pesquisa web, documentos e memória própria, sem depender do Mac nem compartilhar automaticamente conhecimento com o ambiente de programação.

## ADDED Requirements

### Requirement: Operação nativa e persistente no NAS

O ambiente SHALL executar o assistente e sua memória como serviços nativos Linux, sem exigir Docker, sessão SSH aberta, GPU ou modelo de linguagem grande local. Seus dados SHALL residir em armazenamento persistente, e a ausência de uma dependência obrigatória SHALL produzir um diagnóstico acionável antes da entrada em produção.

#### Scenario: Sessão administrativa encerrada
- **WHEN** a sessão SSH usada para administrar uma instalação aprovada é encerrada
- **THEN** os serviços continuam disponíveis e seus dados permanecem acessíveis no próximo atendimento

#### Scenario: Reinício dos serviços
- **WHEN** os serviços são reiniciados após terem salvo um documento e uma lembrança de teste
- **THEN** o documento e a lembrança continuam disponíveis sem depender de arquivos ou serviços do Mac

#### Scenario: Dependência incompatível
- **WHEN** uma dependência obrigatória está ausente ou em versão incompatível
- **THEN** a validação informa a dependência e a versão esperada e impede a troca de produção

### Requirement: Pesquisa e leitura de fontes públicas

O assistente SHALL pesquisar assuntos genéricos na internet e ler fontes públicas sem exigir um repositório de código vinculado. Uma pesquisa validada SHALL retornar fontes verificáveis; um bloqueio ou indisponibilidade SHALL ser comunicado sem apresentar uma resposta inventada como resultado de pesquisa.

#### Scenario: Pesquisa com produção de documento
- **WHEN** o usuário pede uma pesquisa pública e um resumo salvo na pasta pessoal aprovada
- **THEN** o assistente consulta a web, lê ao menos uma fonte e produz um documento com referências verificáveis

#### Scenario: Provedor bloqueado
- **WHEN** um provedor exige CAPTCHA, recusa a requisição ou fica indisponível
- **THEN** o assistente usa uma alternativa configurada ou informa a limitação, sem afirmar que consultou fontes que não acessou

### Requirement: Memória pessoal independente

As leituras, escritas e capturas de memória do assistente SHALL usar exclusivamente a instância pessoal aprovada. A instalação SHALL iniciar sem importar automaticamente a wiki ou preferências globais de programação, e uma falha da memória pessoal SHALL NOT redirecionar operações para o serviço de programação.

#### Scenario: Lembrança pessoal após reinício
- **WHEN** o usuário pede explicitamente para guardar uma informação sintética e consulta essa informação após reiniciar os serviços
- **THEN** a informação é recuperada da instância pessoal no escopo esperado

#### Scenario: Memória pessoal indisponível
- **WHEN** a instância pessoal está inacessível
- **THEN** o assistente informa a indisponibilidade e não consulta nem escreve na instância de programação como fallback

#### Scenario: Configuração sem identidade de memória verificável
- **WHEN** o destino ou o escopo efetivo de uma escrita não pode ser confirmado
- **THEN** a validação impede a promoção dessa configuração para produção

### Requirement: Acesso funcional às pastas pessoais aprovadas

O assistente SHALL ler, criar e editar arquivos nas pastas pessoais aprovadas, sem impor um modo global somente leitura. A permissão operacional e a política do assistente SHALL concordar sobre os caminhos autorizados. A instalação SHALL NOT conceder administração do NAS ou acesso aos serviços existentes para viabilizar esse trabalho.

#### Scenario: Escrita em duas pastas autorizadas
- **WHEN** o usuário pede um documento no workspace pessoal e uma cópia em outra pasta aprovada para escrita
- **THEN** ambos os arquivos são produzidos nos destinos pedidos e permanecem acessíveis ao usuário

#### Scenario: Caminho protegido
- **WHEN** uma tarefa tenta modificar um arquivo sentinela em uma área de teste equivalente a um diretório protegido
- **THEN** a operação é negada e o arquivo permanece inalterado, sem elevar privilégios

### Requirement: Proteção proporcional e explícita

A proteção operacional SHALL permitir pesquisa em domínios públicos variados e acesso ao serviço de memória pessoal, mantendo o alcance de arquivos limitado ao aprovado. A configuração SHALL ser previamente aprovada e executável sem diálogos interativos no atendimento; falhas dessa proteção SHALL NOT iniciar uma execução irrestrita silenciosamente.

#### Scenario: Pesquisa com a proteção ativa
- **WHEN** o assistente pesquisa, abre uma fonte e salva um documento sob a proteção operacional aprovada
- **THEN** a tarefa completa sem exigir uma confirmação administrativa para cada domínio consultado

#### Scenario: Falha no perfil de proteção
- **WHEN** o perfil está inválido ou uma dependência da proteção está ausente
- **THEN** o serviço registra o motivo e não inicia automaticamente fora dessa proteção

### Requirement: Privacidade e migração seletiva

A migração SHALL preservar os dados de origem e importar apenas os itens pessoais aprovados. Histórico de programação, credenciais, sessões e caminhos absolutos do Mac SHALL NOT ser copiados indiscriminadamente. O usuário SHALL ser informado de que o uso de modelos ou serviços externos pode transmitir consultas e trechos de documentos, e a captura de memória SHALL respeitar as exclusões aprovadas.

#### Scenario: Importação pessoal limitada
- **WHEN** o usuário aprova um conjunto de notas pessoais para migração
- **THEN** somente esse conjunto é importado, os originais são preservados e o resultado é conferido sem misturar a wiki de programação

#### Scenario: Documento excluído da captura
- **WHEN** o assistente usa um documento sintético coberto por uma exclusão aprovada de captura
- **THEN** o conteúdo não aparece nas observações ou páginas da memória pessoal

### Requirement: Troca exclusiva do atendimento e dos agendamentos

A troca para o NAS SHALL exigir aprovação explícita e assegurar que apenas um processo consuma o bot de produção. Agendamentos migrados SHALL ter um único executor ativo e destinos revisados para o NAS. Os testes anteriores à troca SHALL NOT disputar o token de produção ou executar agendamentos reais da instalação atual.

#### Scenario: Testes anteriores à migração
- **WHEN** o ambiente do NAS é validado enquanto o Mac continua atendendo
- **THEN** os testes usam uma superfície isolada e não causam conflito de polling nem execução duplicada de agendamentos

#### Scenario: Troca aprovada
- **WHEN** o usuário autoriza a troca após os testes de aceitação
- **THEN** o consumidor antigo é encerrado e sua retomada automática é impedida antes de ativar o consumidor do NAS

### Requirement: Backups recuperáveis sem mistura de memória

A operação SHALL manter backups consistentes da memória pessoal e dos documentos e SHALL permitir restauração em destino isolado. Não há requisito de retorno do serviço ao Mac após o corte; uma falha após a troca SHALL ser tratada por interrupção do serviço e recuperação de dados, sem reativar o Mac como assistente.

#### Scenario: Restauração de backup
- **WHEN** uma cópia de segurança da memória pessoal é restaurada em um destino isolado de teste
- **THEN** uma lembrança sintética previamente registrada pode ser recuperada sem alterar o serviço ou a wiki de programação

#### Scenario: Falha após a troca
- **WHEN** uma falha após o corte exige interromper o atendimento no NAS
- **THEN** o serviço é parado, os dados do NAS são preservados e o Mac não é reativado como consumidor; a recuperação é feita por backup/restauração de dados

### Requirement: Acesso do desktop ao NAS

O cliente desktop no Mac SHALL acessar o daemon do NAS por um transporte
autenticado e criptografado, sem exigir uma porta TCP pública. O cliente SHALL
usar a identidade canónica da persona resolvida pelo daemon; a checagem de peer
UID do socket SHALL NOT ser removida para viabilizar o acesso remoto. Um novo
`chat_id` de desktop SHALL NOT ser tratado como continuação automática do
transcript, da continuidade ou da camada `topic` de um thread do Telegram. A
memória compartilhada entre superfícies SHALL ser a de projeto, derivada do
cwd (`cwd_overlay` + projeto ai-memory), e um novo desktop session SHALL exigir
`/cwd` explícito em vez de herdar o binding do DM ou de um tópico.

#### Scenario: Acesso remoto pela tailnet
- **WHEN** o Mac está na mesma tailnet do NAS e o túnel está ativo
- **THEN** o desktop conecta ao daemon, mostra o status online e opera no cwd
  configurado sem abrir uma porta TCP pública no NAS

#### Scenario: Identidade remota
- **WHEN** o desktop envia uma requisição com `user_id=0`
- **THEN** o daemon resolve a persona canónica e mantém a validação do peer UID;
  um UID local do Mac não é aceito como identidade remota

#### Scenario: Memória de projeto compartilhada
- **WHEN** uma sessão nova do desktop usa `/cwd` de um tópico do Telegram
- **THEN** ela compartilha cwd, arquivos, `cwd_overlay` e o projeto ai-memory,
  mas não carrega automaticamente transcript, continuidade, camada `topic`,
  binding ou lista de sessões do tópico

#### Scenario: TUI desativado
- **WHEN** o cliente TUI não é usado no Mac
- **THEN** o desktop continua funcionando no namespace de sessões da TUI e os
  dados de `tui_sessions.db` são preservados
