# BCV Scraper & Exchange Rate API - Documentación Técnica

Servicio de backend en Go para la sincronización periódica, almacenamiento histórico y consulta de tasas oficiales de cambio (**Dólar**, **Euro** y **Pesos Colombianos TRM**) para integración con sistemas contables, ERP (SAP), Tesorería y Facturación.

---

## 1. Arquitectura y Flujo del Sistema

```
                 +--------------------------------+
                 |       Cron Scheduler /         |
                 |      Startup Background        |
                 +----------------+---------------+
                                  |
               +------------------+------------------+
               |                                     |
               v                                     v
     [ Scraper BCV (HTML) ]               [ API Colombia (JSON) ]
     URL: bcv.org.ve                      URL: datos.gov.co
     Divisas: Dólar, Euro                 Divisa: Pesos (TRM)
               |                                     |
               +------------------+------------------+
                                  |
                                  v
                    +---------------------------+
                    |  RatesRepository (GORM)   |
                    +-------------+-------------+
                                  |
                                  v
                  +-------------------------------+
                  |  SQL Server: LOCATEL.TasasCambio  |
                  +---------------+---------------+
                                  ^
                                  |
                    +-------------+-------------+
                    |      Fiber HTTP Server    |
                    |    (Puerto 8083 por def.) |
                    +---------------------------+
```

* **Fuente de Datos BCV:** Web scraping con `colly` sobre el portal oficial del Banco Central de Venezuela (`https://bcv.org.ve/`).
* **Fuente de Datos Colombia:** API REST de datos abiertos del Gobierno de Colombia (`https://datos.gov.co/resource/32sa-8pi3.json`) para la Tasa Representativa del Mercado (TRM).
* **Persistencia:** Microsoft SQL Server (tabla `[dbo].[LOCATEL.TasasCambio]`).
* **Resiliencia:** Reintentos con backoff exponencial, `CompareAndSwap` atómico para evitar solapamiento de scraping, y verificación de duplicados para no reescribir tasas idénticas.
* **Apagado Limpio (Graceful Shutdown):** Detiene conexiones HTTP, espera la terminación de tareas del Cron y drena logs a disco antes de cerrar el proceso.

---

## 2. Endpoints de Salud y Monitoreo

### 2.1 Liveness Probe
Comprueba que el proceso del servidor HTTP esté vivo y respondiendo.

* **Método:** `GET`
* **Ruta:** `/healthz`
* **Respuesta Exitosa:**
  * **Código HTTP:** `200 OK`

---

### 2.2 Readiness Probe
Comprueba que la conexión a la base de datos SQL Server esté activa y disponible para recibir consultas.

* **Método:** `GET`
* **Ruta:** `/readyz`
* **Respuestas:**
  * **Código HTTP `200 OK`:**
    ```json
    {
      "status": "ready"
    }
    ```
  * **Código HTTP `503 Service Unavailable`:**
    ```json
    {
      "status": "not_ready"
    }
    ```

---

## 3. Endpoints de Tasas de Cambio

### 3.1 Últimas Tasas Vigentes
Devuelve la última tasa vigente registrada para cada una de las tres divisas (`Dolar`, `Euro`, `Pesos`) en una sola respuesta.

* **Método:** `GET`
* **Ruta:** `/exchange-rates` *(alias retrocompatible: `/exchange-rate`)*
* **Path Parameters:** Ninguno.
* **Query Parameters:** Ninguno.
* **Caso de Uso:** Pantallas principales de facturación, displays de cotización actual o inicialización de formularios.
* **Ejemplo de Petición:**
  ```bash
  curl -X GET http://localhost:8083/exchange-rates
  ```
* **Respuesta Exitosa (`200 OK`):**
  ```json
  {
    "dolar": {
      "moneda": "Dolar",
      "valor": 538.1245,
      "fecha": "2026-10-15T10:30:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-15T00:00:00Z"
    },
    "euro": {
      "moneda": "Euro",
      "valor": 625.3214,
      "fecha": "2026-10-15T10:30:00-04:00",
      "simbolo": "€",
      "fechaValida": "2026-10-15T00:00:00Z"
    },
    "pesos": {
      "moneda": "Pesos",
      "valor": 3650.12,
      "fecha": "2026-10-15T10:30:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-15T00:00:00Z"
    }
  }
  ```

---

### 3.2 Histórico de Tasas
Consulta histórica sin paginación pensada para auditorías, reportes contables, cierres de mes o gráficos de evolución.

* **Método:** `GET`
* **Ruta:** `/exchange-rates/history`
* **Path Parameters:** Ninguno.
* **Query Parameters:**
  | Parámetro | Tipo | Requerido | Descripción |
  | :--- | :---: | :---: | :--- |
  | `currency` | String | No | Filtra por divisa (`Dolar`, `Euro` o `Pesos`). Insensible a mayúsculas. |
  | `date` | Date (`YYYY-MM-DD`) | No | Busca la tasa con fecha valor exacta (`FechaValida`). |
  | `from` | Date (`YYYY-MM-DD`) | No | Inicio del rango (`FechaValida >= from`). |
  | `to` | Date (`YYYY-MM-DD`) | No | Fin del rango (`FechaValida <= to`). |
  | `order` | String | No | Dirección cronológica: `DESC` (por defecto, más reciente primero) o `ASC`. |

