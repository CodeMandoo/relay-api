param(
  [ValidateSet("linux", "windows", "darwin")]
  [string]$TargetOS = "linux",

  [ValidateSet("amd64", "arm64")]
  [string]$TargetArch = "amd64",

  [string]$OutputDir = "release",

  [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"

function Invoke-CheckedCommand {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Title,

    [Parameter(Mandatory = $true)]
    [scriptblock]$Command
  )

  Write-Host ""
  Write-Host "==> $Title"
  & $Command
  if ($LASTEXITCODE -ne 0) {
    throw "$Title failed with exit code $LASTEXITCODE"
  }
}

function Resolve-Tool {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name,

    [string[]]$Candidates = @()
  )

  $Command = Get-Command $Name -ErrorAction SilentlyContinue
  if ($Command) {
    return $Command.Source
  }

  foreach ($Candidate in $Candidates) {
    if (Test-Path $Candidate) {
      return $Candidate
    }
  }

  throw "Command not found: $Name"
}

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $RepoRoot

$Pnpm = Resolve-Tool "pnpm"
$Go = Resolve-Tool "go" @(
  "C:\Program Files\Go\bin\go.exe",
  "C:\Program Files (x86)\Go\bin\go.exe"
)

$ReleaseRoot = Join-Path $RepoRoot $OutputDir
$BackendDir = Join-Path $ReleaseRoot "backend"
$FrontendDir = Join-Path $ReleaseRoot "frontend"
$DeployDir = Join-Path $ReleaseRoot "deploy"

if (Test-Path $ReleaseRoot) {
  Remove-Item -LiteralPath $ReleaseRoot -Recurse -Force
}

New-Item -ItemType Directory -Force -Path $BackendDir, $FrontendDir, $DeployDir | Out-Null

if (-not $SkipInstall) {
  Invoke-CheckedCommand "Install workspace dependencies" {
    & $Pnpm install --frozen-lockfile
  }
}

Invoke-CheckedCommand "Build frontend" {
  & $Pnpm --filter "@relay-api/web" build
}

$BinaryName = "relay-server"
if ($TargetOS -eq "windows") {
  $BinaryName = "relay-server.exe"
}
$BackendBinary = Join-Path $BackendDir $BinaryName

$PreviousGOOS = $env:GOOS
$PreviousGOARCH = $env:GOARCH
$PreviousCGO = $env:CGO_ENABLED

try {
  $env:GOOS = $TargetOS
  $env:GOARCH = $TargetArch
  $env:CGO_ENABLED = "0"

  Push-Location (Join-Path $RepoRoot "server")
  try {
    Invoke-CheckedCommand "Build backend for $TargetOS/$TargetArch" {
      & $Go build -trimpath -ldflags="-s -w" -o $BackendBinary ./cmd/server
    }
  } finally {
    Pop-Location
  }
} finally {
  $env:GOOS = $PreviousGOOS
  $env:GOARCH = $PreviousGOARCH
  $env:CGO_ENABLED = $PreviousCGO
}

$FrontendDist = Join-Path $RepoRoot "apps/web/dist"
if (-not (Test-Path (Join-Path $FrontendDist "index.html"))) {
  throw "Frontend build output not found: $FrontendDist"
}
Copy-Item -LiteralPath $FrontendDist -Destination (Join-Path $FrontendDir "dist") -Recurse -Force

$EnvExample = @'
RELAY_ADDR=127.0.0.1:8080
RELAY_FRONTEND_DIST=/opt/relay-api/web/dist

RELAY_DATABASE_DRIVER=postgres
RELAY_DATABASE_DSN=host=127.0.0.1 user=relay password=replace-with-strong-password dbname=relay port=5432 sslmode=disable

RELAY_JWT_SECRET=replace-with-at-least-32-random-characters
RELAY_ACCESS_TTL=2h
RELAY_REFRESH_TTL=336h

RELAY_ADMIN_EMAIL=admin@example.com
RELAY_ADMIN_PASSWORD=replace-with-strong-admin-password
RELAY_SEED_DATA=false

RELAY_CLIPROXYAPI_BASE_URL=http://127.0.0.1:8317
RELAY_CLIPROXYAPI_API_KEY=your-api-key-1
RELAY_CLIPROXYAPI_MANAGEMENT_KEY=replace-with-cliproxy-management-key

RELAY_SMTP_HOST=
RELAY_SMTP_PORT=587
RELAY_SMTP_USERNAME=
RELAY_SMTP_PASSWORD=
RELAY_SMTP_FROM=
RELAY_EMAIL_CODE_TTL=10m
RELAY_EMAIL_CODE_COOLDOWN=1m
'@
Set-Content -LiteralPath (Join-Path $BackendDir "relay.env.example") -Value $EnvExample -Encoding UTF8

$SystemdService = @'
[Unit]
Description=Relay API
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
User=relay
Group=relay
WorkingDirectory=/opt/relay-api
EnvironmentFile=/etc/relay-api/relay.env
ExecStart=/opt/relay-api/bin/relay-server
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
'@
Set-Content -LiteralPath (Join-Path $DeployDir "relay-api.service") -Value $SystemdService -Encoding UTF8

$NginxConfig = @'
server {
    listen 80;
    server_name your-domain.com;

    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate /etc/letsencrypt/live/your-domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/your-domain.com/privkey.pem;

    client_max_body_size 20m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
'@
Set-Content -LiteralPath (Join-Path $DeployDir "nginx.conf") -Value $NginxConfig -Encoding UTF8

$Readme = @"
# Relay API Release

Target: $TargetOS/$TargetArch

Directory layout:

    backend/
      $BinaryName
      relay.env.example
    frontend/
      dist/
    deploy/
      relay-api.service
      nginx.conf

Server install target:

    /opt/relay-api/bin/relay-server
    /opt/relay-api/web/dist
    /etc/relay-api/relay.env

Use `deploy/relay-api.service` as the systemd template and `deploy/nginx.conf`
as the Nginx reverse proxy template.
"@
Set-Content -LiteralPath (Join-Path $ReleaseRoot "README.md") -Value $Readme -Encoding UTF8

Write-Host ""
Write-Host "Release package created:"
Write-Host "  $ReleaseRoot"
Write-Host ""
Get-ChildItem -LiteralPath $ReleaseRoot
