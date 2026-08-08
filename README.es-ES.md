

# BookLife MCP

**Tu vida lectora, unificada.** Un servidor MCP que conecta tu biblioteca, tu seguimiento de lectura y tu estantería en un asistente de lectura impulsado por IA sin interrupciones.

BookLife conecta [Hardcover](https://hardcover.app) (seguimiento de lectura), [Libby/OverDrive](https://libbyapp.com) (acceso a bibliotecas) y [Open Library](https://openlibrary.org) (metadatos) para que puedas descubrir libros, pedir prestados en la biblioteca, seguir tu progreso de lectura y obtener recomendaciones personalizadas, todo a través de conversaciones naturales con Claude.

---

## Inicio Rápido

### 1. Compilar

```bash
cd booklife-mcp
go build -o booklife ./cmd/booklife
```

### 2. Conectar Libby

```bash
# Get your clone code from Libby app:
# Settings → Copy To Another Device → Sonos Speakers
./booklife libby-connect <8-digit-code>
```

### 3. Configurar

Crea `booklife.kdl`:

```kdl
server {
    name "booklife"
    version "0.1.0"
    transport "stdio"
}

providers {
    hardcover enabled=true {
        api-key env="HARDCOVER_API_KEY"
        endpoint "https://api.hardcover.app/v1/graphql"
    }

    libby enabled=true {
        notifications {
            hold-available true
            due-soon-days 3
        }
    }

    open-library enabled=true {
        endpoint "https://openlibrary.org"
        covers-endpoint "https://covers.openlibrary.org"
    }
}
```

### 4. Agregar a Claude Desktop

```json
{
  "mcpServers": {
    "booklife": {
      "command": "/path/to/booklife",
      "args": ["--config", "/path/to/booklife.kdl"],
      "env": {
        "HARDCOVER_API_KEY": "your-key-from-hardcover.app/settings/api"
      }
    }
  }
}
```

### 5. Instalar el complemento de Claude Code (Opcional)

Para una integración mejorada de Claude Code con habilidades y comandos con slash:

```bash
# Add the marketplace
/plugin marketplace add https://github.com/andylbrummer/andy-marketplace

# Install the plugin
/plugin install booklife@andy-marketplace
```

Consulta los detalles en el [complemento BookLife en andy-marketplace](https://github.com/andylbrummer/andy-marketplace/tree/main/plugins/booklife).

---

## Características

### Acceso a Bibliotecas (Libby/OverDrive)
Busca en el catálogo de tu biblioteca, verifica la disponibilidad, coloca reservas en ebooks y audiolibros, y realiza un seguimiento de préstamos y fechas de vencimiento, todo sin abrir la aplicación Libby.

### Seguimiento de Lectura (Hardcover)
Gestiona tu lista de lectura, actualiza tu progreso, califica libros y mantén tu historial de lectura en Hardcover a través de lenguaje natural.

### Gestión Unificada de TBR (Por Leer)
Una única lista por leer que agrega libros de Hardcover (deseo de leer), Libby (reservas y etiquetas) y libros físicos agregados manualmente. Filtra por fuente, busca y prioriza.

### Sincronización Integral
Sincronización con un solo comando que encadena: importar préstamos de Libby → marcar los libros devueltos como "leídos" en Hardcover → enriquecer metadatos → almacenar en caché las etiquetas de Libby. Incremental, con vista previa en modo de prueba (dry-run).

### Recomendaciones Basadas en Contenido
Enriquece tu historial de lectura con datos de temas, tópicos y estado de ánimo de Open Library y Google Books, y luego encuentra libros similares basados en lo que te ha gustado.

### Perfil de Lectura y Analíticas
Análisis automático de tus patrones de lectura: preferencias de formato, géneros principales, autores favoritos, ritmo de lectura, rachas y tasas de finalización.

### Descubrimiento Progresivo
Herramienta incorporada `info` con guías de flujo de trabajo, navegación por categorías y ayuda detallada de herramientas. Nunca te preguntes qué está disponible, solo pregunta.

---

## Herramientas de un Vistazo

| Categoría | Herramientas | Propósito |
|----------|-------|---------|
| **Hardcover** | 3 | Gestión de biblioteca, actualizaciones de estado, seguimiento de libros |
| **Libby** | 6 | Búsqueda en catálogo, préstamos, reservas, sincronización de etiquetas |
| **TBR** | 6 | Lista de lectura unificada en todas las fuentes |
| **Unificado** | 2 | Búsqueda cruzada entre proveedores y recomendaciones de acceso |
| **Historial** | 4 | Importación de línea de tiempo, almacenamiento local, estadísticas |
| **Enriquecimiento** | 2 | Tareas de metadatos en segundo plano con seguimiento de progreso |
| **Sincronización** | 1 | Sincronización universal con revelación progresiva |
| **Perfil** | 1 | Preferencias y patrones de lectura |
| **Recomendaciones** | 1 | Similitud de libros basada en contenido |
| **Info** | 1 | Descubrimiento progresivo y guías de flujo de trabajo |

---

## Arquitectura

```
┌──────────────────────────────────────────┐
│           Claude / AI Assistant          │
└────────────────────┬─────────────────────┘
                     │ MCP (stdio)
┌────────────────────▼─────────────────────┐
│           BookLife MCP Server            │
│                                          │
│  ┌─────────┐ ┌─────────┐ ┌───────────┐  │
│  │Hardcover│ │  Libby  │ │OpenLibrary│  │
│  │ GraphQL │ │   API   │ │  REST API │  │
│  └─────────┘ └─────────┘ └───────────┘  │
│                                          │
│  ┌──────────────────────────────────┐    │
│  │        Local SQLite Store        │    │
│  │  History · TBR · Enrichment      │    │
│  └──────────────────────────────────┘    │
└──────────────────────────────────────────┘
```

---

## Documentación

La documentación completa está disponible en **[andylbrummer.github.io/booklife-mcp](https://andylbrummer.github.io/booklife-mcp)**:

- [Primeros Pasos](https://andylbrummer.github.io/booklife-mcp/docs/getting-started) — Instalación y configuración
- [Referencia de Herramientas](https://andylbrummer.github.io/booklife-mcp/docs/category/tool-reference) — Las 27 herramientas con parámetros y ejemplos
- [Flujos de Trabajo](https://andylbrummer.github.io/booklife-mcp/docs/category/workflows) — Guías paso a paso para tareas comunes
- [Configuración](https://andylbrummer.github.io/booklife-mcp/docs/configuration) — Referencia del archivo de configuración KDL
- [Complemento de Claude Code](https://andylbrummer.github.io/booklife-mcp/docs/claude-code-plugin) — Habilidades y comandos a través de andy-marketplace

---

## Variables de Entorno

| Variable | Requerida | Descripción |
|----------|----------|-------------|
| `HARDCOVER_API_KEY` | Sí | [Obtener desde la configuración de Hardcover](https://hardcover.app/settings/api) |
| `BOOKLIFE_DATA_DIR` | No | Directorio de datos (predeterminado: `~/.local/share/booklife`) |
| `BOOKLIFE_LOG_LEVEL` | No | `debug` / `info` / `warn` / `error` |

La autenticación de Libby se maneja mediante el comando `booklife libby-connect` (no se requiere variable de entorno).

---

## Comandos CLI

```bash
booklife serve --config booklife.kdl    # Start MCP server
booklife libby-connect <code>           # Connect Libby account
booklife sync [--dry-run] [--limit N]   # Sync returned books to Hardcover
booklife import-timeline <file>         # Import Libby timeline JSON
booklife version                        # Show version
```

---

## Desarrollo

```bash
# Build
cd booklife-mcp && go build -o booklife ./cmd/booklife

# Run tests
go test ./...

# Vet
go vet ./...
```

---

## Licencia

Proyecto personal — adáptalo según sea necesario para tu propia gestión de lectura.
