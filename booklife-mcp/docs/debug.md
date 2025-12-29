# Debug Logging

BookLife MCP provides optional debug logging to help diagnose API integration issues.

## Enabling Debug Mode

Set the `BOOKLIFE_DEBUG` environment variable to enable debug logging:

```bash
# Enable debug mode
export BOOKLIFE_DEBUG=true
booklife serve

# Or inline
BOOKLIFE_DEBUG=true booklife serve
```

## What Gets Logged

When debug mode is enabled, BookLife logs:

- **Hardcover API responses** - Full GraphQL responses
- **Libby API responses** - API calls to OverDrive services
- **Request/response timing** - Performance diagnostics

## Log Location

Debug logs are stored securely in your user data directory with restricted permissions (user-only readable):

```
Linux:   ~/.local/share/booklife/debug/
macOS:   ~/Library/Application Support/BookLife/debug/
Windows: %LOCALAPPDATA%\BookLife\debug\
```

Files:
- `hardcover-debug.log` - Last Hardcover API response
- `libby-debug.log` - Last Libby API response

## Security Note

Debug logs contain:
- API responses (potentially including personal data)
- Authentication tokens and session information
- Book titles, authors, and reading history

**Important:**
- Debug logs are stored with **0600 permissions** (user-only access)
- Logs are **overwritten** on each request (no accumulation)
- **Never commit** debug logs to version control
- **Disable debug mode** in production

## Troubleshooting with Debug Logs

### 1. API Authentication Issues

```bash
BOOKLIFE_DEBUG=true booklife serve
# Check hardcover-debug.log for authentication errors
cat ~/.local/share/booklife/debug/hardcover-debug.log | jq '.errors'
```

### 2. Libby Connection Problems

```bash
BOOKLIFE_DEBUG=true booklife libby-connect 12345678
# Check libby-debug.log for connection details
cat ~/.local/share/booklife/debug/libby-debug.log
```

### 3. Rate Limiting

Debug logs show HTTP status codes including 429 (rate limit exceeded).

## Disabling Debug Mode

```bash
unset BOOKLIFE_DEBUG
# Or set to false
export BOOKLIFE_DEBUG=false
```

## Privacy

Debug mode is **opt-in** and disabled by default. Logs are **never sent externally** and remain on your local machine only.
