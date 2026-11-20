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
