# Spec — Catálogo de comandos remotos controlados

- **Status:** em progresso — base do catálogo implementada no agente Go; nenhuma action nova
- **Origem:** evolução explicitamente fora do escopo do MVP `KILL_ANYDESK`
- **Pré-requisito:** agente Go V1 estável em produção

## Objetivo

Evoluir o agente de uma única ação allowlisted para um catálogo pequeno, explícito e auditável de comandos remotos. A evolução não pode introduzir shell remoto nem execução de texto recebido pela fila.

## Escopo

1. Definir um registry compilado de ações e schemas de argumentos por versão.
2. Adicionar comandos somente após cada um ter uma spec e testes próprios.
3. Migrar transporte para Redis Streams com consumer groups, confirmação (`XACK`) e recuperação de pendências antes de comandos não idempotentes.
4. Incluir resultado estruturado, correlação, expiração, deduplicação por `commandId` e retenção limitada.
5. Expor no n8n apenas opções do catálogo e campos validados — nunca um campo livre de comando.
6. Aplicar ACL Redis por agente e, se necessário, assinatura criptográfica de envelopes.
7. Criar auditoria de quem solicitou, alvo, ação, resultado e timestamps, sem segredos nem payloads sensíveis.

## Não escopo

- PowerShell, CMD, Bash ou qualquer shell genérico;
- terminal interativo;
- upload/download de arquivos;
- captura ou controle de tela, teclado, mouse, áudio ou webcam;
- elevação de privilégio, bypass de UAC ou instalação de serviço;
- autoatualização nesta primeira evolução.

## Regras de segurança obrigatórias

- O agente recebe somente JSON e despacha apenas handlers compilados.
- Cada action declara schema, autorização, expiração máxima e semântica de idempotência.
- Não usar `exec`, `eval`, `os/exec` com strings recebidas nem shell.
- O workflow n8n não decide comandos arbitrários: ele apenas cria envelopes permitidos.
- Toda action nova precisa de threat model curto e testes de rejeição de payload inválido.

## Critérios de aceite

- Uma action desconhecida ou com argumentos fora do schema é rejeitada e auditada.
- Mensagens duplicadas não repetem uma ação não idempotente.
- Mensagens pendentes são recuperáveis após reinício do agente.
- O n8n mostra apenas ações autorizadas para cada alvo.
- Não existe caminho de execução genérica de processos ou shell.

## Estado da implementação

Implementado no agente Go, com `KILL_ANYDESK` como única entrada do catálogo:

- registry compilado com schema de argumentos, idempotência e expiração máxima por action
  (`agent-go/catalog.go`);
- autorização por máquina via `allowedActions` no `config.json`; uma action fora do catálogo
  compilado reprova o carregamento da configuração, e uma action não habilitada é rejeitada com
  motivo próprio;
- teto de vida útil do envelope por action, além da checagem de expiração já existente;
- deduplicação por `commandId` em memória, limitada por idade e por quantidade, antes de qualquer
  handler rodar;
- resultado estruturado com `action` e `status` (`SUCCEEDED`, `FAILED`, `DUPLICATE`) e auditoria em
  `remote-agent:audit:<máquina>` sem argumentos nem payload;
- retenção limitada nas listas de resultado, auditoria e dead-letter;
- falha de execução deixou de derrubar o transporte: ela vira resultado `FAILED` auditado.

Processo para cada action nova: [`adicionar-nova-action.md`](adicionar-nova-action.md).

## Pendências bloqueantes

1. **Decisão do primeiro comando adicional.** Definir a action, a justificativa operacional e se ela
   é idempotente. Sem isso, nenhuma ação além de `KILL_ANYDESK` será implementada.
2. **Transporte.** A migração para Redis Streams com consumer group, `XACK` e recuperação de
   pendências continua pendente e é pré-requisito da primeira action não idempotente. Ela depende de
   uma decisão de produtor: o nó Redis do n8n expõe operações de lista (`push`/`pop`) e não tem
   `XADD`, então migrar só o consumidor deixaria o workflow sem como enfileirar. As opções são um nó
   produtor diferente, um pequeno serviço publicador entre n8n e Redis, ou aceitar um produtor fora
   do n8n.
3. **ACL Redis por agente e assinatura de envelope.** Trabalho de implantação, ainda não iniciado.
