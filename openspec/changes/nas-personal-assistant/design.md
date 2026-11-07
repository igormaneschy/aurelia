## Context

Motivação e escopo: [proposal.md](proposal.md). Contrato de comportamento: [nas-personal-runtime](specs/nas-personal-runtime/spec.md).

### Evidência já obtida, em 2026-11-07

| Item | Observação | Limite da evidência |
|---|---|---|
| NAS | Intel N100, 4 núcleos, x86_64, Debian 12, kernel 6.18, glibc 2.36 | Não é benchmark de produção |
| Recursos | 7,5 GiB de RAM utilizável, cerca de 3,4 GiB disponíveis; aproximadamente 1,4 TB livres em ext4 | Reavaliar antes de instalar |
| Persistência | `/home` e volume de dados em ext4; raiz do sistema em overlay | Não guardar o estado só na raiz overlay |
| Pasta pessoal existente | `/home/<nas-user>` corresponde à mesma pasta que `/volume2/@home/<nas-user>`, confirmado por dispositivo/inode | São duas formas de acessar o mesmo diretório, não duas cópias nem uma partição exclusiva |
| Permissões pessoais | Conta `<nas-user>` pode ler/escrever/atravessar a raiz; ACL POSIX observada `user::rwx`, `group::rwx`, `other::rwx` | Não foi realizado teste de escrita nem verificado acesso de uma futura conta de serviço a todos os arquivos |
| Runtime | Node 18.20.4; npm ausente | Insuficiente para o bridge atual: Node >=22.19.0 |
| Pesquisa | Busca real pelo Exa a partir do Node do NAS retornou HTTP 200 e fontes Debian; leitura HTTPS funcionou | Ainda não foi uma execução pelo Aurelia/PI instalado no NAS |
| DuckDuckGo | HTTP 202 com desafio de bot | Não usar como única garantia de pesquisa |
| Proteção | Namespaces de usuário/rede funcionaram; Landlock ABI 7 e seccomp disponíveis | ai-jail e bubblewrap ainda não instalados nem ensaiados com o daemon |
| Serviços | systemd de usuário funcionando, `Linger=no` | Persistência após logout exige configuração explícita |

Baseline inicial: `main` em `bbf6464` (v0.51.0). Para o PR v0.53.0, a branch é rebaseada em `main` v0.52.0; a feature de anexos já está no main e entra na base. A implantação registra o commit real e as versões instaladas, não apenas nomes de diretórios.

A verificação posterior da pasta pessoal consultou apenas montagens, metadados e nomes de diretórios. A área pessoal já contém dados e não precisa ser recriada ou movida. O volume compartilhado tem aproximadamente 1,4 TB livres, não uma quota exclusiva dessa pasta. Também existem diretórios de desenvolvimento e backups nessa área: sua inclusão no acesso não será presumida. Uma subpasta `/home/<nas-user>/Aurelia` é uma proposta de workspace/saída, ainda não criada; o estado do serviço e a base ai-memory ficam separados dos documentos pessoais.

### Restrições do código atual

- `scripts/aurelia.service.tmpl` e `scripts/install-systemd.sh` já oferecem serviço de usuário Linux; o instalador também inicia o serviço, portanto não deve ser executado prematuramente com o token de produção.
- `Makefile` ainda usa launchd em operações como deploy/restart/stop/status. Não usar esses alvos como se já administrassem o NAS; preferir o caminho systemd existente e corrigir apenas lacunas necessárias à operação adotada.
- `internal/pipeline/prompt_builder.go` resolve um cwd por conversa e aplica `DefaultCWD` a chats privados. O cwd do processo não é um workspace pessoal implicitamente autorizado.
- `internal/runtime/resolver.go` valida diretórios e prefixos; `/volume2/...` pode precisar de admissão explícita. `$HOME` inteiro não é um cwd de trabalho aceito.
- `internal/security/policy.go` já contém `allowed_outside_cwd`; uma autorização no ai-jail não substitui a autorização interna do bridge.
- `internal/bridge/setup.go` usa o HOME do usuário para recursos PI e symlinks. Mudar somente `AURELIA_HOME` não deve ser presumido como isolamento completo de todas as configurações.
- `cmd/aurelia/app.go` inicia Telegram e cron sem controles separados de staging. Não inventar flags existentes nem iniciar o daemon normal com o bot real durante a preparação.

## Goals / Non-Goals

