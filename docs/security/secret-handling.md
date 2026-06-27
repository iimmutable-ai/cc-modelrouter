# Security: Secret Handling in Logs

This document describes the security measures implemented to prevent API key and credential leakage in log files.

## Overview

**Severity**: CRITICAL - Exposing API keys in logs is a severe security vulnerability that can lead to unauthorized access to LLM provider accounts.

**Solution**: All HTTP headers containing sensitive information are automatically redacted before being written to log files.

## Policy

**CRITICAL**: API keys, tokens, and other secrets MUST NEVER appear in log files.

This applies to:
- Production logs
- Debug logs
- Test output
- Error messages
- Any written output that may persist

## Sensitive Headers

The following headers are automatically redacted by the sanitization library:

| Header | Description |
|--------|-------------|
| `Authorization` | Bearer tokens, Basic auth |
| `X-Api-Key` | Anthropic and other API keys |
| `X-Auth-Token` | Authentication tokens |
| `Cookie` | Session cookies |
| `Set-Cookie` | Session cookies being set |
| `Proxy-Authorization` | Proxy credentials |
| `X-Amz-Security-Token` | AWS security tokens |
| `X-Goog-Iam-Authorization` | Google IAM tokens |
| `X-BigModel-Api-Key` | BigModel API keys |
| `X-OpenRouter-Api-Key` | OpenRouter API keys |
| `Apikey` | Generic API key header |

**Note**: Header matching is case-insensitive to handle variations like `X-API-KEY`, `x-api-key`, etc.

## Implementation

### Sanitization Library Location

```
internal/logging/sanitize.go
```

### Core Functions

```go
// SanitizeHeaders returns a copy of headers with sensitive values redacted
func SanitizeHeaders(headers http.Header) map[string][]string

// SanitizeHeadersString returns a string representation safe for logging
func SanitizeHeadersString(headers http.Header) string
```

### Usage Examples

**CORRECT - Sanitized logging:**
```go
import "github.com/iimmutable-ai/cc-modelrouter/internal/logging"

// For debug logging
logging.Debugf("[PROXY REQUEST] URL: %s, Method: %s, Headers: %s",
    req.URL.String(), req.Method, logging.SanitizeHeadersString(req.Header))

// For error logging
logging.Errorf("[ERROR] Request Headers: %s", logging.SanitizeHeadersString(req.Header))
```

**INCORRECT - NEVER do this:**
```go
// DANGEROUS - Leaks API keys!
logging.Debugf("[PROXY REQUEST] Headers: %v", req.Header)

// DANGEROUS - Leaks API keys!
log.Printf("Headers: %+v", headers)

// DANGEROUS - Leaks API keys!
fmt.Printf("Request: %#v\n", request)
```

### Redaction Format

Sensitive values are logged with:
1. First 6 characters preserved (for debugging - identifies which key)
2. Asterisks for remaining characters (max 16)
3. `[REDACTED]` marker

Examples:
```
X-Api-Key:[sk-ant-**************** [REDACTED]]
Authorization:[Bearer**************** [REDACTED]]
Cookie:[sessio**************** [REDACTED]]
X-BigModel-Api-Key:[bigmod*********** [REDACTED]]
```

For short values (≤8 characters):
```
X-Api-Key:[REDACTED]
```

For empty values:
```
X-Api-Key:[EMPTY]
```

## Testing

### Security Test Location

```
test/security/secret_logging_test.go
```

### Running Security Tests

```bash
# Run all security tests
go test -v ./test/security

# Run specific test
go test -v ./test/security -run Test_NoAPIKeyInLogs
```

### Test Coverage

The security test suite verifies:
1. **Test_NoAPIKeyInLogs** - Verifies API keys never appear in log output
2. **Test_HeaderRedaction** - Tests all sensitive header types
3. **Test_LogOutputCapture** - Captures actual log output and verifies no secrets
4. **Test_MultipleHeaderValues** - Tests headers with multiple values
5. **Test_RealWorldScenarios** - Tests realistic provider request headers
6. **Test_NilAndEmptyHeaders** - Edge cases

### Adding New Sensitive Headers

1. Add the header name (lowercase) to `SensitiveHeaders` map in `internal/logging/sanitize.go`:

```go
var SensitiveHeaders = map[string]bool{
    // ... existing headers ...
    "x-new-provider-api-key": true,
}
```

2. Add a test case in `test/security/secret_logging_test.go`:

```go
{"X-New-Provider-Api-Key", "secret-value"},
```

3. Run tests to verify:
```bash
go test -v ./internal/logging ./test/security
```

## Code Review Checklist

Before committing code that logs HTTP requests/responses:

- [ ] Uses `logging.SanitizeHeaders()` or `logging.SanitizeHeadersString()`
- [ ] Never uses `%v`, `%+v`, or `%#v` with `http.Header`
- [ ] Security tests pass
- [ ] No raw header values in log output
- [ ] Test code also uses sanitization (mock servers, etc.)

## Incident Response

If secrets are found in logs:

1. **Immediately rotate exposed keys/tokens** at the provider's console
2. **Delete log files** containing secrets (they should not be committed to git)
3. **Identify and fix** the logging code that leaked the secret
4. **Add regression test** to prevent recurrence
5. **Update this documentation** if new patterns discovered

## Common Mistakes

### 1. Using `%v` with Headers

```go
// BAD - Leaks secrets
logging.Debugf("Headers: %v", req.Header)

// GOOD - Sanitized
logging.Debugf("Headers: %s", logging.SanitizeHeadersString(req.Header))
```

### 2. Logging Entire Request Objects

```go
// BAD - May include headers with secrets
logging.Debugf("Request: %+v", request)

// GOOD - Log specific non-sensitive fields
logging.Debugf("Request URL: %s, Method: %s", request.URL, request.Method)
```

### 3. Test Code Logging Headers

```go
// BAD - Test output may be visible
t.Logf("Headers: %+v", r.Header)

// GOOD - Sanitized test output
t.Logf("Headers: %s", logging.SanitizeHeadersString(r.Header))
```

## Related Files

- `internal/logging/sanitize.go` - Sanitization implementation
- `internal/logging/sanitize_test.go` - Unit tests
- `test/security/secret_logging_test.go` - Security integration tests
- `internal/proxy/handler.go` - Main logging location (now secured)

## History

- **2026-03-15**: Initial implementation after discovering API keys were being logged with debug level enabled
- Created `internal/logging/sanitize.go` with header sanitization
- Fixed 3 locations in `internal/proxy/handler.go` that logged raw headers
- Added comprehensive security test suite in `test/security/`