* **Regla de Negocio:**
  > [!IMPORTANT]
  > Si se omiten `date`, `from` y `to`, el sistema filtra automáticamente para devolver únicamente los **últimos 30 días** respecto a la fecha actual.

* **Ejemplos de Petición:**
  ```bash
  # Últimos 30 días de todas las monedas en orden descendente
  curl -X GET "http://localhost:8083/exchange-rates/history"

  # Rango de fechas específico para Dólar en orden ascendente
  curl -X GET "http://localhost:8083/exchange-rates/history?currency=Dolar&from=2026-09-01&to=2026-09-30&order=ASC"

  # Fecha valor puntual
  curl -X GET "http://localhost:8083/exchange-rates/history?date=2026-10-10"
  ```

* **Respuesta Exitosa (`200 OK`):**
  ```json
  [
    {
      "moneda": "Dolar",
      "valor": 538.1245,
      "fecha": "2026-10-15T10:30:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-15T00:00:00Z"
    },
    {
      "moneda": "Dolar",
      "valor": 536.9812,
      "fecha": "2026-10-14T15:00:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-14T00:00:00Z"
    }
  ]
  ```

* **Errores Posibles:**
  * `400 Bad Request`: Si el formato de fecha de `date`, `from` o `to` no es `YYYY-MM-DD`.

---

### 3.3 Tasa Específica por Moneda (con Fallback a Día Hábil)
Consulta puntual para una sola divisa. Ideal para pantallas de Cuentas por Pagar al cargar una factura o Tesorería al armar un pago.

* **Método:** `GET`
* **Ruta:** `/exchange-rates/specific/:currency`
* **Path Parameters:**
  | Parámetro | Tipo | Requerido | Descripción |
  | :--- | :---: | :---: | :--- |
  | `:currency` | String | **Sí** | La divisa a consultar: `Dolar`, `Euro` o `Pesos`. |
* **Query Parameters:**
  | Parámetro | Tipo | Requerido | Descripción |
  | :--- | :---: | :---: | :--- |
  | `date` | Date (`YYYY-MM-DD`) | No | Fecha de la tasa deseada. |

* **Regla de Negocio (Fallback Día Hábil Anterior):**
  > [!NOTE]
  > * Si se **omite `date`**, devuelve la **última tasa registrada** de esa moneda.
  > * Si se **envía `date`** y la fecha cayó en fin de semana, día feriado o feriado bancario donde no hubo cotización oficial, el sistema busca automáticamente la **fecha hábil anterior más cercana**:
  >   `WHERE LOWER(Moneda) = LOWER(@currency) AND FechaValida <= @date ORDER BY FechaValida DESC TOP 1`

* **Ejemplos de Petición:**
  ```bash
  # Última tasa de Euro
  curl -X GET "http://localhost:8083/exchange-rates/specific/Euro"

  # Tasa de Dólar para una fecha (ej: un domingo que resolverá al viernes anterior)
  curl -X GET "http://localhost:8083/exchange-rates/specific/Dolar?date=2026-10-18"
  ```

* **Respuesta Exitosa (`200 OK`):**
  ```json
  {
    "moneda": "Dolar",
    "valor": 538.1245,
    "fecha": "2026-10-16T15:00:00-04:00",
    "simbolo": "$",
    "fechaValida": "2026-10-16T00:00:00Z"
  }
  ```

* **Errores Posibles:**
  * `400 Bad Request`: Formato de fecha inválido.
  * `404 Not Found`: Si no existe ninguna tasa en la base de datos para esa divisa en esa fecha o fechas anteriores.
    ```json
    {
      "error": "no rate found for currency 'Dolar' on or prior to the requested date"
    }
    ```

---

### 3.4 Diferencial Cambiario
Calcula de forma exacta la diferencia entre la tasa con la que se registró la factura original de un proveedor (`issue_date`) y la tasa con la que Tesorería la liquida (`payment_date`), obteniendo directamente la ganancia o pérdida en cambio.

* **Método:** `GET`
* **Ruta:** `/exchange-rates/differential`
* **Path Parameters:** Ninguno.
* **Query Parameters:**
  | Parámetro | Tipo | Requerido | Descripción |
  | :--- | :---: | :---: | :--- |
  | `currency` | String | **Sí** | Divisa de la operación (`Dolar`, `Euro`, `Pesos`). |
  | `issue_date` | Date (`YYYY-MM-DD`) | **Sí** | Fecha de emisión/recepción de la factura del proveedor. |
  | `payment_date` | Date (`YYYY-MM-DD`) | **Sí** | Fecha en la que Tesorería realiza el pago. |