**Goals:**
- Obter uma instalação nativa repetível e observável, com intervenção administrativa pontual e execução diária sem sudo ou grupo Docker.
- Dar leitura/escrita útil nas pastas pessoais aprovadas, mantendo documentos e memória após reinício.
- Manter internet funcional sob ai-jail e demonstrar a separação da memória pessoal também durante falhas, com recuperação por backup e sem retorno ao Mac.
- Definir gates verificáveis antes de instalação, ativação e troca de produção.

**Non-Goals:**
- Transformar o Aurelia em orquestrador, adicionar planejamento agentic próprio ou substituir o PI SDK.
- Usar Docker como pré-requisito, modificar outros serviços do NAS ou exigir reboot geral para preparar o ambiente.
- Importar toda a instalação do Mac, indexar todos os documentos ou habilitar aprendizado automático sem revisar captura e retenção.
- Implementar todas as melhorias da análise de memória anterior. Apenas falhas que bloqueiem este cenário entram neste trabalho.

## Decisions

### 1. Execução nativa na conta pessoal com versões próprias

A implementação revelou que os módulos `ugacl_vfs`/`ug_posix_acl` do UGREEN
escondem a pasta pessoal de contas não-donas, mesmo com modo 777, e ignoram ACLs
POSIX comuns. O grupo `admin` tem `sudo ALL`, então adicionar uma conta dedicada
a esse grupo anularia a separação. A conta temporária foi removida.

Decisão revista: executar como `<nas-user>`, dono da pasta pessoal, com
`NoNewPrivileges`, capabilities vazias, socket Docker/`.ssh`/estado sensível
ocultos e ai-jail para reduzir acidentes. Isso atende ao acesso amplo pedido,
mas não é uma fronteira de privilégio contra código hostil; esse risco residual
foi aceito pelo usuário. Não alterar recursivamente permissões do volume.

Instalar uma versão suportada e fixada de Node/npm em caminho próprio e declará-la no PATH da unidade; não trocar o Node 18 do sistema como efeito colateral. Usar o pin PI do commit candidato, verificando manifests de fonte e runtime. ai-memory 3.4.3, ai-jail 3.2.0 e pi-web-access 0.47.0 são referências observadas, não autorização de atualização automática. Downloads devem ser verificados pelos mecanismos oficiais disponíveis.

A compilação pode usar um ambiente compatível separado; não é necessário instalar compiladores permanentemente no NAS. Testar o artefato Linux no destino antes da ativação e preservar o binário anterior com substituição atômica.

Alternativas: reutilizar a conta administrativa simplifica permissões, mas amplia o alcance de um erro; Docker simplifica empacotamento, mas não é necessário nem o caminho escolhido pelo usuário.

### 2. Persistência e serviços independentes

Criar serviços systemd de usuário para a memória pessoal e o Aurelia, com HOME/estado persistentes, PATH explícito, logs no journal e reinício controlado. Habilitar lingering para a conta mediante autorização administrativa. Gerar unidades e perfis antes de ativá-los; a etapa de preparação não consome o bot de produção.

Separar fisicamente: binários/runtime, estado Aurelia/PI, estado ai-memory e pastas de documentos. Os caminhos finais são parâmetros; uma subpasta de trabalho real, e não `/` ou o HOME inteiro, será o `DefaultCWD` pessoal. Manter o diretório ai-memory fora dos diretórios expostos ao agente para edição direta.

O ai-memory escuta somente em loopback por padrão, com autenticação própria. Não depender do Mac, não abrir uma porta pública e não instalar uma segunda instância usando o mesmo diretório de dados. A memória permanece fora do jail do assistente, acessível via MCP.

Alternativa: um único processo supervisionando todos os serviços aumenta o acoplamento e dificulta recuperar a memória sem interromper o atendimento.

### 3. PI pessoal e memória iniciada limpa

Provisionar o PI sob a conta do NAS e instalar a extensão ai-memory uma única vez nesse ambiente. Manter symlinks locais de `auth.json`/`models.json` entre os diretórios PI suportados; não copiar symlinks absolutos, caches, regras de coding ou credenciais do Mac indiscriminadamente. Credenciais são configuradas por fluxo aprovado ou referências a arquivos privados, nunca incorporadas ao plano, à linha de comando ou aos logs.

Definir identidade canônica de workspace/projeto pessoal para a pasta de trabalho, mesmo sem Git. Validar que o bloco de escopo do Aurelia não impõe um basename conflitante com a identidade resolvida pela extensão. Se houver conflito, corrigir a integração existente sem criar outro resolvedor/capturador no Go.

Começar com base pessoal vazia. Importar notas antigas apenas por lista aprovada, sem apagar os originais. Nunca configurar o ai-memory de programação como fallback. Verificar endpoint/escopo efetivos por evidências sanitizadas, incluindo em uma chamada feita pelo agente, e não somente em uma chamada administrativa.

