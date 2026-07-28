# 📡 TowerCoverage Scraper & OmniService – Go + Playwright + MCP

Este proyecto es un microservicio híbrido desarrollado en **Golang**. Actúa como un motor de extracción automatizada (Scraper) y un puente de comunicación dual: expone una **API REST** tradicional y un servidor **MCP (Model Context Protocol)** para interactuar de forma nativa con Agentes de Inteligencia Artificial (IA).

✅ Extracción *Headless* de mapas dinámicos usando Playwright para Go.

✅ Implementación dual del Model Context Protocol (MCP) vía `stdio` (Local) y `sse` (Remoto).

✅ Endpoint REST tradicional (`POST /api/coverage`) para integraciones estándar.

✅ Filtro algorítmico estricto: Solo aprueba torres a **<= 6 millas** con estado **"Good Link"**.

✅ Extracción estructurada mediante expresiones regulares (RegEx) y parsing del DOM HTML.


El sistema permite automatizar la consulta de cobertura en TowerCoverage.com inyectando latitud y longitud, parseando las tarjetas de resultados interactivos en el mapa, evaluando la viabilidad del enlace y devolviendo un JSON estructurado con coordenadas, alineación y *tilt* para el equipo de instalación.

---

# 📑 Índice

   🔎 Descripción General

   📁 Estructura del Proyecto

   🏗 Arquitectura del Sistema

   ⚙️ Configuración del Entorno

   🧠 Modos de Operación (IA vs REST)

   🚀 Ejecución y Despliegue

   🤖 Integración con Agente de IA (n8n + MCP)

   🧩 Diseño del Sistema y Filtros

   💾 Modelo de Datos (Payloads)

   🧪 Pruebas y Verificación

   🚨 Troubleshooting

---

# 🔎 Descripción General

Este servicio elimina la necesidad de que un operador humano inicie sesión en TowerCoverage, introduzca coordenadas, espere a que cargue el mapa, haga clic pin por pin en cada torre y calcule visualmente las distancias.

Actúa como un agente invisible que orquesta un navegador real mediante Playwright. Al recibir coordenadas (ya sea desde una petición HTTP o desde un LLM mediante MCP), el bot navega a la vista de *Path Analysis Result*, espera a que los scripts de mapeo se inicialicen, extrae el HTML crudo de cada enlace (Link Result), y aplica las reglas de negocio del NOC. Finalmente, guarda un histórico en MySQL y devuelve la data limpia.

---

# 📂 Estructura del Proyecto

```text
tower-scraper/
├── cmd/
│   └── scraper/
│       └── main.go              → Bootstrap, enrutamiento HTTP (REST/SSE), Server MCP y orquestador
├── internal/
│   ├── config/                  → Parseo de variables de entorno (.env)
│   ├── models/                  → Estructuras de datos principales (TowerCoverage)
│   ├── scraper/                 → Lógica de Playwright, Login, navegación y selectores HTML
├── .env.template                → Plantilla de variables de entorno
├── go.mod
└── go.sum
```

---

# 🏗 Arquitectura del Sistema

El servicio opera en un flujo unidireccional pero atiende a múltiples clientes:

```plaintext
A[Agente IA en n8n/Claude]    B[Aplicación Frontend / Postman]
       | (vía MCP)                    | (vía API REST)
       v                              v
C[Servidor Go: Rutas /sse, /message o /api/coverage]
       |
       v
D[Scraper Engine (Playwright)] --> Login en TowerCoverage
       |
       v
E[Navegación Dinámica al Mapa + Inyección Lat/Lon]
       |
       v
F[Extracción DOM HTML + Limpieza RegEx]
       |
       v
G[Filtro de Negocio: <= 6 millas & "Good Link"]
       |
       |
       v
I[Respuesta JSON al Cliente (A o B)]
```

---
# ⚙️ Configuración y Despliegue

 💻 Aplicación y Transporte MCP

APP_PORT=8080

MCP_TRANSPORT=sse  (Valores admitidos: stdio para uso local CLI, sse para levantar servidor HTTP).

MCP_API_KEY=Token para clientes MCP en modo SSE (header: Authorization: Bearer <valor>).

--------------------------------------------------------
### 🔐 CREDENCIALES TOWER COVERAGE
--------------------------------------------------------

TOWER_USERNAME=correo@ejemplo.com

TOWER_PASSWORD=password_seguro

----------------------------------------------

---

# 🧠 Modos de Operación (IA vs REST)

El motor principal (main.go) es "bilingüe". Dependiendo de la variable MCP_TRANSPORT, el servicio se comporta de diferentes formas:

