# Relay API Server

Go backend for the Relay API.

## Run

```powershell
cd server
& "C:\Program Files\Go\bin\go.exe" run ./cmd/server
```

Default runtime:

- Address: `:8080`
- Database: `relay.db` SQLite file in `server/`
- Admin: `admin@relay.io` / `admin123456`
- Default invite code: `TEAM-DEV-2026`
- Built-in CLIProxyAPI URL: `http://127.0.0.1:8317`

Upstream sources use `apiBase` as the service root URL, without protocol
suffixes such as `/v1` or `/v1beta`. The relay appends the protocol path during
forwarding, for example `/v1/chat/completions`, `/v1/messages`, or
`/v1beta/models/...`.

CLIProxyAPI is a built-in source. Its root URL is configured with
`RELAY_CLIPROXYAPI_BASE_URL`, not through the admin UI. Its forwarding key is
configured with `RELAY_CLIPROXYAPI_API_KEY`, while
`RELAY_CLIPROXYAPI_MANAGEMENT_KEY` must match CLIProxyAPI
`remote-management.secret-key` and is used only for OAuth/account pool
operations. Account sync calls
`/v0/management/auth-files`; OAuth start calls the provider-specific
`/v0/management/*-auth-url` endpoint. If CLIProxyAPI has an empty
`remote-management.secret-key`, those management routes are unavailable and
return 404.

Account pool management is only available for `CLIProxyAPI` sources. Third-party
provider sources use the source-level API key for upstream authentication by
default. They can also own multiple named source keys, and each model can bind
to one source key to route that model through a specific upstream credential or
billing group.

Relay endpoints:

- OpenAI-compatible: `/v1/chat/completions`, `/v1/completions`, `/v1/responses`
- Anthropic native: `/v1/messages`, `/v1/messages/count_tokens`
- Gemini native: `/v1beta/models`, `/v1beta/models/{model}:generateContent`

For CLIProxyAPI sources, native Anthropic and Gemini requests are forwarded to
CLIProxyAPI provider-specific paths such as
`/api/provider/anthropic/v1/messages` and
`/api/provider/google/v1beta/models/...`. The platform does not convert OpenAI
payloads into Anthropic payloads for the native Anthropic endpoint.

## Environment

```text
RELAY_ADDR=:8080
RELAY_DATABASE_DRIVER=sqlite
RELAY_DATABASE_DSN=relay.db
RELAY_JWT_SECRET=change-me
RELAY_ADMIN_EMAIL=admin@relay.io
RELAY_ADMIN_PASSWORD=admin123456
RELAY_CLIPROXYAPI_BASE_URL=http://127.0.0.1:8317
RELAY_CLIPROXYAPI_API_KEY=
RELAY_CLIPROXYAPI_MANAGEMENT_KEY=
RELAY_SEED_DATA=true
RELAY_SMTP_HOST=smtp.example.com
RELAY_SMTP_PORT=587
RELAY_SMTP_USERNAME=noreply@example.com
RELAY_SMTP_PASSWORD=change-me
RELAY_SMTP_FROM=noreply@example.com
RELAY_EMAIL_CODE_TTL=10m
RELAY_EMAIL_CODE_COOLDOWN=60s
RELAY_EMAIL_CODE_DEV=false
RELAY_REQUIRE_EMAIL_VERIFICATION=false
```

Registration email verification is disabled by default. Set
`RELAY_REQUIRE_EMAIL_VERIFICATION=true` to show the email-code step and require
verification during registration. When it is enabled, email codes use SMTP if
`RELAY_SMTP_HOST` is configured. In local development without SMTP, also set
`RELAY_EMAIL_CODE_DEV=true` to let the send-code API return a `devCode` for
end-to-end testing.

PostgreSQL is supported with:

```text
RELAY_DATABASE_DRIVER=postgres
RELAY_DATABASE_DSN=host=127.0.0.1 user=relay password=relay dbname=relay port=5432 sslmode=disable
```