Configurar captura, exclusões e retenção antes de usar documentos reais. Separar a permissão para ler um arquivo da permissão para persistir seu conteúdo em observações/wiki. Primeiro usar corpus sintético; informar o usuário sobre consultas e trechos enviados a modelos/serviços externos. Backups incluem memória pessoal e documentos e seguem o procedimento consistente do serviço, não cópia ingênua de SQLite aberto.

Dream/Nudge locais ficam desativados na primeira validação. Só reativar após justificar seu papel operacional e confirmar o contrato efetivo de zero ferramentas; a análise identificou perda da lista vazia por `omitempty`/tradução do bridge. Isso não impede a captura nativa do ai-memory, que tem política própria a revisar.

### 4. ai-jail flexível, sem execução privilegiada

Preparar um perfil para iniciar o daemon completo e seus filhos Node/PI sob ai-jail. É configuração de um comando personalizado, não uma integração já pronta do Aurelia. Validar execução headless, sinais, encerramento, IPC e persistência antes de promovê-lo.

Escolher internet liberada para pesquisa em domínios variados; não usar o preset de coding como se já autorizasse Telegram, provedores de busca e memória. Com rede aberta, não combinar mecanismos de port-forward destinados ao namespace de rede fechado. Liberar apenas os diretórios/env necessários, sem `--inherit-env` amplo, SSH agent, sockets Docker ou acessos de administração.

Mapear pastas pessoais aprovadas como leitura/escrita, runtime como leitura e os diretórios operacionais necessários para caches, sessões e renovação de credenciais. Revisar os destinos reais dos symlinks. A política interna `DefaultCWD`, prefixos de cwd, perfis e `allowed_outside_cwd` deve representar os mesmos caminhos; não usar `privileged` global para contornar divergências.

Preparar aprovações do perfil em sessão administrativa, depois executar o serviço sem TTY ou perguntas interativas. Falha do perfil deve parar a inicialização, não remover o jail como fallback. Rede aberta permite transmissão dos arquivos legíveis: ai-jail limita acidentes de filesystem, não elimina esse risco nem substitui uma VM para código hostil.

O wrapper `scripts/aurelia-jail.sh` usa `--network --no-save-config --rw-map "$HOME"` e nega `.ssh`, `.config`, `.docker`, o banco ai-memory e `#recycle`. A unidade systemd usa `WorkingDirectory=$HOME/.aurelia`: o ai-jail recusa o config store `~/.config/ai-jail` quando ele fica dentro do diretório do projeto, e `.aurelia` também evita transformar a home inteira no cwd implícito do processo. Teste headless confirmou rede, escrita no workspace, negação de `.ssh` e negação do banco ai-memory. ai-jail ausente falha fechado no wrapper.

Alternativas: read-only geral prejudica a tarefa pretendida; allowlist estreita de domínios prejudica pesquisa livre; execução nativa sem jail é possível tecnicamente, mas não substitui silenciosamente o perfil aprovado se este falhar.

### 5. Paridade web e arquivos comprovada, não presumida

Instalar pi-web-access e os recursos MCP necessários em versões fixadas. Verificar a superfície real por `session.getAllTools()` e executar as ferramentas pelo caminho PI/bridge, além dos testes de HTTPS. A lista estática do Aurelia contém nomes legados, enquanto a extensão observada registra também ferramentas de ativação/verificação: corrigir somente incompatibilidades reproduzidas, sem liberar automaticamente tudo o que qualquer extensão registrar.

Teste principal: pedido genérico em chat privado com workspace pessoal configurado, pesquisa pública, abertura de fonte, criação/edição de Markdown com citações e cópia para uma segunda pasta autorizada. Conferir conteúdo e destinos no disco. PDF/DOCX e sites autenticados não são requisitos iniciais; só adicionar conversores se houver uma necessidade concreta.

Testar erro de provedor e indisponibilidade de memória. Para caminhos negados usar arquivos sentinela descartáveis, nunca tentar modificar dados reais de outros serviços. Anexos Telegram são teste adicional apenas se o commit candidato aprovado incluir a feature correspondente; validar bytes/caminho de origem, não só a resposta do modelo.

### 6. Staging sem segundo consumidor de produção

Preparar ambiente com estado e corpus sintéticos, sem importar crons reais. Usar testes do bridge/pipeline e uma superfície headless que não inicie o Telegram de produção. Para um ensaio completo anterior à troca, um bot de teste separado é opcional. Não usar o daemon normal com o token atual para simular um modo staging inexistente.

