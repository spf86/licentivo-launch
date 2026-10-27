# Test HTTP sui server del cliente e nelle reti private

[Scegli la lingua](http-testing/README.md)

Tutti gli indirizzi dei server, gli IP e i nomi host in questa guida appartengono all'installazione Launch del cliente. I clienti non devono accedere alla rete privata di Licentivo o dello sviluppatore. Sono supportati sia i server di test pubblici sia quelli nella rete privata del cliente.

L'SDK usa il protocollo indicato in `merchant_origin`: `https://` usa HTTPS; `http://` è consentito solo con le autorizzazioni di test descritte sotto. Una richiesta HTTPS non riuscita non passa mai a HTTP. Un interruttore non riscrive gli indirizzi e non installa certificati TLS.

## Due interruttori indipendenti, disattivati per impostazione predefinita

| Interruttore | Dove lo imposta il cliente | Effetto |
| --- | --- | --- |
| **Consenti HTTP (test)** per una singola chiave API | Licentivo → Sviluppo e integrazione → API Key → Crea API Key o Impostazioni di accesso | Consente solo a questa chiave di creare o aggiornare connessioni degli account Launch per origini HTTP del commerciante. Le chiavi di sola lettura non ottengono permessi di scrittura. La rotazione della chiave conserva l'impostazione. |
| **Consenti HTTP all'SDK (solo ambiente di test)** | Launch del cliente → Impostazioni di sistema → Sito e sicurezza → Certificati SSL e protocollo di accesso | Consente a questa installazione Launch di accettare richieste HTTP degli account SDK e generare codice di test HTTP. Lo scambio di identità per l'autorizzazione in esecuzione via HTTP è limitato a `sandbox`; `live` richiede HTTPS. |

Il cliente gestisce entrambi gli interruttori. Non servono un amministratore della piattaforma Licentivo né una modalità di sviluppo globale. Queste impostazioni riguardano solo l'indirizzo Launch del cliente. Launch e l'SDK ufficiale di licenza continuano a chiamare Licentivo tramite HTTPS.

## Configurare un server di test del cliente

1. Aggiorna il server Licentivo, il server Launch e il pacchetto SDK. Gli SDK precedenti che consentono HTTP solo su localhost vanno aggiornati: le impostazioni del server non cambiano la convalida degli URL di un SDK installato.
2. Nello spazio di lavoro Licentivo del cliente crea una chiave di test dedicata con permessi di **lettura e scrittura**, oppure apri le **Impostazioni di accesso** di una chiave attiva e seleziona **Consenti HTTP (test)**.
3. Nelle impostazioni **Sito e sicurezza** del Launch del cliente inserisci un indirizzo raggiungibile dai client di test, per esempio `http://192.168.1.20:18080`, `http://test-server:18080` o il dominio di test del cliente. Abilita **Consenti HTTP all'SDK (solo ambiente di test)** e salva. Usa un'origine senza percorso, query, frammento o barra finale.
4. Ricollega Launch a Licentivo con questa chiave. Leggere prodotti e criteri non basta a stabilire la connessione degli account: verifica anche che la connessione di autorizzazione sia pronta.
5. In **Integrazione applicazione → Verifiche di integrazione e codice**, seleziona **sandbox**, esegui le verifiche e copia il nuovo codice generato. Dopo verifiche riuscite include automaticamente l'opzione di test HTTP. Usa account e acquisti di test.

Non servono un IP pubblico o un record DNS pubblico. I client di test devono risolvere e raggiungere l'indirizzo Launch configurato. `localhost` indica il computer che esegue l'SDK; un altro computer deve usare l'IP del server o il nome host interno. Licentivo registra l'origine e le chiavi di firma fissate, senza richiamare quell'indirizzo né recuperarvi chiavi. Launch e il client di licenza online richiedono comunque accesso in uscita agli endpoint HTTPS di Licentivo. Una rete privata completamente disconnessa non può eseguire l'autorizzazione online degli account.

