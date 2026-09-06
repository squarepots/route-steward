[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-True([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }

$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$agent = Join-Path $repo 'agent\route-steward-agent.ps1'
$stage = Join-Path ([IO.Path]::GetTempPath()) ('rst-agent-test-' + [Guid]::NewGuid().ToString('N'))
$binaryStage = Join-Path ([IO.Path]::GetTempPath()) ('rst-agent-bin-' + [Guid]::NewGuid().ToString('N'))
$recoveryTarget = Join-Path ([IO.Path]::GetTempPath()) ('rst-agent-recovery-target-' + [Guid]::NewGuid().ToString('N'))
$archiveFixture = Join-Path ([IO.Path]::GetTempPath()) ('rst-agent-recovery-fixture-' + [Guid]::NewGuid().ToString('N') + '.7z')
$previousRouteStewardBin = $env:RST_ROUTE_STEWARD_BIN

try {
    $go = Get-Command go -ErrorAction SilentlyContinue
    if (-not $go) {
        $portableGo = Join-Path $repo '.tools\go\bin\go.exe'
        if (Test-Path -LiteralPath $portableGo -PathType Leaf) { $go = [pscustomobject]@{ Source = $portableGo } }
    }
    Assert-True ($null -ne $go) 'Go is unavailable for the source-checkout compatibility test.'
    $goVersion = (& $go.Source env GOVERSION 2>$null | Out-String).Trim()
    Assert-True ($LASTEXITCODE -eq 0 -and $goVersion -match '^go1\.27(?:\.|$)') "Go 1.27 is required for the source-checkout compatibility test; found '$goVersion'."

    Push-Location $repo
    try {
        $sourceVersion = (& $go.Source run ./cmd/route-steward version | Out-String).Trim()
        $sourceExit = $LASTEXITCODE
    }
    finally { Pop-Location }
    Assert-True ($sourceExit -eq 0 -and $sourceVersion -eq ([IO.File]::ReadAllText((Join-Path $repo 'version.txt')).Trim())) 'The documented Go source-checkout invocation failed.'

    New-Item -ItemType Directory -Force -Path $binaryStage | Out-Null
    $agentBinaryName = if ($env:OS -eq 'Windows_NT') { 'route-steward-agent-test.exe' } else { 'route-steward-agent-test' }
    $agentBinary = Join-Path $binaryStage $agentBinaryName
    Push-Location $repo
    try {
        & $go.Source build -o $agentBinary ./cmd/route-steward
        $buildExit = $LASTEXITCODE
    }
    finally { Pop-Location }
    Assert-True ($buildExit -eq 0 -and (Test-Path -LiteralPath $agentBinary -PathType Leaf)) 'The agent compatibility test could not build the current native CLI.'
    $env:RST_ROUTE_STEWARD_BIN = $agentBinary

    $bootstrap = & $agent bootstrap -PrivateDirectory $stage | ConvertFrom-Json
    Assert-True ($bootstrap.success -and $bootstrap.data.created) 'Clean agent bootstrap did not create private state.'
    Assert-True ($bootstrap.data.context.inventory_schema -eq 2) 'Clean bootstrap did not create current inventory state.'
    Assert-True (@($bootstrap.data.context.profiles).Count -eq 0 -and @($bootstrap.data.context.client_targets).Count -eq 0) 'Clean bootstrap invented topology or client intent.'

    $legacyOperatorPath = Join-Path $stage 'operator.json'
    $legacyOperator = '{"schema":1,"mode":"steward"}'
    [IO.File]::WriteAllText($legacyOperatorPath, $legacyOperator, [Text.UTF8Encoding]::new($false))
    $bootstrapAgain = & $agent bootstrap -PrivateDirectory $stage | ConvertFrom-Json
    Assert-True (-not $bootstrapAgain.data.created) 'Bootstrap did not recognize complete existing state.'
    Assert-True ([IO.File]::ReadAllText($legacyOperatorPath, [Text.Encoding]::UTF8) -eq $legacyOperator) 'Deprecated operator state was changed instead of ignored.'

    $capabilities = & $agent capabilities -PrivateDirectory $stage | ConvertFrom-Json
    Assert-True ($capabilities.success -and $capabilities.data.product -eq 'route-steward' -and $capabilities.data.interface -eq 'agent-machine-surface') 'Full capability discovery did not expose the machine interface.'
    Assert-True (@($capabilities.data.capabilities | Where-Object id -eq 'add-server').Count -eq 1) 'Full capability discovery is missing add-server.'

    $focusedCapability = & $agent capabilities -PrivateDirectory $stage -Operation add-server | ConvertFrom-Json
    Assert-True ($focusedCapability.success -and $focusedCapability.data.capability.id -eq 'add-server') 'Focused capability lookup returned the wrong operation.'
    Assert-True (-not $focusedCapability.data.PSObject.Properties['capabilities'] -and -not $focusedCapability.data.PSObject.Properties['drivers']) 'Focused capability lookup returned full discovery data.'
    Assert-True (@($focusedCapability.data.capability.required_context | Where-Object name -eq 'host_ownership').Count -eq 1) 'Focused add-server metadata is missing host ownership.'

    $blocked = & $agent preflight -PrivateDirectory $stage -Operation add-server | ConvertFrom-Json
    Assert-True (-not $blocked.data.ready -and $blocked.data.missing_context.Count -gt 0) 'Incomplete add-server context was not blocked.'

    $keyPath = Join-Path $stage 'fixture.pem'
    [IO.File]::WriteAllText($keyPath, 'fixture', [Text.UTF8Encoding]::new($false))
    $badContext = [ordered]@{ server_id = 'blocked-user'; public_ipv4 = '192.0.2.21'; ssh_user = 'bad user'; ssh_key_path = $keyPath; host_ownership = 'dedicated' } | ConvertTo-Json -Compress
    $badPreflight = & $agent preflight -PrivateDirectory $stage -Operation add-server -ContextJson $badContext | ConvertFrom-Json
    Assert-True (-not $badPreflight.data.ready -and @($badPreflight.data.conflicts) -contains 'ssh-user-invalid') 'Unsafe SSH user passed agent preflight.'

    $serverContext = [ordered]@{ server_id = 'entry-a'; public_ipv4 = '192.0.2.10'; public_ipv6 = '2001:db8::10'; ssh_user = 'ubuntu'; ssh_key_path = $keyPath; host_ownership = 'dedicated' } | ConvertTo-Json -Compress
    $addServer = & $agent execute -PrivateDirectory $stage -Operation add-server -ContextJson $serverContext | ConvertFrom-Json
    Assert-True ($addServer.success -and $addServer.data.result.id -eq 'entry-a' -and -not $addServer.data.result.remote_changed) 'Structured add-server failed or overstated remote effects.'

    $routeContext = [ordered]@{ route_id = 'direct-a'; display_name = 'Direct-A'; kind = 'direct'; entry_server = 'entry-a'; listen_port = 20000 } | ConvertTo-Json -Compress
    $addRoute = & $agent execute -PrivateDirectory $stage -Operation add-route -ContextJson $routeContext | ConvertFrom-Json
    Assert-True ($addRoute.success -and $addRoute.data.result.id -eq 'direct-a') 'Structured add-route failed.'

    $profileContext = [ordered]@{ profile_id = 'primary'; include_routes = @('direct-a'); include_providers = @(); routing = [ordered]@{ rules = @() } } | ConvertTo-Json -Depth 8 -Compress
    $addProfile = & $agent execute -PrivateDirectory $stage -Operation add-profile -ContextJson $profileContext | ConvertFrom-Json
    Assert-True ($addProfile.success -and $addProfile.data.result.id -eq 'primary') 'Structured add-profile failed.'

    $targetContext = [ordered]@{ target_id = 'desktop'; profile_id = 'primary'; renderer = 'mihomo'; mihomo_process_names = @('launcher.exe', 'com.example.app') } | ConvertTo-Json -Compress
    $addTarget = & $agent execute -PrivateDirectory $stage -Operation add-client-target -ContextJson $targetContext | ConvertFrom-Json
    Assert-True ($addTarget.success -and $addTarget.data.result.renderer -eq 'mihomo') 'Structured add-client-target failed.'

    $focusedContext = & $agent context -PrivateDirectory $stage -Target direct-a | ConvertFrom-Json
    Assert-True ($focusedContext.success -and $focusedContext.data.kind -eq 'route' -and $focusedContext.data.route.id -eq 'direct-a') 'Focused context did not return the requested Route.'
    Assert-True (@($focusedContext.data.profiles) -contains 'primary' -and @($focusedContext.data.client_targets) -contains 'desktop') 'Focused Route context lost direct consumers.'
    Assert-True (-not $focusedContext.data.PSObject.Properties['counts'] -and -not $focusedContext.data.PSObject.Properties['routing']) 'Focused Route context expanded full project data.'

    $context = & $agent context -PrivateDirectory $stage | ConvertFrom-Json
    $contextText = $context | ConvertTo-Json -Depth 20
    Assert-True ($context.data.inventory_schema -eq 2) 'Full context did not preserve current inventory schema.'
    Assert-True (-not ($contextText -match '192\.0\.2\.10|fixture\.pem|2001:db8|launcher\.exe|com\.example\.app')) 'Agent context leaked private infrastructure or process data.'

    [IO.File]::WriteAllText($archiveFixture, 'fixture-only-for-preflight', [Text.UTF8Encoding]::new($false))
    $recoveryContext = [ordered]@{ archive_path = $archiveFixture } | ConvertTo-Json -Compress
    $recoverPreflight = & $agent preflight -PrivateDirectory $recoveryTarget -Operation recover -ContextJson $recoveryContext | ConvertFrom-Json
    Assert-True ($recoverPreflight.success -and $recoverPreflight.data.ready -and $recoverPreflight.data.requires_local_secret_prompt) 'Recovery preflight lost its local secret-prompt boundary.'
    $recoverExecute = & $agent execute -PrivateDirectory $recoveryTarget -Operation recover -ContextJson $recoveryContext | ConvertFrom-Json
    Assert-True (-not $recoverExecute.success -and $recoverExecute.code -eq 'local-assistance-required') 'Agent recovery incorrectly pretended to complete non-interactively.'
    Assert-True ($recoverExecute.data.result.command -match '^route-steward recover ') 'Agent recovery did not delegate to the native secure restore workflow.'
    $global:LASTEXITCODE = 0

    Write-Host 'Agent machine envelope, focused discovery, focused context, preflight, privacy, and assisted recovery tests passed.'
}
finally {
    if ($null -eq $previousRouteStewardBin) { Remove-Item Env:\RST_ROUTE_STEWARD_BIN -ErrorAction SilentlyContinue }
    else { $env:RST_ROUTE_STEWARD_BIN = $previousRouteStewardBin }
    foreach ($path in @($stage, $binaryStage, $recoveryTarget)) { if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force } }
    if (Test-Path -LiteralPath $archiveFixture) { Remove-Item -LiteralPath $archiveFixture -Force }
}