Se o ensaio exigir um controle de inicialização ainda ausente, propor e testar a menor mudança necessária antes de ativá-lo; não ampliar o produto com uma plataforma de staging por antecipação. Sem bot de teste, a validação Telegram final ocorre na janela de troca aprovada, com o backup de dados já ensaiado; não há retorno do serviço ao Mac.

### 7. Desktop remoto por streamlocal, com identidade da persona

O desktop no Mac não deve abrir uma porta TCP no NAS. O transporte é um túnel
SSH streamlocal (`ssh -N -L ~/.aurelia/nas.sock:/home/<nas-user>/.aurelia/aurelia.sock`)
sobre Tailscale, mantido por launchd e reutilizando a chave já na keychain. O
sshd precisa de `AllowTcpForwarding local` e `AllowStreamLocalForwarding yes`;
o socket local e o remoto ficam 0600. O túnel não cria chaves nem amplia a
autorização existente do usuário; o shell SSH já é mais poderoso que o
forwarding.

O cliente desktop usa `~/.aurelia/nas.sock` quando o marcador
`~/.aurelia/nas-tunnel.enabled` existe e envia `user_id=0`; o daemon resolve a
persona canónica e mantém a checagem de peer UID. O UID local do Mac não é
usado como identidade remota. A memória compartilhada é a de projeto
(`cwd_overlay` + ai-memory pelo marker); transcript, continuity, camada `topic`
do Telegram, binding e lista de sessões permanecem separados. O TUI pode ficar
desativado; o desktop usa o namespace de sessões da TUI no NAS, que não deve ser
apagado.

## Risks / Trade-offs

- [Dados pessoais podem sair pela rede aberta] -> transparência sobre provedores, exclusões de captura e credenciais de menor alcance; ai-jail não é proteção contra toda exfiltração.
- [Permissões do NAS e do bridge divergem] -> testar leitura/escrita em cada raiz e negação sobre sentinela fora do escopo.
- [Perfil ai-jail não funciona com daemon/Node/IPC] -> ensaio headless antes do corte; falha bloqueia promoção, sem fallback irrestrito.
- [Conta do NAS tem privilégios excessivos] -> conta dedicada e grants em pastas específicas; não reutilizar autoridade administrativa por conveniência.
- [Raiz pessoal existente tem permissões POSIX amplas] -> não tratar a criação de outra conta como isolamento suficiente; usar mapas/exclusões do jail e validar os destinos aprovados, sem aplicar chmod/chown recursivo à pasta pessoal.
- [Dois bots ou crons executam a mesma ação] -> consumidor de produção exclusivo, crons sem importação automática e ativação explícita de cada job revisado.
- [Memória pessoal cai no servidor/escopo de coding] -> ambiente próprio, canário sintético, verificação de roteamento e nenhum fallback para o servidor atual.
- [Importação traz caminhos absolutos ou credenciais obsoletas] -> inventário seletivo, sessões novas e autenticação preparada no destino.
- [Pressão de memória, CPU ou I/O afeta outros serviços] -> medir o cenário completo e recuperação, limitar concorrência/background, só habilitar embeddings/consolidação após medir; não prometer footprint com base no tamanho do binário/modelo.
- [Atualização reinicia Mac ou promove feature alheia] -> branch própria, sem commits/deploy automáticos durante planejamento, versão candidata aprovada e pipeline Linux explícito.
- [Túnel SSH vira uma porta de acesso ampla] -> streamlocal apenas, sockets 0600, chave existente na keychain, `AllowTcpForwarding local` (sem `-R`), host key estrita e logs; o shell SSH já é mais poderoso que o forwarding.
- [Desktop e Telegram competem pelo mesmo cwd] -> a memória de projeto é intencionalmente compartilhada; documentar que transcript/continuity são separados e evitar edição concorrente dos mesmos arquivos.

## Migration Plan

| Fase | Entrega | Gate de saída |
|---|---|---|
| 0. Aprovação operacional | Paths/grants, destino de memória, credenciais por referência, baseline, inventário e estratégia Telegram | Usuário aprova instalações e alterações de permissão; nenhum corte implícito |
| 1. Fundação nativa | Conta, Node/npm, artefato Linux, persistência e serviços preparados | Dependências e binários executam, sem polling/crons de produção |
| 2. PI e memória pessoal | Extensões, endpoint autenticado, identidade e captura/backup próprios | Agente grava/recupera canário só na instância pessoal; restauração isolada funciona |
| 3. Proteção e workspace | Perfil ai-jail headless e política Aurelia alinhados | Web, arquivos aprovados, memória, sinais e IPC funcionam; sentinela protegido não muda |
| 4. Ensaio e backup | Cenário integrado, reinício/logout, consumo medido e backup/restauração ensaiados | Evidências sanitizadas aprovadas; sem impacto nos serviços existentes |
| 5. Troca e observação | Parar consumidor Mac, ativar NAS e depois jobs revisados | Telegram completo validado, um consumidor, sem duplicação; aceite explícito |