## Come l'SDK sceglie HTTP o HTTPS

La configurazione pubblica Launch generata per un test abilitato sul server del cliente equivale a:

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

L'indirizzo seleziona il protocollo. `allow_http: true` consente l'origine HTTP del commerciante solo in `sandbox`; non declassa mai un URL HTTPS. La configurazione ufficiale del prodotto di licenza resta HTTPS. Opzioni equivalenti per chiamate dirette nei nove linguaggi:

| SDK | Opzione esplicita per test HTTP |
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

Queste opzioni richiedono un SDK aggiornato. L'SDK non riceve e non richiede mai la chiave API di gestione del commerciante. Launch esporta configurazioni pubbliche e verifica anche sul server il proprio interruttore HTTP: cambiare solo un parametro client non abilita HTTP quando l'interruttore server è spento. I costruttori precedenti mantengono per compatibilità l'eccezione localhost in sandbox, ma non aggirano l'interruttore server disattivato per impostazione predefinita.

## Terminare i test o passare a HTTPS

HTTP non crittografa password, token degli account o altri dati. Anche la rete privata del cliente può essere monitorata o compromessa. Inserire una chiave API nell'amministrazione Launch tramite HTTP espone anche la richiesta dal browser a Launch, sebbene le chiamate da Launch a Licentivo usino HTTPS. Usa credenziali di test dedicate; usa HTTPS in produzione e per operazioni amministrative sensibili.

Dopo aver configurato un listener HTTPS o un proxy TLS funzionante, aggiorna l'origine effettiva del commerciante in Launch, ricollega Licentivo se necessario, poi rigenera e aggiorna la configurazione dei client. Un URL HTTP copiato o compilato non cambia automaticamente. Disattiva entrambi gli interruttori HTTP al termine dei test. Lo scambio di identità SDK in `live` richiede HTTPS a prescindere dagli interruttori.

Disattivando l'interruttore Launch, le successive richieste SDK HTTP sono rifiutate con `SDK_HTTP_DISABLED`; le richieste di identità HTTP in `live` restituiscono `SDK_HTTPS_REQUIRED`. I permessi offline già firmati seguono le normali regole di scadenza e revoca di Licentivo. Disattivare il permesso HTTP della chiave API impedisce a quella chiave di creare o aggiornare connessioni HTTP, ma non elimina connessioni esistenti, revoca acquisti o invalida permessi emessi. Disattiva separatamente una connessione se deve smettere di autorizzare.

## Diagnosticare una connessione non riuscita

Launch mostra la fase non riuscita (lettura di prodotti o criteri, ricerca delle connessioni o creazione di una connessione sandbox/live), il motivo preciso e, se forniti, codice di errore e ID richiesta di Licentivo. Gli errori salvati restano visibili dopo un aggiornamento della pagina. I messaggi non mostrano mai la chiave API di gestione.

| Errore | Azione |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Abilita HTTP per l'esatta chiave API usata da Launch oppure configura un URL HTTPS funzionante del commerciante. |
| `LICENSE_KEY_INVALID` | Controlla la chiave completa, scadenza, revoca e stato del proprietario e dello spazio di lavoro. |
| `LICENSE_KEY_FORBIDDEN` | Usa una chiave di lettura/scrittura di un Owner valido. Il permesso HTTP non aggiunge permessi di scrittura. |
| `SDK_HTTP_DISABLED` | Abilita l'interruttore di test HTTP SDK nel Launch del cliente oppure usa HTTPS. |
| `SDK_HTTPS_REQUIRED` | Usa HTTPS per `live`; i test HTTP devono usare `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Controlla rete in uscita, DNS, firewall e proxy sulla macchina di test del cliente. |
| `LICENTIVO_TLS_ERROR` | Controlla orologio di sistema, certificati attendibili e proxy HTTPS. Mantieni attiva la verifica TLS. |
