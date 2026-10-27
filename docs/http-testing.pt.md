# Testes HTTP em servidores do cliente e redes privadas

[Escolher idioma](http-testing/README.md)

Todos os endereços de servidor, IPs e nomes de host deste guia pertencem à implantação Launch do próprio cliente. O cliente não precisa entrar na rede privada da Licentivo nem do desenvolvedor. São aceitos servidores de teste públicos e servidores na rede privada do cliente.

O SDK usa o protocolo de `merchant_origin`: `https://` usa HTTPS; `http://` só é permitido pelas autorizações de teste abaixo. Uma solicitação HTTPS com falha nunca muda automaticamente para HTTP. Um interruptor não reescreve endereços nem instala certificados TLS.

## Dois interruptores independentes, desativados por padrão

| Interruptor | Onde o cliente configura | Efeito |
| --- | --- | --- |
| **Permitir HTTP (testes)** para uma chave API | Licentivo → Desenvolvimento e integração → API Key → Criar API Key ou Configurações de acesso | Permite que esta chave específica crie ou atualize conexões de contas Launch para origens HTTP do comerciante. Chaves somente de leitura não ganham permissão de escrita. A rotação da chave preserva a configuração. |
| **Permitir HTTP no SDK (somente ambiente de teste)** | Launch do cliente → Configurações do sistema → Site e segurança → Certificados SSL e protocolo de acesso | Permite que esta instalação Launch aceite solicitações de contas do SDK por HTTP e gere código de teste HTTP. A troca de identidade para autorização em execução por HTTP é restrita a `sandbox`; `live` exige HTTPS. |

O cliente controla os dois interruptores. Não é necessário um administrador da plataforma Licentivo nem um modo de desenvolvimento global. Essas configurações afetam apenas o endereço Launch do cliente. Launch e o SDK oficial de licenciamento continuam acessando Licentivo por HTTPS.

## Configurar um servidor de teste do cliente

1. Atualize o servidor Licentivo, o servidor Launch e o pacote SDK. SDKs antigos que só permitem HTTP em localhost devem ser atualizados; configurações do servidor não alteram a validação de URL de um SDK instalado.
2. No espaço de trabalho Licentivo do cliente, crie uma chave de teste dedicada com **leitura e escrita**, ou abra as **Configurações de acesso** de uma chave ativa e selecione **Permitir HTTP (testes)**.
3. Em **Site e segurança** do Launch do cliente, informe um endereço acessível aos clientes de teste, como `http://192.168.1.20:18080`, `http://test-server:18080` ou o domínio de teste do cliente. Ative **Permitir HTTP no SDK (somente ambiente de teste)** e salve. Use uma origem sem caminho, consulta, fragmento ou barra final.
4. Reconecte Launch a Licentivo com essa chave. Ler produtos e políticas não basta para estabelecer a conexão de contas; confirme também que a conexão de autorização de contas está pronta.
5. Em **Integração de aplicações → Verificações de integração e código**, selecione **sandbox**, execute as verificações e copie o novo código gerado. Após a validação, a opção de teste HTTP é incluída automaticamente. Use contas e compras de teste.

Não é necessário IP público nem registro DNS público. Os clientes de teste precisam resolver e alcançar o endereço Launch configurado. `localhost` é a máquina que executa o SDK; outra máquina deve usar o IP do servidor ou seu nome de host interno. Licentivo registra a origem e as chaves de assinatura fixadas, sem fazer chamadas de retorno nem buscar chaves nessa origem. Launch e o cliente de licenciamento online ainda precisam de acesso de saída aos endpoints HTTPS da Licentivo. Uma rede privada totalmente desconectada não pode executar autorização de contas online.

## Como o SDK escolhe HTTP ou HTTPS

A configuração pública Launch gerada para um teste habilitado no servidor do cliente equivale a:

```json
{
  "schema_version": 1,
  "merchant_origin": "http://192.168.1.20:18080",
  "launch_product_id": "11111111-1111-4111-8111-111111111111",
  "licentivo_product_id": "22222222-2222-4222-8222-222222222222",
  "environment": "sandbox",
  "allow_http": true
}
```