### Dados da instalação antiga

Preservar backup de origem antes de qualquer importação. Candidatos a importação: notas pessoais, persona/perfil e preferências aprovados, após remover referências operacionais ao Mac. Não copiar a árvore `~/.aurelia` inteira. Não migrar automaticamente sessões PI, `sessions.json`, sockets, logs, binários, caches, project bindings, continuidade, runlogs ou crons com caminhos do Mac. Jobs desejados são revisados/recriados individualmente. Nenhum arquivo secreto é lido ou transportado sem autorização específica.

### Corte e recuperação de dados

1. Registrar binários/configurações e backups consistentes e confirmar que não há execução importante em andamento.
2. Somente na janela autorizada, parar o consumidor Mac e impedir que launchd/KeepAlive ou um boot o reative; preservar instalação e dados.
3. Iniciar o atendimento NAS com crons pessoais ainda inativos; validar resposta, pesquisa, documento e lembrança pelo Telegram.
4. Ativar apenas jobs revisados e confirmar executor único. Observar erros, permissões, captura e consumo antes de declarar conclusão.
5. **Não há rollback de serviço ao Mac.** Se uma falha após o corte exigir interromper o atendimento, parar o consumidor/jobs NAS, preservar os dados novos e manter o Mac inativo como assistente. A recuperação é feita por backup/restauração de dados em destino isolado; não reativar o Mac como consumidor nem sincronizar bancos entre instâncias ativas.
6. Nunca promover uma cópia antiga sobre a memória pessoal nova como efeito colateral de uma restauração.

Reboot geral do NAS, alterações de firewall/ACL/Tailscale, abertura de portas, ativação de jobs e promoção de release têm autorização própria; não fazem parte de um teste de reinício de serviço. Se houver alterações Go/bridge, executar a validação do repositório, revisão backend/segurança e validação live conforme a política de branches antes de propor uma release.

## Prior Lessons Applied

- `.opencode/lessons/learned/auth-symlink-instead-of-copy.md`: autenticação PI do daemon deve acompanhar a fonte local do NAS por symlink; seguir a regra atual de `AGENTS.md` também para models, não detalhes históricos já superados da lição.
- `.opencode/lessons/learned/pi-sdk-pin-drift.md`: conferir versão realmente instalada e superfície SDK, não apenas a existência de `node_modules`.
- `.opencode/lessons/learned/chat-mode-tool-policy.md`: permissões e instruções precisam concordar com o workspace pessoal; não depender só de prompt ou da permissão do jail.
- `AGENTS.md`, diagnóstico de extensions: testar tools reais e manter o SDK externalizado; tools citadas no prompt não provam registro/disponibilidade.

## Open Questions

São parâmetros de implantação que não alteram a abordagem:

- Provisionar a chave `opencode-go` aprovada e escolher o modelo final; o runtime atual usa a credencial DeepSeek existente.
- Implementar upload de bytes para anexos do desktop no modo NAS; hoje o protocolo envia apenas caminhos locais do Mac.
- Definir um destino de backup off-volume quando houver outro disco ou armazenamento de rede.
- Promover a v0.53.0 após o PR, reconstruir o binário Linux e repetir os checks live de Telegram + desktop.

## References

- `.specs/codebase/AGENT_RESPONSIBILITY_MODEL.md`: fronteira produto/PI/wiki.
- `scripts/aurelia.service.tmpl`, `scripts/install-systemd.sh`, `Makefile`, `docs/OPERATIONS.md`: ciclo operacional existente.
- `internal/bridge/setup.go`, `internal/bridge/protocol.go`, `bridge/index.ts`, `internal/security/extension_tools.go`: instalação e superfície de tools.
- `internal/pipeline/prompt_builder.go`, `internal/runtime/resolver.go`, `internal/security/policy.go`: workspace e permissões.
- [ai-jail v3.2.0](https://github.com/akitaonrails/ai-jail/blob/v3.2.0/README.md): rede, mapas, execução customizada e limites de proteção.
- [ai-memory v3.4.3 deploy](https://github.com/akitaonrails/ai-memory/blob/v3.4.3/docs/deploy.md) e [install](https://github.com/akitaonrails/ai-memory/blob/v3.4.3/docs/install.md): dados persistentes, backup e serviço nativo.
