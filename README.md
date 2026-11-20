# DDD 1.5 — Chat em tempo real para Windows

O DDD inclui contas com nick/senha, mensagens globais, amizade e mensagens privadas, fotos, bio/status e visualização interna de imagens. O cliente Windows é uma janela dedicada via Microsoft Edge; o servidor Go pode ser executado no Railway.

## Estrutura

- `main.go`, `server_connection.go`: aplicação e APIs do DDD. O cliente, com `defaultCentralURL` embutido, encaminha as chamadas ao servidor central por HTTPS.
- `public/`: interface de chat que roda na janela do aplicativo.
- `Dockerfile.central` e `docker-entrypoint.sh`: serviço Linux para Railway, volume persistente `/data`, **somente uma réplica**.
- `.github/workflows/publicar-ddd.yml`: testa, compila para Windows e publica releases assinadas quando `appVersion` muda. Requer variável `DDD_SERVER_URL` e segredo `DDD_SIGNING_KEY` configurados no repositório.

## Desenvolvimento local

`go test ./...`

`go run . -server-only -data-dir ./data`

Compilação Windows (de produção requer URL e repo embutidos via flags de compilação):

`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags='-H=windowsgui -X main.defaultCentralURL=https://SEU-DOMINIO -X main.defaultUpdateRepo=RoseSirnes/ddd' -o DDD.exe .`

## Segurança e limites de beta

Não distribua nem publique chaves privadas; ela **não pertence ao repositório**. O backend usa JSON e arquivos no disco; para teste com poucos usuários, requer volume e backups. A versão ainda não tem denúncia/bloqueio/moderação e não é apropriada para abertura irrestrita ao público. O domínio HTTPS do Railway, o segredo do GitHub Actions e a primeira release são etapas necessárias para a atualização automática funcionar.

## E-mail e recuperação de conta (DDD 1.6.1)

- No cadastro, o usuário informa **nick, e-mail e senha**. O endereço é privado e **não há etapa separada de confirmação do e-mail**.
- Em **Meu perfil → E-mail de recuperação**, usuários antigos podem adicionar ou alterar o endereço informando sua senha atual, sem receber código de verificação.
- **Esqueci minha senha:** exige um código enviado ao endereço cadastrado, com expiração em 15 minutos, limite de tentativas, uso único e invalidação das sessões anteriores.
- Sem um provedor de envio configurado no servidor, cadastro e alteração de e-mail continuam funcionando, mas **a recuperação por e-mail permanece indisponível**.

### Ativar envio de e-mail no Railway (uma única vez)

Para enviar os códigos a usuários reais, use [Resend](https://resend.com). O dono do DDD precisa verificar um domínio de envio na Resend (SPF/DKIM) e criar uma chave de API **Sending access**. Em Railway → projeto DDD → serviço `ddd-chat` → **Variables**, adicione:

- `RESEND_API_KEY`: chave de API, mantida como segredo do Railway; **não** a coloque no GitHub, no código nem no chat.
- `DDD_FROM_EMAIL`: um remetente do domínio verificado, como `DDD <contas@seudominio.com>`.

A implantação automática do Railway reiniciará o servidor com essas variáveis. O endpoint `GET /api/auth/email/status` informa se o envio está configurado. Sem as variáveis, a criação de contas continua funcionando, mas a recuperação por e-mail permanece indisponível. **Não anuncie recuperação ativa até verificar uma entrega real de e-mail.**

Os e-mails são enviados pelo servidor central ao endpoint HTTPS do Resend (`POST /emails`), nunca diretamente pelo executável Windows. Códigos são armazenados somente como hashes, expiram, são de uso único e têm limites de tentativas.
