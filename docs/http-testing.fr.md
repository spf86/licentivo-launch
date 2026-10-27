# Tests HTTP sur les serveurs du client et les réseaux privés

[Choisir une langue](http-testing/README.md)

Toutes les adresses de serveur, IP et noms d'hôte de ce guide appartiennent au déploiement Launch du client. Le client ne rejoint pas le réseau privé de Licentivo ni celui du développeur. Les serveurs de test publics et ceux du réseau privé du client sont pris en charge.

Le SDK utilise le protocole de `merchant_origin` : `https://` utilise HTTPS ; `http://` n'est autorisé qu'avec les permissions de test ci-dessous. Une requête HTTPS en échec ne se rabat jamais sur HTTP. Un interrupteur ne réécrit pas les adresses et n'installe aucun certificat TLS.

## Deux interrupteurs indépendants, désactivés par défaut

| Interrupteur | Où le client le règle | Effet |
| --- | --- | --- |
| **Autoriser HTTP (tests)** pour une clé API | Licentivo → Développement et intégration → API Key → Créer une clé API ou Paramètres d'accès | Autorise cette clé précise à créer ou modifier les connexions de comptes Launch pour des origines marchand HTTP. Les clés en lecture seule n'obtiennent aucun droit d'écriture. La rotation de la clé conserve ce réglage. |
| **Autoriser HTTP pour le SDK (environnement de test uniquement)** | Launch du client → Paramètres système → Site et sécurité → Certificats SSL et protocole d'accès | Autorise cette installation Launch à accepter les requêtes de compte SDK en HTTP et à générer le code de test HTTP. L'échange d'identité pour l'autorisation d'exécution en HTTP est limité à `sandbox` ; `live` exige HTTPS. |

Le client gère lui-même les deux interrupteurs. Aucun administrateur de plateforme Licentivo ni mode développement global n'est nécessaire. Ces réglages concernent uniquement l'adresse Launch du client. Launch et le SDK officiel de licence continuent d'appeler Licentivo en HTTPS.

## Configurer un serveur de test du client

1. Mettez à jour le serveur Licentivo, le serveur Launch et le paquet SDK. Les anciens SDK n'autorisant HTTP que sur localhost doivent être mis à jour ; les réglages serveur ne changent pas la validation d'URL d'un SDK déjà installé.
2. Dans l'espace de travail Licentivo du client, créez une clé de test dédiée en **lecture/écriture**, ou ouvrez les **Paramètres d'accès** d'une clé active et sélectionnez **Autoriser HTTP (tests)**.
3. Dans les paramètres **Site et sécurité** du Launch du client, saisissez une adresse accessible aux clients de test, par exemple `http://192.168.1.20:18080`, `http://test-server:18080` ou le domaine de test du client. Activez **Autoriser HTTP pour le SDK (environnement de test uniquement)** et enregistrez. L'origine ne doit contenir ni chemin, ni requête, ni fragment, ni barre oblique finale.
4. Reconnectez Launch à Licentivo avec cette clé. Lire les produits et les politiques ne suffit pas à établir la connexion de comptes ; vérifiez aussi que la connexion d'autorisation des comptes est prête.
5. Dans **Intégration de l'application → Vérifications d'intégration et code**, sélectionnez **sandbox**, lancez les vérifications et copiez le nouveau code généré. Après validation, l'option de test HTTP y est ajoutée automatiquement. Utilisez des comptes et des achats de test.

Aucune IP publique ni entrée DNS publique n'est requise. Les clients de test doivent pouvoir résoudre et atteindre l'adresse Launch configurée. `localhost` désigne la machine exécutant le SDK ; une autre machine doit utiliser l'IP du serveur ou son nom d'hôte interne. Licentivo enregistre l'origine et les clés de signature épinglées, sans rappeler cette origine ni y récupérer des clés. Launch et le client de licence en ligne doivent toujours pouvoir accéder en sortie aux points de terminaison HTTPS de Licentivo. Un réseau privé totalement déconnecté ne permet pas la licence de comptes en ligne.

