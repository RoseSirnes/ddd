# DDD — funcionamento automático, sem configurações para os usuários

Este projeto está preparado para que, **depois da ativação do serviço online**, as pessoas usem o DDD sem digitar servidor, sem configurar GitHub e sem baixar o executável em cada atualização.

## O que acontece com o usuário

- O DDD.exe já vem com o endereço oficial do servidor de mensagens embutido durante a compilação.
- Contas, amigos, conversas e fotos passam a ser guardados no servidor online; nunca é necessário escolher um servidor no aplicativo.
- O DDD verifica o GitHub Releases **ao ser aberto**. Quando houver versão superior com assinatura válida, baixa, verifica e substitui o executável, depois reabre.
- No menu Atualizações, é possível conferir a versão. A pessoa não precisa configurar o repositório.
- Caso o servidor esteja indisponível, o aplicativo **não finge estar no chat global**; informa erro de conexão. Os dados do servidor não passam automaticamente para modo local.

## Etapa única do responsável pelo DDD (ainda NÃO concluída)

1. Vincular uma conta GitHub ao projeto e publicar os arquivos deste ZIP em um repositório, preferencialmente privado enquanto se implementam moderação, backup e segurança de produção. Observação: o mecanismo atual de autoatualização usa a URL pública de GitHub Releases; é necessário um repositório/releases publicamente acessíveis para que os executáveis façam downloads sem credenciais.
2. Vincular um serviço de hospedagem como Railway ao repositório para executar o backend DDD. Usar `Dockerfile.central`, iniciar somente **uma instância** e montar um volume persistente em `/data` com backups automáticos. Configurar TLS/HTTPS e um domínio estável para acesso de todos.
3. No GitHub, registrar o segredo **`DDD_SIGNING_KEY`** com a chave privada de assinatura, sem divulgá-la nem versioná-la. A chave deve combinar com a pública em `update_public_key.go`. Se suspeitar de exposição da chave, rotacioná-la de maneira planejada.
4. No GitHub, configurar a variável **`DDD_SERVER_URL`** com o endereço definitivo, por exemplo `https://chat.exemplo.com` (sem barra no fim).
5. Após isso, os envios para a branch `main` acionam `.github/workflows/publicar-ddd.yml`. O fluxo testa o Go, compila o Windows com URL do servidor e repositório oficiais embutidos, assina `ddd-update.json` e publica a release de `appVersion`, se não existir. Para cada mudança no executável, incrementar `appVersion` antes do commit.
6. Testar com duas instalações Windows em PCs distintos, incluindo atualização in-place, recuperação de conexões e persistência após atualização do servidor. Distribuir **uma vez** a release inicial do DDD.exe.

A publicação de *novas mudanças feitas pelo ChatGPT* requer acesso autorizado ao repositório para alterar e enviar código. Conversar com o ChatGPT por si só não muda arquivos em outro computador nem publica executáveis.

## Limitações de segurança e escala

O backend atual foi feito como protótipo. Ele usa armazenamento de dados em JSON e arquivos de imagens no disco. Antes de convidar pessoas para uma comunidade aberta, é fundamental implementar bloqueio/denúncia, moderação, backups/restauração, proteção contra ataques, conformidade com privacidade e banco adequado para crescimento. Não é uma solução pronta para internet pública em larga escala.

O app usa `-ldflags` para configurar `main.defaultCentralURL` e `main.defaultUpdateRepo` durante a criação da release. O modo local ainda existe **apenas para desenvolvimento**, caso esses valores não sejam fornecidos. Em produção, o workflow bloqueia a publicação sem `DDD_SERVER_URL` e a chave de assinatura.

As atualizações automáticas do executável são verificadas **no próximo lançamento do DDD**, não imediatamente enquanto o usuário está digitando. Atualizações do servidor são publicadas pelo provedor conforme o repositório for atualizado, após ativar deploy automático no provedor.
