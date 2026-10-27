# HTTP-Tests auf Kundenservern und in privaten Netzwerken

[Sprache wählen](http-testing/README.md)

Alle Serveradressen, IP-Adressen und Hostnamen in dieser Anleitung gehören zur eigenen Launch-Installation des Kunden. Kunden müssen keinem privaten Netzwerk von Licentivo oder des Entwicklers beitreten. Öffentliche Testserver und Server im privaten Kundennetzwerk werden unterstützt.

Das SDK verwendet das Protokoll in `merchant_origin`: `https://` verwendet HTTPS; `http://` ist nur mit den unten beschriebenen Testberechtigungen zulässig. Fehlgeschlagene HTTPS-Anfragen werden niemals auf HTTP zurückgestuft. Ein Schalter schreibt keine Adressen um und installiert kein TLS-Zertifikat.

## Zwei unabhängige, standardmäßig deaktivierte Schalter

| Schalter | Einstellung durch den Kunden | Wirkung |
| --- | --- | --- |
| **HTTP erlauben (Tests)** für einen API-Schlüssel | Licentivo → Entwicklung und Integration → API Key → API Key erstellen oder Zugriffseinstellungen | Erlaubt genau diesem Schlüssel, Launch-Kontoverbindungen für HTTP-Händleradressen zu erstellen oder zu aktualisieren. Schreibgeschützte Schlüssel erhalten keine Schreibrechte. Die Einstellung bleibt beim Rotieren des Schlüssels erhalten. |
| **SDK-Zugriff über HTTP erlauben (nur Testumgebung)** | Launch des Kunden → Systemeinstellungen → Website und Sicherheit → SSL-Zertifikate und Zugriffsprotokoll | Erlaubt dieser Launch-Installation HTTP-Kontoanfragen vom SDK und die Erzeugung von HTTP-Testcode. Der Austausch einer Identitätsbestätigung für die Laufzeit ist über HTTP nur in `sandbox` zulässig; `live` erfordert HTTPS. |

Kunden verwalten beide Schalter selbst. Weder ein Licentivo-Plattformadministrator noch ein plattformweiter Entwicklungsmodus ist erforderlich. Die Einstellungen betreffen nur die Launch-Adresse des Kunden. Launch und das offizielle Lizenzierungs-SDK rufen Licentivo weiterhin über HTTPS auf.

## Einen Testserver beim Kunden einrichten

1. Aktualisieren Sie den Licentivo-Server, den Launch-Server und das SDK-Paket. Ältere SDKs, die HTTP nur für localhost zulassen, müssen aktualisiert werden; Servereinstellungen ändern die URL-Prüfung eines installierten SDKs nicht.
2. Erstellen Sie im Licentivo-Arbeitsbereich des Kunden einen eigenen Testschlüssel mit **Lese- und Schreibrechten**, oder öffnen Sie die **Zugriffseinstellungen** eines aktiven Schlüssels und wählen Sie **HTTP erlauben (Tests)**.
3. Tragen Sie in den Launch-Einstellungen **Website und Sicherheit** des Kunden eine für Testclients erreichbare Adresse ein, etwa `http://192.168.1.20:18080`, `http://test-server:18080` oder die Testdomain des Kunden. Aktivieren Sie **SDK-Zugriff über HTTP erlauben (nur Testumgebung)** und speichern Sie. Verwenden Sie einen Origin ohne Pfad, Abfrage, Fragment oder abschließenden Schrägstrich.
4. Verbinden Sie Launch mit diesem Schlüssel erneut mit Licentivo. Das Lesen von Produkten und Richtlinien allein stellt keine Kontoverbindung her; prüfen Sie auch, dass die Kontoautorisierungsverbindung bereit ist.
5. Wählen Sie unter **Anwendungsintegration → Integrationsprüfung und Code** die Umgebung **sandbox**, führen Sie die Prüfungen aus und kopieren Sie den neu erzeugten Code. Nach erfolgreicher Prüfung enthält er die HTTP-Testoption automatisch. Verwenden Sie Testkonten und Testkäufe.

Eine öffentliche IP-Adresse oder ein öffentlicher DNS-Eintrag ist nicht erforderlich. Testclients müssen die konfigurierte Launch-Adresse auflösen und erreichen können. `localhost` bezeichnet den Rechner, auf dem das SDK läuft; andere Rechner müssen die Server-IP oder den internen Hostnamen verwenden. Licentivo speichert den Origin und die fest hinterlegten Signaturprüfschlüssel, ohne die Adresse zurückzurufen oder Schlüssel von ihr abzurufen. Launch und der Online-Lizenzierungsclient benötigen weiterhin ausgehenden Zugriff auf die HTTPS-Endpunkte von Licentivo. In einem vollständig abgeschotteten privaten Netzwerk ist keine Online-Kontolizenzierung möglich.

## Wie das SDK HTTP oder HTTPS auswählt

