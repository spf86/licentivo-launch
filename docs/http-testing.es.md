# Pruebas HTTP en servidores del cliente y redes privadas

[Elegir idioma](http-testing/README.md)

Todas las direcciones de servidor, IP y nombres de host de esta guía pertenecen al despliegue Launch del cliente. El cliente no tiene que entrar en la red privada de Licentivo ni en la del desarrollador. Se admiten tanto servidores de pruebas públicos como servidores de la red privada del cliente.

El SDK usa el protocolo de `merchant_origin`: `https://` usa HTTPS; `http://` solo se permite con los permisos de prueba indicados abajo. Una solicitud HTTPS fallida nunca cambia automáticamente a HTTP. Un interruptor no reescribe direcciones ni instala certificados TLS.

## Dos interruptores independientes, desactivados por defecto

| Interruptor | Dónde lo configura el cliente | Efecto |
| --- | --- | --- |
| **Permitir HTTP (pruebas)** para una clave API | Licentivo → Desarrollo e integración → API Key → Crear API Key o Ajustes de acceso | Permite que esta clave concreta cree o actualice conexiones de cuentas Launch para orígenes HTTP del comerciante. Las claves de solo lectura no obtienen permisos de escritura. La rotación de la clave conserva el ajuste. |
| **Permitir HTTP al SDK (solo entorno de pruebas)** | Launch del cliente → Ajustes del sistema → Sitio y seguridad → Certificados SSL y protocolo de acceso | Permite que esta instalación Launch acepte solicitudes de cuentas del SDK por HTTP y genere código de prueba HTTP. El intercambio de identidad para la autorización en ejecución por HTTP se limita a `sandbox`; `live` exige HTTPS. |

El cliente controla ambos interruptores. No hace falta un administrador de la plataforma Licentivo ni un modo de desarrollo global. Estos ajustes solo afectan a la dirección Launch del cliente. Launch y el SDK oficial de licencias siguen llamando a Licentivo mediante HTTPS.

## Configurar un servidor de pruebas del cliente

1. Actualiza el servidor Licentivo, el servidor Launch y el paquete SDK. Los SDK antiguos que solo permiten HTTP en localhost deben actualizarse; los ajustes del servidor no cambian la validación de URL de un SDK instalado.
2. En el espacio de trabajo Licentivo del cliente, crea una clave de pruebas dedicada de **lectura y escritura**, o abre los **Ajustes de acceso** de una clave activa y selecciona **Permitir HTTP (pruebas)**.
3. En **Sitio y seguridad** del Launch del cliente, introduce una dirección accesible para los clientes de prueba, como `http://192.168.1.20:18080`, `http://test-server:18080` o el dominio de pruebas del cliente. Activa **Permitir HTTP al SDK (solo entorno de pruebas)** y guarda. Usa un origen sin ruta, consulta, fragmento ni barra final.
4. Vuelve a conectar Launch con Licentivo mediante esa clave. Leer productos y políticas no basta para establecer la conexión de cuentas; confirma también que la conexión de autorización de cuentas esté lista.
5. En **Integración de aplicaciones → Comprobaciones de integración y código**, selecciona **sandbox**, ejecuta las comprobaciones y copia el código recién generado. Tras superarlas, incluye automáticamente la opción de prueba HTTP. Usa cuentas y compras de prueba.

No se necesita una IP pública ni un registro DNS público. Los clientes de prueba deben resolver y alcanzar la dirección Launch configurada. `localhost` es la máquina que ejecuta el SDK; otra máquina debe usar la IP del servidor o su nombre de host interno. Licentivo registra el origen y las claves de firma fijadas, sin realizar llamadas de retorno ni recuperar claves de ese origen. Launch y el cliente de licencias en línea siguen necesitando acceso saliente a los endpoints HTTPS de Licentivo. Una red privada totalmente desconectada no puede realizar autorización de cuentas en línea.

## Cómo elige el SDK HTTP o HTTPS

La configuración pública Launch generada para una prueba habilitada en el servidor del cliente equivale a:

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

La dirección elige el protocolo. `allow_http: true` permite el origen HTTP del comerciante solo en `sandbox`; nunca rebaja una URL HTTPS. La configuración oficial del producto de licencias permanece en HTTPS. Opciones equivalentes de llamada directa para los nueve lenguajes:

| SDK | Opción explícita de prueba HTTP |
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

Estas opciones requieren un SDK actualizado. El SDK nunca recibe ni consulta la clave API de administración del comerciante. Launch exporta configuración pública y también comprueba su interruptor HTTP en el servidor; cambiar solo un parámetro del cliente no permite HTTP si el interruptor del servidor está apagado. Los constructores antiguos conservan por compatibilidad la excepción localhost de sandbox, pero no evitan el interruptor del servidor, desactivado por defecto.

## Finalizar las pruebas o migrar a HTTPS

HTTP no cifra contraseñas, tokens de cuenta ni otros datos. La red privada del cliente también puede ser vigilada o comprometida. Introducir una clave API en la administración Launch por HTTP expone igualmente la solicitud del navegador a Launch, aunque Launch llame a Licentivo por HTTPS. Usa credenciales de prueba dedicadas; usa HTTPS en producción y para tareas administrativas sensibles.

Tras configurar un listener HTTPS o proxy TLS que funcione, actualiza el origen efectivo del comerciante en Launch, reconecta Licentivo cuando sea necesario y regenera y actualiza la configuración de integración de los clientes. Una URL HTTP copiada o compilada no cambia automáticamente. Desactiva ambos interruptores HTTP al terminar las pruebas. El intercambio de identidad SDK en `live` exige HTTPS independientemente de los interruptores.

Desactivar el interruptor Launch rechaza las solicitudes HTTP posteriores del SDK con `SDK_HTTP_DISABLED`; las solicitudes de identidad HTTP en `live` devuelven `SDK_HTTPS_REQUIRED`. Los permisos sin conexión ya firmados siguen las reglas normales de caducidad y revocación de Licentivo. Desactivar el permiso HTTP de la clave API impide crear o actualizar conexiones HTTP con esa clave, pero no elimina conexiones existentes, revoca compras ni invalida permisos emitidos. Desactiva por separado una conexión si debe dejar de autorizar.

## Diagnosticar una conexión fallida

Launch muestra la fase fallida (lectura de productos o políticas, consulta de conexiones o creación de una conexión sandbox/live), el motivo específico y, cuando se proporcionan, el código de error y el ID de solicitud de Licentivo. Los fallos guardados siguen visibles al actualizar la página. Los mensajes nunca muestran la clave API de administración.

| Error | Acción |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Permite HTTP para la clave API exacta usada por Launch o configura una URL HTTPS válida del comerciante. |
| `LICENSE_KEY_INVALID` | Comprueba la clave completa, caducidad, revocación y estado de su propietario y espacio de trabajo. |
| `LICENSE_KEY_FORBIDDEN` | Usa una clave de lectura/escritura de un Owner válido. El permiso HTTP no añade permisos de escritura. |
| `SDK_HTTP_DISABLED` | Activa el interruptor de pruebas HTTP del SDK en el Launch del cliente o usa HTTPS. |
| `SDK_HTTPS_REQUIRED` | Usa HTTPS para `live`; las pruebas HTTP deben usar `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Comprueba la red saliente, DNS, cortafuegos y proxy de la máquina de pruebas del cliente. |
| `LICENTIVO_TLS_ERROR` | Comprueba el reloj del sistema, los certificados de confianza y el proxy HTTPS. Mantén activada la verificación TLS. |