O endereço escolhe o protocolo. `allow_http: true` permite a origem HTTP do comerciante apenas em `sandbox`; nunca rebaixa uma URL HTTPS. A configuração oficial do produto de licenciamento permanece em HTTPS. Opções equivalentes de chamada direta para as nove linguagens:

| SDK | Opção explícita de teste HTTP |
| --- | --- |
| Go | `launchclient.Config{Environment: "sandbox", AllowHTTP: true}` |
| Python | `Application.connect(..., environment="sandbox", allow_http=True)` |
| JavaScript / TypeScript | `Application.connect(..., {environment: "sandbox", allowHTTP: true})` |
| C# | `Application.Connect(..., "sandbox", allowHttp: true)` |
| Java | `Launch.connect(..., "sandbox", true)` |
| Rust | `Application::connect_with_http(..., "sandbox", true)` |
| Ruby | `Application.connect(..., environment: 'sandbox', allow_http: true)` |
| C | `launch_application_connect_http(..., "sandbox", 1, 0, NULL)` |
| C++ | `Application::ConnectHTTP(..., "sandbox")` |

Essas opções exigem um SDK atualizado. O SDK nunca recebe nem consulta a chave API de gerenciamento do comerciante. Launch exporta a configuração pública e também verifica seu interruptor HTTP no servidor; alterar apenas um parâmetro do cliente não habilita HTTP quando o interruptor do servidor está desligado. Construtores antigos mantêm por compatibilidade a exceção localhost em sandbox, mas ela não ignora o interruptor do servidor, desativado por padrão.

## Encerrar os testes ou migrar para HTTPS

HTTP não criptografa senhas, tokens de conta ou outros dados. A rede privada do cliente também pode ser monitorada ou comprometida. Informar uma chave API na administração Launch por HTTP expõe também a solicitação do navegador para Launch, mesmo que Launch acesse Licentivo por HTTPS. Use credenciais de teste dedicadas; use HTTPS em produção e em operações administrativas sensíveis.

Após configurar um listener HTTPS ou proxy TLS funcional, atualize a origem efetiva do comerciante em Launch, reconecte Licentivo se necessário e gere novamente e atualize a configuração de integração dos clientes. Uma URL HTTP copiada ou compilada não muda automaticamente. Desative os dois interruptores HTTP ao terminar os testes. A troca de identidade SDK em `live` exige HTTPS, independentemente dos interruptores.

Desativar o interruptor Launch rejeita as próximas solicitações HTTP do SDK com `SDK_HTTP_DISABLED`; solicitações de identidade HTTP em `live` retornam `SDK_HTTPS_REQUIRED`. Permissões offline já assinadas seguem as regras normais de expiração e revogação da Licentivo. Desativar o HTTP da chave API bloqueia novas conexões HTTP ou atualizações com essa chave; não exclui conexões existentes, revoga compras nem invalida permissões emitidas. Desative a conexão separadamente se ela precisar parar de autorizar.

## Diagnosticar uma falha de conexão

Launch mostra a etapa com falha (leitura de produtos ou políticas, consulta de conexões ou criação de uma conexão sandbox/live), o motivo específico e, quando fornecidos, o código de erro e o ID da solicitação Licentivo. Falhas salvas continuam visíveis após atualizar a página. As mensagens nunca exibem a chave API de gerenciamento.

| Erro | Ação |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Habilite HTTP para a chave API exata usada por Launch ou configure uma URL HTTPS funcional do comerciante. |
| `LICENSE_KEY_INVALID` | Confira a chave completa, validade, revogação e estado do proprietário e do espaço de trabalho. |
| `LICENSE_KEY_FORBIDDEN` | Use uma chave de leitura/escrita de um Owner válido. A permissão HTTP não adiciona permissão de escrita. |
| `SDK_HTTP_DISABLED` | Ative o interruptor de teste HTTP do SDK no Launch do próprio cliente ou use HTTPS. |
| `SDK_HTTPS_REQUIRED` | Use HTTPS para `live`; os testes HTTP devem usar `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Verifique rede de saída, DNS, firewall e proxy da máquina de teste do cliente. |
| `LICENTIVO_TLS_ERROR` | Verifique relógio do sistema, certificados confiáveis e proxy HTTPS. Mantenha a verificação TLS ativada. |
