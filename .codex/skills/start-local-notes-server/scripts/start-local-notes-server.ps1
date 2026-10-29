param(
    [Parameter(Mandatory = $false)]
    [string]$RepoRoot = (Get-Location).Path,

    [Parameter(Mandatory = $false)]
    [switch]$ValidateOnly,

    [Parameter(Mandatory = $false)]
    [int]$DockerTimeoutSeconds = 120
)

$ErrorActionPreference = 'Stop'

function Fail([string]$Message) {
    throw $Message
}

function Quote-PowerShellLiteral([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

function Get-RequiredCommand([string]$Name) {
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($null -eq $command) {
        Fail "Required command '$Name' was not found on PATH. Install it, then retry."
    }
    return $command
}

function Test-DockerEngine([string]$DockerPath) {
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $DockerPath info 2>$null | Out-Null
        return ($LASTEXITCODE -eq 0)
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
}

function Read-SimpleEnvFile([string]$Path) {
    $values = @{}
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $values
    }

    foreach ($line in Get-Content -LiteralPath $Path) {
        $trimmed = $line.Trim()
        if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#')) {
            continue
        }
        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) {
            continue
        }
        $key = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $values[$key] = $value
    }
    return $values
}

function Set-SimpleEnvValue([string]$Path, [string]$Key, [string]$Value) {
    $lines = @()
    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        $lines = @(Get-Content -LiteralPath $Path)
    }

    $replacement = "$Key=$Value"
    $found = $false
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if ($lines[$index] -match "^\s*$([regex]::Escape($Key))\s*=") {
            $lines[$index] = $replacement
            $found = $true
            break
        }
    }
    if (-not $found) {
        $lines += $replacement
    }

    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, $lines, $utf8)
}

function Test-TcpPort([int]$Port) {
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $connect = $client.BeginConnect('127.0.0.1', $Port, $null, $null)
        if (-not $connect.AsyncWaitHandle.WaitOne(500)) {
            return $false
        }
        $client.EndConnect($connect)
        return $true
    }
    catch {
        return $false
    }
    finally {
        $client.Dispose()
    }
}

function Test-HttpEndpoint([string]$Uri, [switch]$RequireBody) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $Uri -TimeoutSec 3
        if ($response.StatusCode -lt 200 -or $response.StatusCode -ge 300) {
            return $false
        }
        if ($RequireBody -and [string]::IsNullOrWhiteSpace($response.Content)) {
            return $false
        }
        return $true
    }
    catch {
        return $false
    }
}

function Wait-HttpEndpoint([string]$Uri, [int]$TimeoutSeconds = 60) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        if (Test-HttpEndpoint -Uri $Uri -RequireBody) {
            return
        }
        Start-Sleep -Seconds 1
    } while ((Get-Date) -lt $deadline)
    Fail "Timed out waiting for $Uri. Inspect the server window for startup errors."
}

function Start-GoWindow(
    [string]$Title,
    [string]$GoPath,
    [string[]]$GoArguments,
    [string]$WorkingDirectory
) {
    $shell = Get-Command 'pwsh' -ErrorAction SilentlyContinue
    if ($null -eq $shell) {
        $shell = Get-Command 'powershell' -ErrorAction SilentlyContinue
    }
    if ($null -eq $shell) {
        Fail 'PowerShell was not found to host the local server windows.'
    }

    $repoLiteral = Quote-PowerShellLiteral $WorkingDirectory
    $goLiteral = Quote-PowerShellLiteral $GoPath
    $argumentLiterals = @($GoArguments | ForEach-Object { Quote-PowerShellLiteral $_ })
    $command = "`$Host.UI.RawUI.WindowTitle = $(Quote-PowerShellLiteral $Title); Set-Location -LiteralPath $repoLiteral; & $goLiteral run $($argumentLiterals -join ' '); if (`$LASTEXITCODE -ne 0) { Write-Host 'Process exited with code' `$LASTEXITCODE -ForegroundColor Red; Read-Host 'Press Enter to close' }"

    return Start-Process -FilePath $shell.Source -ArgumentList @('-NoProfile', '-NoExit', '-Command', $command) -WorkingDirectory $WorkingDirectory -PassThru
}

if (-not (Test-Path -LiteralPath $RepoRoot -PathType Container)) {
    Fail "Repository directory does not exist: $RepoRoot"
}
$RepoRoot = (Resolve-Path -LiteralPath $RepoRoot).Path

$expectedFiles = @('AGENTS.md', 'go.mod', 'compose.yaml', 'run-instruction.md', '.env.example')
$missingFiles = @($expectedFiles | Where-Object { -not (Test-Path -LiteralPath (Join-Path $RepoRoot $_) -PathType Leaf) })
if ($missingFiles.Count -gt 0) {
    Fail "The supplied directory is not the Notes Sync Backend repository. Missing: $($missingFiles -join ', ')"
}

