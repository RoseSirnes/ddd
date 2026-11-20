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

## E-mail e recuperação de conta (DDD 1.6)

- Cada conta nova informa **nick, e-mail e senha**. O endereço de e-mail fica **privado**.
- Um código de 16 caracteres é enviado para o e-mail informado. Em **Meu perfil → E-mail e recuperação**, o usuário confirma o código.
- Pessoas com contas antigas podem adicionar seu e-mail pelo mesmo caminho, informando a senha atual.
- **Esqueci minha senha:** solicita um código no e-mail **confirmado** e permite escolher uma senha nova. O código expira após 15 minutos, tem limite de tentativas e invalida sessões anteriores depois de utilizado.
- A confirmação de e-mail expira após 30 minutos. Um endereço pendente não é aceito para redefinir senhas. A verificação ou alteração do e-mail exige a senha atual, mesmo com a conta já conectada.

### Ativar envio de e-mail no Railway (uma única vez)

Para enviar os códigos a usuários reais, use [Resend](https://resend.com). O dono do DDD precisa verificar um domínio de envio na Resend (SPF/DKIM) e criar uma chave de API **Sending access**. Em Railway → projeto DDD → serviço `ddd-chat` → **Variables**, adicione:

- `RESEND_API_KEY`: chave de API, mantida como segredo do Railway; **não** a coloque no GitHub, no código nem no chat.
- `DDD_FROM_EMAIL`: um remetente do domínio verificado, como `DDD <contas@seudominio.com>`.

A implantação automática do Railway reiniciará o servidor com essas variáveis. O endpoint `GET /api/auth/email/status` informa se o envio está configurado. Sem as variáveis, a criação de contas continua funcionando, mas a confirmação e a recuperação por e-mail permanecem indisponíveis. **Não anuncie recuperação ativa até verificar uma entrega real de e-mail.**

Os e-mails são enviados pelo servidor central ao endpoint HTTPS do Resend (`POST /emails`), nunca diretamente pelo executável Windows. Códigos são armazenados somente como hashes, expiram, são de uso único e têm limites de tentativas.