### 1. Modo Remoto (REST + MCP/SSE)

Si MCP_TRANSPORT=sse, la aplicación levanta un servidor web en el puerto definido en APP_PORT exponiendo tres endpoints:

    GET /sse: Conexión de Eventos de Servidor a Servidor para clientes MCP.

    POST /message: Recepción de comandos JSON-RPC del protocolo MCP.

    POST /api/coverage: Endpoint REST tradicional para integraciones que no son de IA.

### 2. Modo Local (MCP/Stdio)

Si MCP_TRANSPORT=stdio (o vacío), el servicio NO abre puertos de red. En su lugar, se comunica directamente a través de la entrada y salida estándar del sistema operativo. Esto es ideal para Agentes locales (como la app Desktop de Claude) o para debuggear con el MCP Inspector.

---

# 🐳 Ejecución con Docker

El despliegue está contenerizado mediante un Multi-stage build para garantizar que el entorno de producción no contenga el código fuente, sino únicamente un binario compilado estáticamente corriendo sobre un SO Alpine ultra-ligero.

```Bash
docker-compose up -d --build
```

El proceso:

    Inicia la etapa builder con Golang 1.22, descarga dependencias y compila.

    Inicia la etapa de producción transfiriendo solo el binario a una imagen Alpine.

    El archivo commands.yaml se monta como un volumen read-only (ro). Esto permite modificar o agregar comandos al vuelo sin tener que reconstruir la imagen Docker.

Para ver registros en tiempo real:

```Bash
docker compose logs -f
```

---

# 🤖 Integración con Agente de IA (n8n + MCP)

Este proyecto está diseñado para exponer herramientas de contexto (MCP) que son consumidas por un agente de Inteligencia Artificial implementado en **n8n**. El agente automatiza la verificación de factibilidad técnica y cobertura residencial, procesando directamente los tickets de soporte.

### Flujo de Trabajo del Agente

El agente ejecuta un ciclo continuo de validación y respuesta estructurado en las siguientes fases:

#### 1. Escucha y Extracción de Tickets (Ubersmith)
- **Activación:** Se ejecuta mediante un *Schedule Trigger* en intervalos programados regulares.
- **Consulta:** Utiliza la herramienta `MCP_ubersmith` para solicitar a la API los tickets que requieren atención (`method=support.ticket_list`, `queue=32`, `type=Open`).
- **Contexto:** Extrae el `ticket_id` y obtiene el historial de comentarios de cada ticket para ubicar la solicitud de verificación de coordenadas. Mantiene un registro interno para no procesar el mismo ticket dos veces.

#### 2. Evaluación de Coordenadas
- Identifica la latitud y longitud dentro del texto del ticket.
- **Prioridad de Relocalización:** Si el ticket reporta una mudanza y contiene tanto coordenadas "Hacia" (nuevo destino) como "Actual", el agente da prioridad estricta a analizar la coordenada "Hacia".

#### 3. Ejecución del Scraper de Coberturas
- El agente envía las coordenadas definitivas a la herramienta `MCP_coberturas`.
- Recibe un JSON que contiene: estado general de la cobertura, listado de Access Points (AP) cercanos, recuento de clientes conectados por equipo y errores de lectura (si los hay).

#### 4. Selección Óptima de Access Points
El agente procesa la respuesta del scraper y selecciona los mejores APs bajo dos criterios estrictos:
1. **Menor distancia** geométrica al cliente.
2. **Menor saturación** (menor volumen de clientes conectados actualmente en ese sector).

#### 5. Matriz de Compatibilidad (CPE y Planes)
Una vez definidos los mejores APs, el agente asigna el equipo cliente (CPE) necesario y los perfiles de velocidad factibles, aplicando la siguiente regla de negocio:

| Familia AP | Modelo de AP | Equipo Cliente (CPE) Sugerido | Planes Soportados (Mbps) |
| :--- | :--- | :--- | :--- |
| **Serie 4600** | ePMP4600L, ePMP4600 | Force4625 | 200/50 |
| **Serie 4500** | ePMP4500L, ePMP4500 | Force4525L, Force300 | 100/20, 50/10, 25/5 |
| **Serie 3000** | ePMP3000, ePMP3000L | Force300 | 100/20, 50/10, 25/5 |
| **Serie 2000** | ePMP2000 | Force300 | 50/10, 25/5 |
| **Ubiquiti** | Cualquier AP AC | LiteBeam AC | 50/10, 25/5 |