$go = Get-RequiredCommand 'go'
$docker = Get-RequiredCommand 'docker'

& $go.Source version | Out-Host
& $docker.Source compose version | Out-Host
if ($LASTEXITCODE -ne 0) {
    Fail 'Docker Compose is unavailable. Install Docker Desktop with Compose support, then retry.'
}

$envPath = Join-Path $RepoRoot '.env'
$envExamplePath = Join-Path $RepoRoot '.env.example'
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    if ($ValidateOnly) {
        Fail '.env is missing. Copy .env.example to .env and fill the required values before a preflight can pass.'
    }
    Copy-Item -LiteralPath $envExamplePath -Destination $envPath
    Write-Host 'Created .env from .env.example.' -ForegroundColor Yellow
}

$envValues = Read-SimpleEnvFile $envPath
$issuer = if ([string]::IsNullOrWhiteSpace($envValues['JWT_ISSUER'])) { 'notes-dev' } else { $envValues['JWT_ISSUER'].Trim() }
$audience = if ([string]::IsNullOrWhiteSpace($envValues['JWT_AUDIENCE'])) { 'notes-api' } else { $envValues['JWT_AUDIENCE'].Trim() }
$jwksUrl = 'http://localhost:8081/.well-known/jwks.json'

if (-not $ValidateOnly) {
    $authDir = Join-Path $RepoRoot '.dev/auth'
    $authFiles = @('jwks.json', 'token.txt', 'metadata.json') | ForEach-Object { Join-Path $authDir $_ }
    $authReady = @($authFiles | Where-Object { -not (Test-Path -LiteralPath $_ -PathType Leaf) }).Count -eq 0
    if (-not $authReady) {
        Write-Host 'Generating development JWT/JWKS fixture...' -ForegroundColor Cyan
        & $go.Source run ./cmd/devauth generate -out .dev/auth -issuer $issuer -audience $audience -jwks-url $jwksUrl -days 365
        if ($LASTEXITCODE -ne 0) {
            Fail 'Development JWT/JWKS generation failed.'
        }
    }
    Set-SimpleEnvValue -Path $envPath -Key 'JWT_ISSUER' -Value $issuer
    Set-SimpleEnvValue -Path $envPath -Key 'JWT_AUDIENCE' -Value $audience
    Set-SimpleEnvValue -Path $envPath -Key 'JWT_JWKS_URL' -Value $jwksUrl
    $envValues = Read-SimpleEnvFile $envPath
}

$requiredValues = @('DATABASE_URL', 'JWT_ISSUER', 'JWT_AUDIENCE', 'JWT_JWKS_URL', 'SUPABASE_URL', 'SUPABASE_SECRET_KEY', 'SUPABASE_STORAGE_BUCKET')
$missingValues = @($requiredValues | Where-Object { [string]::IsNullOrWhiteSpace($envValues[$_]) })
if ($missingValues.Count -gt 0) {
    Fail "Required .env values are missing: $($missingValues -join ', '). Fill them in $envPath and retry. Do not use placeholder Supabase credentials."
}

$apiPort = 8080
$websocketPort = 8082
if ($envValues.ContainsKey('PORT') -and -not [string]::IsNullOrWhiteSpace($envValues['PORT']) -and -not [int]::TryParse($envValues['PORT'], [ref]$apiPort)) {
    Fail "PORT must be an integer; found '$($envValues['PORT'])'."
}
if ($envValues.ContainsKey('WEBSOCKET_PORT') -and -not [string]::IsNullOrWhiteSpace($envValues['WEBSOCKET_PORT']) -and -not [int]::TryParse($envValues['WEBSOCKET_PORT'], [ref]$websocketPort)) {
    Fail "WEBSOCKET_PORT must be an integer; found '$($envValues['WEBSOCKET_PORT'])'."
}

if ($ValidateOnly) {
    if (-not (Test-Path -LiteralPath (Join-Path $RepoRoot '.dev/auth/jwks.json') -PathType Leaf)) {
        Fail 'Development JWKS fixture is missing. Run without -ValidateOnly to generate it.'
    }
    Write-Host 'Preflight passed: repository, tools, .env values, and local auth fixture are available.' -ForegroundColor Green
    exit 0
}

