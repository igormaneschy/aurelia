## Why

Transformar o Aurelia em assistente pessoal residente no NAS: pesquisar na internet, criar documentos e organizar ideias e arquivos, sem depender do Mac. Uma instância própria do ai-memory deve separar esse conhecimento pessoal da memória de programação existente.

## What Changes

- Preparar execução nativa Linux de Aurelia, bridge Node/PI e ai-memory, sem Docker como requisito e sem hospedar um LLM grande no NAS.
- Instalar versões fixadas e compatíveis de Node/npm, PI SDK, extensões web, ai-memory e ai-jail/bubblewrap, sem substituir indiscriminadamente os componentes usados pelo sistema do NAS.
- Criar ambiente pessoal de memória e configuração independente, sem importar automaticamente a wiki, o perfil global ou os projetos de programação do Mac.
- Preparar perfil ai-jail para o daemon, com internet permitida e leitura/escrita nas pastas pessoais aprovadas; alinhar essas permissões com a política interna do Aurelia.
- Validar pesquisa real, leitura de fontes, produção de documentos, memória persistente e recuperação após reinício, antes da mudança de produção.
- Planejar migração seletiva, serviços systemd, backups e troca controlada do Telegram, garantindo um único consumidor do bot e evitando agendamentos duplicados.
- Garantir backup restaurável da memória pessoal e dos documentos; não há requisito de retorno do serviço ao Mac após o corte.
- Permitir que o cliente desktop no Mac acesse o daemon do NAS por um túnel SSH streamlocal sobre Tailscale, sem porta TCP pública, compartilhando a memória de projeto por cwd sem prometer continuidade do transcript/thread do Telegram.

## Capabilities

### New Capabilities

- `nas-personal-runtime`: execução pessoal nativa no NAS, com memória dedicada, acesso web e arquivos, proteção contra acidentes, persistência, transição controlada de produção com backup restaurável e acesso do desktop remoto por cwd.

### Modified Capabilities

Nenhuma mudança de requisito nas capacidades OpenSpec existentes. Ajustes internos de compatibilidade serão limitados aos necessários para cumprir a nova capacidade, sem redesenhar o motor PI.

## Impact

- Infraestrutura futura: conta de serviço sem privilégios administrativos, armazenamento persistente, Node/npm, bubblewrap, ai-jail, ai-memory e unidades systemd.
- Configuração futura: PI próprio do NAS, extensões e suas allowlists, credenciais por referência, workspace pessoal, permissões e regras de captura de memória.
- Possíveis ajustes no repositório: scripts/unidade Linux, operações de deploy e compatibilidade das ferramentas web; alterações de código exigem testes e revisão antes do uso real.
- Operação: o ai-memory de programação e os demais serviços do NAS ficam fora do escopo de modificação. Nenhuma troca de bot, importação de dados ou instalação é autorizada por estes artefatos.
- Exclusões: isolamento contra código hostil, indexação integral da pasta pessoal, modelos grandes locais, reconstrução do sistema de memória e promoção automática de branches/releases.
- Cliente: o desktop no Mac passa a ser um consumidor do daemon do NAS; o TUI fica desativado, mas o namespace de sessões da TUI é preservado para o desktop.
- Planejamento iniciado em 2026-11-07, na branch `feature/nas-personal-assistant`, baseada em `main` (`bbf6464`, v0.51.0). Para o PR v0.53.0, a branch é rebaseada em `main` v0.52.0; a feature de anexos v0.52.0 passa a fazer parte da base e é validada junto.