#### 6. Respuesta y Manejo de Alertas
El agente consolida la información y responde automáticamente en el ticket de Ubersmith con el siguiente formato:
- Resumen claro de la ubicación analizada (Hacia o Actual).
- Un "Top 3" de las mejores antenas factibles, detallando por cada una: Nombre del AP, Distancia, Clientes actuales, CPE recomendado y Planes aplicables.
- ⚠️ **Manejo de Errores de Lectura:** Si el `MCP_coberturas` logra identificar una antena óptima pero carece de datos complementarios (ej. *falta azimut* o *no se pudo contar clientes*), el agente incluye la antena en la respuesta, pero le añade una nota explícita: **"Requiere verificación manual de capacidad/azimut por falta de datos en el sistema"**.


El agente ejecuta un ciclo continuo de validación y respuesta estructurado en las siguientes fases...

![Diagrama de flujo del Agente n8n](docs/flujo-agente.png)


---
# 🧩 Diseño del Sistema

✔ Espera Inteligente: El scraper no depende de pausas estáticas de tiempo (time.Sleep). Emplea la función WaitForSelector de Playwright para buscar el botón de Sign Out tras el login, y el searchBox (input[placeholder*='Address']) en el mapa, reduciendo los tiempos muertos drásticamente.

✔ Extracción Avanzada (RegEx): Las distancias en la web vienen en formato mixto (ej. 21.062km 13.09mi). El sistema usa expresiones regulares en Go (([\d\.]+)\s*mi) para aislar exclusivamente el valor numérico en millas y convertirlo a tipo float64 para evaluación matemática.

✔ Filtro de Descarte: El sistema evalúa cada torre raspada. Si la distancia supera las 6.0 millas, o el status de la señal no es estrictamente igual a "Good Link", la torre es bloqueada y descartada en la memoria, asegurando que a la base de datos y al cliente solo llegue data accionable.

---

# 💾 Modelo de Datos (Payloads)

1. Herramienta MCP (get_tower_coverage)

Si un LLM interactúa con el sistema, verá la siguiente firma de herramienta:

    Descripción: "Consulta TowerCoverage para encontrar torres con 'Good Link' a menos de 6 millas. Devuelve distancias, señal, alineación y tilt."

    Parámetros Requeridos: lat (string) y lon (string).

### 2. Petición API REST (POST /api/coverage)

Formato para clientes tradicionales:

```Bash
{
  "lat": "18.465500",
  "lon": "-66.105700"
}
```

Response (Éxito):

```Bash
[
  {
    "TowerName": "OSN.Torre de la Reina",
    "Latitude": "18.4621",
    "Longitude": "-66.1102",
    "Alignment": "145.2°",
    "Tilt": "2.1°",
    "Distance": "0.83 mi",
    "Signal": "-69.2:RSSI",
    "Status": "Good Link"
  }
]
```

--- 

# 🧪 Pruebas y Verificación

✅ Probar el Endpoint REST:
Asegúrate de que el servidor corra con MCP_TRANSPORT=sse y ejecuta:

```Bash
curl -X POST http://localhost:8080/api/coverage \
-H "Content-Type: application/json" \
-d '{"lat": "18.465500", "lon": "-66.105700"}'
```

✅ Probar el Modo MCP con Inspector:
Para simular el razonamiento de una IA localmente:

```Bash
# Exportar variable explícitamente a stdio
export MCP_TRANSPORT=stdio
npx @modelcontextprotocol/inspector go run cmd/scraper/main.go
```

---

# 🚨 Troubleshooting

❌ Error: "playwright: timeout: Timeout 30000ms exceeded"

    Causa: El mapa tardó demasiado en renderizarse o el servicio de TowerCoverage está lento calculando la cobertura.

    Solución: Revisa tu conexión a internet o verifica el archivo debug_mapa_failed.png (si se generó) para confirmar si la página pidió re-autenticación.

❌ Error: "MySQL connection refused" / Fallo guardando en DB

    Causa: Credenciales incorrectas en el .env o servicio de base de datos caído.

    Solución: El scraper funciona y responde a la petición API incluso si la base de datos falla (el guardado es asíncrono y tolerante a fallos), pero verás un log de alerta en la consola. Valida el puerto DB_PORT y la existencia de la DB.

❌ Se devuelven 0 torres aprobadas (pero el mapa sí tiene torres)

    Causa: Ninguna torre cumplió la regla rígida (<= 6.0 millas Y "Good Link").

    Solución: Revisa los logs de Go; verás líneas ❌ DESCARTADA indicando el motivo exacto (ej. distancia 13.09 mi o status Weak Link).