## Comment le SDK choisit HTTP ou HTTPS

La configuration publique Launch générée pour un test activé sur le serveur du client équivaut à :

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

L'adresse détermine le protocole. `allow_http: true` autorise l'origine marchand HTTP uniquement dans `sandbox` ; une URL HTTPS n'est jamais rétrogradée. La configuration officielle du produit de licence reste en HTTPS. Options équivalentes d'appel direct pour les neuf langages :

| SDK | Option explicite de test HTTP |
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

Ces options exigent une version à jour du SDK. Le SDK ne reçoit et ne demande jamais la clé API de gestion du marchand. Launch exporte une configuration publique et vérifie également son interrupteur HTTP côté serveur : modifier un paramètre client seul ne peut donc pas activer HTTP lorsque cet interrupteur est désactivé. Les anciens constructeurs conservent l'exception localhost en sandbox pour compatibilité ; elle ne contourne pas l'interrupteur serveur désactivé par défaut.

## Terminer les tests ou passer à HTTPS

HTTP ne chiffre ni les mots de passe, ni les jetons de compte, ni les autres données. Le réseau privé du client peut lui aussi être surveillé ou compromis. Saisir une clé API dans l'administration Launch en HTTP expose également la requête du navigateur vers Launch, même si Launch appelle Licentivo en HTTPS. Utilisez des identifiants de test dédiés ; utilisez HTTPS en production et pour l'administration sensible.

Après avoir configuré un écouteur HTTPS ou un proxy TLS fonctionnel, mettez à jour l'origine marchand effective de Launch, reconnectez Licentivo si nécessaire, puis régénérez et mettez à jour la configuration des clients. Une URL HTTP copiée ou compilée ne change pas automatiquement. Désactivez les deux interrupteurs HTTP à la fin des tests. L'échange d'identité SDK en `live` exige HTTPS quels que soient ces réglages.

Désactiver l'interrupteur Launch rejette les requêtes SDK HTTP suivantes avec `SDK_HTTP_DISABLED` ; les requêtes d'identité HTTP en `live` renvoient `SDK_HTTPS_REQUIRED`. Les autorisations hors ligne déjà signées suivent toujours les règles normales d'expiration et de révocation de Licentivo. Retirer l'autorisation HTTP de la clé API bloque la création ou la modification ultérieure de connexions HTTP avec cette clé ; cela ne supprime pas les connexions existantes, ne révoque pas les achats et n'invalide pas les autorisations émises. Désactivez séparément une connexion si elle doit cesser d'autoriser.

## Diagnostiquer un échec de connexion

Launch affiche l'étape en échec (lecture des produits ou politiques, recherche des connexions ou création d'une connexion sandbox/live), sa cause précise et, lorsqu'ils sont fournis, le code d'erreur et l'identifiant de requête Licentivo. Les erreurs enregistrées restent visibles après actualisation. Les messages ne montrent jamais la clé API de gestion.

| Erreur | Action |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Autorisez HTTP pour la clé API exacte utilisée par Launch, ou configurez une URL marchand HTTPS fonctionnelle. |
| `LICENSE_KEY_INVALID` | Vérifiez la clé complète, son expiration, sa révocation et l'état de son propriétaire et de l'espace de travail. |
| `LICENSE_KEY_FORBIDDEN` | Utilisez la clé en lecture/écriture d'un Owner valide. L'autorisation HTTP n'ajoute aucun droit d'écriture. |
| `SDK_HTTP_DISABLED` | Activez l'interrupteur de test HTTP du SDK dans le Launch du client, ou utilisez HTTPS. |
| `SDK_HTTPS_REQUIRED` | Utilisez HTTPS pour `live` ; les tests HTTP doivent utiliser `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Vérifiez le réseau sortant, le DNS, le pare-feu et le proxy sur la machine de test du client. |
| `LICENTIVO_TLS_ERROR` | Vérifiez l'heure système, les certificats de confiance et le proxy HTTPS. Conservez la vérification TLS activée. |
