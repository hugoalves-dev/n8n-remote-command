# Como adicionar uma action ao catálogo

Este checklist é obrigatório para cada nova action. Ele existe porque o catálogo só continua
seguro enquanto cada entrada for pequena, explícita e revisada isoladamente.

Nada aqui autoriza uma action nova: a decisão pendente em
[`controlled-remote-command-catalog.md`](controlled-remote-command-catalog.md) continua valendo.

## 1. Spec própria

Escreva `docs/specs/<action>.md` com:

- justificativa operacional — qual problema real ela resolve e com que frequência;
- efeito exato no host, em uma frase;
- semântica de idempotência: repetir a action produz o mesmo estado final?
- expiração máxima aceitável;
- quais máquinas devem recebê-la.

Sem semântica de idempotência declarada, a action não entra.

## 2. Threat model curto

Responda, no mesmo arquivo:

| Pergunta | Resposta obrigatória |
| --- | --- |
| O que um produtor comprometido consegue fazer com esta action? | |
| O que um Redis comprometido consegue fazer? | |
| A action aceita algum caminho, nome de processo ou texto vindo da fila? | Deve ser **não**. |
| Ela executa binário fora de uma lista fixa em código? | Deve ser **não**. |
| Ela precisa de privilégio elevado? | Deve ser **não**. |
| Ela é reversível? Se não, qual o custo de um disparo indevido? | |

Uma resposta fora do esperado reprova a action.

## 3. Entrada no catálogo compilado

Em `agent-go/catalog.go`, adicione uma entrada declarando `name`, `idempotent`, `maxLifetime`,
`enabledByDefault` e as funções `parseArgs`/`execute`.

- `enabledByDefault` deve ser `false`. A action nasce inerte e só passa a valer numa máquina que
  a liste em `allowedActions`.
- `parseArgs` valida o schema inteiro: tipos, chaves desconhecidas e faixas. Use
  `argumentFields` para rejeitar qualquer chave não declarada.
- `execute` recebe apenas os argumentos já tipados. Não use `os/exec` com string recebida, não
  monte linha de comando por concatenação e não chame shell.

## 4. Testes de rejeição

Além do teste do caminho feliz, cada action precisa de testes provando que o agente rejeita:

- argumentos que não são objeto;
- chave desconhecida;
- tipo errado em cada campo declarado;
- `expiresAt - requestedAt` acima do `maxLifetime` da action;
- a action chegando numa máquina que não a habilitou.

## 5. Transporte

Se a action **não** for idempotente, a migração para Redis Streams com consumer group, `XACK` e
recuperação de pendências é pré-requisito de merge, não trabalho posterior. A dedup por
`commandId` do agente é uma proteção em memória: ela não sobrevive a uma reinicialização.

## 6. Interface n8n

O formulário só pode oferecer opções do catálogo, sempre como lista fixa. Nenhum campo livre de
comando, caminho ou argumento textual pode chegar ao envelope.

## 7. Revisão

Merge só depois de spec, threat model, entrada no catálogo, testes de rejeição e — quando
aplicável — transporte confirmado. Habilite a action em uma máquina de cada vez.