* **Regla de Negocio:**
  > [!TIP]
  > Aplica la misma búsqueda con **fallback a día hábil anterior** tanto para `issue_date` como para `payment_date`.
  > Luego realiza las fórmulas matemáticas con precisión fija decimal:
  > * $\text{Diferencia Absoluta} = \text{Tasa Pago} - \text{Tasa Emisión}$
  > * $\text{Variación Porcentual} = \left(\frac{\text{Tasa Pago} - \text{Tasa Emisión}}{\text{Tasa Emisión}}\right) \times 100$

* **Ejemplo de Petición:**
  ```bash
  curl -X GET "http://localhost:8083/exchange-rates/differential?currency=Dolar&issue_date=2026-10-01&payment_date=2026-10-15"
  ```

* **Respuesta Exitosa (`200 OK`):**
  ```json
  {
    "currency": "Dolar",
    "issue_rate": {
      "moneda": "Dolar",
      "valor": 520.0000,
      "fecha": "2026-10-01T15:00:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-01T00:00:00Z"
    },
    "payment_rate": {
      "moneda": "Dolar",
      "valor": 538.1245,
      "fecha": "2026-10-15T15:00:00-04:00",
      "simbolo": "$",
      "fechaValida": "2026-10-15T00:00:00Z"
    },
    "absolute_difference": 18.1245,
    "percentage_difference": 3.4855
  }
  ```

* **Errores Posibles:**
  * `400 Bad Request`: Si falta alguno de los 3 parámetros obligatorios o las fechas no cumplen `YYYY-MM-DD`.
  * `404 Not Found`: Si alguna de las dos fechas (o sus días hábiles previos) no posee cotización registrada.

---

## 4. Estructura de Entidades (Modelos)

### Entidad `Coin`
Representa un registro individual de tasa de cambio:
```json
{
  "moneda": "string (Dolar | Euro | Pesos)",
  "valor": "decimal (ej. 538.1245)",
  "fecha": "datetime ISO8601 (momento del scrape)",
  "simbolo": "string ($, €)",
  "fechaValida": "datetime ISO8601 (fecha valor de vigencia)"
}
```

### Entidad `DifferentialResponse`
```json
{
  "currency": "string",
  "issue_rate": { ...Coin... },
  "payment_rate": { ...Coin... },
  "absolute_difference": "decimal",
  "percentage_difference": "decimal"
}
```

---

## 5. Tabla de Base de Datos (SQL Server)

El servicio lee y escribe sobre la tabla:
```sql
CREATE TABLE dbo.[LOCATEL.TasasCambio] (
    Moneda      NVARCHAR(100) NULL,
    Valor       MONEY         NULL,
    Fecha       DATETIME      NULL,
    Simbolo     NCHAR(10)     NULL,
    FechaValida DATETIME      NULL
);
```

---

## 6. Variables de Entorno (`.env`)

| Variable | Tipo | Default | Descripción |
| :--- | :---: | :---: | :--- |
| `APP_NAME` | String | `facturacion-sap` | Identificador de la aplicación. |
| `PORT` | Int | `8080` | Puerto en el que escucha Fiber HTTP. |
| `SQLSERVER_HOST` | String | `localhost` | Host de la base de datos SQL Server. |
| `SQLSERVER_PORT` | String | `1433` | Puerto de SQL Server. |
| `SQLSERVER_USER` | String | `sa` | Usuario de SQL Server. |
| `SQLSERVER_PASSWORD` | String | `""` | Contraseña de SQL Server. |
| `SQLSERVER_DATABASE` | String | `""` | Base de datos (ej: `RMHLOCATEL_A344`). |
| `SQLSERVER_ENCRYPT` | String | `true` | `disable` o `true`. |
| `SQLSERVER_TRUST_SERVER_CERTIFICATE` | String | `false` | `true` para certificados autofirmados. |
| `SQLSERVER_CONNECTION_TIMEOUT` | String | `30` | Timeout de conexión en segundos. |
| `SQLSERVER_RECONNECTION_ATTEMPTS` | Int | `3` | Número de reintentos con backoff al arrancar. |
| `CRON_SPEC` | String | `0 1 * * *` | Expresión cron para scraping recurrente. |
| `SCRAPER_MAX_ATTEMPTS` | Int | `3` | Reintentos del scraper en caso de fallo de red. |
| `SCRAPER_BASE_DELAY` | Duration | `1s` | Demora base entre reintentos. |
| `SCRAPER_MAX_DELAY` | Duration | `5s` | Demora máxima entre reintentos. |
| `LOG_FILE_NAME` | String | `logs/app.log` | Archivo de log rotativo (Lumberjack). |
| `LOG_LEVEL` | String | `info` | Nivel de logging (`debug`, `info`, `warn`, `error`). |
