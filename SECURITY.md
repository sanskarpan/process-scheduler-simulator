# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest (`main`) | Yes |
| Older tagged releases | No — upgrade to latest |

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Please report security issues by emailing **sanskar@noclick.com** with:

1. A clear description of the vulnerability
2. Steps to reproduce (minimal reproduction case preferred)
3. Affected version or commit SHA
4. Your assessment of severity and impact
5. Any suggested remediation (optional but appreciated)

You will receive an acknowledgement within **48 hours** and a status update
within **7 days**. We aim to release a fix within **30 days** for confirmed
critical issues.

We do not currently have a bug bounty programme.

## Security Design

### Network Exposure

- The server binds to a configurable port (default `8082`). Do not expose this
  port directly to the internet without a reverse proxy or firewall rule.
- WebSocket origins are validated against a configurable allowlist.
  `ALLOW_LOCAL_ORIGIN=false` disables localhost origins in production.

### HTTP Hardening

The following headers are set on every response by the `SecureHeaders`
middleware:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Content-Security-Policy` | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:` |

### Input Validation

- All process fields are validated: `burstTime ≥ 1`, `arrivalTime ≥ 0`, `pid ≥ 0`.
- Reflected HTTP headers (`X-Request-ID`) are sanitized to printable ASCII
  (bytes 0x20–0x7E) before being written back to the client, preventing
  CRLF injection.
- JSON decoding enforces a maximum body size to prevent memory exhaustion
  on large payloads.

### Concurrency Protection

- The REST `/api/simulate` endpoint uses a semaphore to limit parallel
  simulations (configurable via `SIM_CONCURRENCY_LIMIT`, default 10).
  Requests over the limit return 503 immediately rather than queuing.

### Container Security

- The Docker image uses Google's **distroless** runtime base (`gcr.io/distroless/static`).
  There is no shell, package manager, or system utilities in the image.
- The `docker-compose.yml` configuration mounts the container filesystem
  read-only (`read_only: true`) with only `/tmp` writable as a `tmpfs`.
- The container runs without elevated privileges.

### Dependency Management

- `go.sum` pins all transitive dependency checksums.
- Dependabot is configured to open weekly PRs for patch and minor updates
  for both Go modules and GitHub Actions.
- `govulncheck` is run in CI on every push to detect known CVEs in
  dependencies and reachable standard-library code.

## Known Limitations

- **No authentication or authorisation.** The API is open. Deploy behind an
  authenticating reverse proxy (e.g., nginx with basic auth, or a VPN) if
  access control is required.
- **No rate limiting.** The concurrency semaphore limits parallelism but does
  not rate-limit individual clients. Add a rate-limiting proxy layer for
  public deployments.
- **In-memory state only.** Simulation history is stored in process memory and
  lost on restart. There is no persistence layer to harden.