try {
    $dockerReady = $false
    $dockerReady = Test-DockerEngine -DockerPath $docker.Source

    if (-not $dockerReady) {
        $dockerDesktopCandidates = @(
            (Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'),
            (Join-Path $env:LOCALAPPDATA 'Docker\Docker Desktop.exe')
        ) | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf }
        if ($dockerDesktopCandidates.Count -eq 0) {
            Fail 'Docker Desktop is not running and its executable could not be found. Start Docker Desktop, wait for it to become ready, and retry.'
        }
        Write-Host 'Starting Docker Desktop...' -ForegroundColor Cyan
        Start-Process -FilePath $dockerDesktopCandidates[0] -WindowStyle Hidden | Out-Null
        $deadline = (Get-Date).AddSeconds($DockerTimeoutSeconds)
        do {
            Start-Sleep -Seconds 2
            $dockerReady = Test-DockerEngine -DockerPath $docker.Source
        } while (-not $dockerReady -and (Get-Date) -lt $deadline)
        if (-not $dockerReady) {
            Fail "Docker Desktop did not become ready within $DockerTimeoutSeconds seconds."
        }
    }
    Write-Host 'Docker is running.' -ForegroundColor Green

    Write-Host 'Starting PostgreSQL container...' -ForegroundColor Cyan
    & $docker.Source compose --project-directory $RepoRoot up -d postgres
    if ($LASTEXITCODE -ne 0) {
        Fail 'PostgreSQL container failed to start.'
    }

    $dbDeadline = (Get-Date).AddSeconds(90)
    do {
        $health = (& $docker.Source inspect -f '{{.State.Health.Status}}' notes-postgres 2>$null | Select-Object -First 1).Trim()
        if ($health -eq 'healthy') {
            break
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $dbDeadline)
    if ($health -ne 'healthy') {
        Fail "PostgreSQL did not become healthy. Current Docker health state: '$health'."
    }
    Write-Host 'PostgreSQL is healthy.' -ForegroundColor Green

    Write-Host 'Applying pending database migrations...' -ForegroundColor Cyan
    & $go.Source run -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@latest -path db/migrations -database $envValues['DATABASE_URL'] up
    if ($LASTEXITCODE -ne 0) {
        Fail 'Database migrations failed.'
    }

    $started = @()

    $jwksUri = 'http://localhost:8081/.well-known/jwks.json'
    if (Test-HttpEndpoint -Uri $jwksUri) {
        Write-Host 'Development JWKS server is already healthy; reusing it.' -ForegroundColor Yellow
    }
    elseif (Test-TcpPort -Port 8081) {
        Fail 'Port 8081 is already in use by a non-JWKS service.'
    }
    else {
        Write-Host 'Starting development JWKS server...' -ForegroundColor Cyan
        $started += Start-GoWindow -Title 'Notes Sync - Development JWKS' -GoPath $go.Source -GoArguments @('./cmd/devauth', 'serve', '-dir', '.dev/auth', '-addr', ':8081') -WorkingDirectory $RepoRoot
        Wait-HttpEndpoint -Uri $jwksUri -TimeoutSeconds 30
    }

    $websocketUri = "http://localhost:$websocketPort/health"
    if (Test-HttpEndpoint -Uri $websocketUri) {
        Write-Host "WebSocket server is already healthy on port $websocketPort; reusing it." -ForegroundColor Yellow
    }
    elseif (Test-TcpPort -Port $websocketPort) {
        Fail "Port $websocketPort is already in use by a non-WebSocket service."
    }
    else {
        Write-Host "Starting WebSocket server on port $websocketPort..." -ForegroundColor Cyan
        $started += Start-GoWindow -Title 'Notes Sync - WebSocket' -GoPath $go.Source -GoArguments @('./cmd/websocket') -WorkingDirectory $RepoRoot
        Wait-HttpEndpoint -Uri $websocketUri -TimeoutSeconds 60
    }

    $apiUri = "http://localhost:$apiPort/health"
    if (Test-HttpEndpoint -Uri $apiUri) {
        Write-Host "API server is already healthy on port $apiPort; reusing it." -ForegroundColor Yellow
    }
    elseif (Test-TcpPort -Port $apiPort) {
        Fail "Port $apiPort is already in use by a non-API service."
    }
    else {
        Write-Host "Starting API server on port $apiPort..." -ForegroundColor Cyan
        $started += Start-GoWindow -Title 'Notes Sync - API' -GoPath $go.Source -GoArguments @('./cmd/api') -WorkingDirectory $RepoRoot
        Wait-HttpEndpoint -Uri $apiUri -TimeoutSeconds 60
        Wait-HttpEndpoint -Uri "http://localhost:$apiPort/ready" -TimeoutSeconds 60
    }

    Write-Host ''
    Write-Host 'Local Notes Sync environment is running.' -ForegroundColor Green
    Write-Host "  API:       http://localhost:$apiPort"
    Write-Host "  WebSocket: http://localhost:$websocketPort"
    Write-Host '  JWKS:      http://localhost:8081/.well-known/jwks.json'
    Write-Host '  Database:  notes-postgres (Docker Compose)'
    Write-Host '  Dev token: .dev/auth/token.txt'
}
catch {
    Write-Error $_
    Write-Host 'Startup did not complete. Any server windows already opened remain available for diagnostics.' -ForegroundColor Yellow
    exit 1
}