Die erzeugte öffentliche Launch-Konfiguration für einen aktivierten Test auf einem Kundenserver entspricht:

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

Die Adresse bestimmt das Protokoll. `allow_http: true` erlaubt die HTTP-Händleradresse nur in `sandbox`; eine HTTPS-URL wird niemals herabgestuft. Die offizielle Lizenzproduktkonfiguration bleibt bei HTTPS. Gleichwertige Optionen für direkte Aufrufe in allen neun Sprachen:

| SDK | Explizite HTTP-Testoption |
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

Diese Optionen erfordern eine aktualisierte SDK-Version. Das SDK erhält oder erfragt niemals den Verwaltungs-API-Schlüssel des Händlers. Launch exportiert öffentliche Konfiguration und prüft seinen HTTP-Schalter auch auf dem Server. Ein Clientflag allein kann HTTP daher nicht aktivieren, wenn der Serverschalter deaktiviert ist. Bestehende Konstruktoren behalten aus Kompatibilitätsgründen die bisherige localhost-Ausnahme in sandbox; diese umgeht den standardmäßig deaktivierten Serverschalter nicht.

## Tests beenden oder auf HTTPS umstellen

HTTP verschlüsselt weder Passwörter noch Kontotoken oder andere Daten. Auch das private Kundennetzwerk kann überwacht oder kompromittiert werden. Die Eingabe eines API-Schlüssels in der Launch-Verwaltung über HTTP legt auch die Browser-zu-Launch-Anfrage offen, selbst wenn Launch Licentivo über HTTPS aufruft. Verwenden Sie eigene Testzugangsdaten; nutzen Sie HTTPS für den Produktivbetrieb und sensible Verwaltungsaufgaben.

Konfigurieren Sie einen funktionierenden HTTPS-Listener oder TLS-Proxy, aktualisieren Sie den wirksamen Händler-Origin in Launch und verbinden Sie Licentivo bei Bedarf erneut. Erzeugen und aktualisieren Sie anschließend die Integrationskonfiguration der Clients. Eine kopierte oder einkompilierte HTTP-URL ändert sich nicht automatisch. Deaktivieren Sie nach den Tests beide HTTP-Schalter. Der SDK-Identitätsaustausch in `live` erfordert unabhängig von beiden Schaltern HTTPS.

Nach dem Deaktivieren des Launch-Schalters werden weitere HTTP-SDK-Anfragen mit `SDK_HTTP_DISABLED` abgelehnt; HTTP-Identitätsanfragen in `live` liefern `SDK_HTTPS_REQUIRED`. Bereits signierte Offlineberechtigungen unterliegen weiterhin den normalen Ablauf- und Widerrufsregeln von Licentivo. Das Deaktivieren der HTTP-Berechtigung eines API-Schlüssels verhindert nur neue oder aktualisierte HTTP-Verbindungen mit diesem Schlüssel; bestehende Verbindungen, Käufe und erteilte Berechtigungen werden nicht gelöscht oder widerrufen. Deaktivieren Sie eine Verbindung gesondert, wenn sie keine Autorisierungen mehr erteilen soll.

## Fehlgeschlagene Verbindungen untersuchen

Launch zeigt die fehlgeschlagene Phase (Lesen von Produkten oder Richtlinien, Abfragen von Verbindungen oder Erstellen einer sandbox/live-Verbindung), den konkreten Grund sowie gegebenenfalls Fehlercode und Anfrage-ID von Licentivo. Gespeicherte Fehler bleiben nach dem Neuladen sichtbar. Fehlermeldungen zeigen niemals den Verwaltungs-API-Schlüssel.

| Fehler | Maßnahme |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Aktivieren Sie HTTP für genau den von Launch verwendeten API-Schlüssel oder konfigurieren Sie eine funktionierende HTTPS-Händleradresse. |
| `LICENSE_KEY_INVALID` | Prüfen Sie den vollständigen Schlüssel, Ablauf, Widerruf sowie den Status des Eigentümers und Arbeitsbereichs. |
| `LICENSE_KEY_FORBIDDEN` | Verwenden Sie einen Lese-/Schreibschlüssel eines gültigen Owners. Die HTTP-Berechtigung gewährt keine Schreibrechte. |
| `SDK_HTTP_DISABLED` | Aktivieren Sie den SDK-HTTP-Testschalter in der eigenen Launch-Installation des Kunden oder verwenden Sie HTTPS. |
| `SDK_HTTPS_REQUIRED` | Verwenden Sie HTTPS für `live`; HTTP-Tests müssen `sandbox` verwenden. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Prüfen Sie ausgehende Netzwerkverbindungen, DNS, Firewall und Proxy auf dem Testrechner des Kunden. |
| `LICENTIVO_TLS_ERROR` | Prüfen Sie Systemzeit, vertrauenswürdige Zertifikate und HTTPS-Proxy. Lassen Sie die TLS-Prüfung aktiviert. |
