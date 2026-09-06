[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$failures = [Collections.Generic.List[string]]::new()

function Require-Text([string]$RelativePath, [string]$Pattern, [string]$Message) {
    $path = Join-Path $repo $RelativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { $script:failures.Add("${RelativePath}: required file is missing."); return }
    $text = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8)
    if ($text -notmatch $Pattern) { $script:failures.Add("${RelativePath}: $Message") }
}

function Reject-Text([string]$RelativePath, [string]$Pattern, [string]$Message) {
    $path = Join-Path $repo $RelativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { return }
    $text = [IO.File]::ReadAllText($path, [Text.Encoding]::UTF8)
    if ($text -match $Pattern) { $script:failures.Add("${RelativePath}: $Message") }
}

function Require-IgnoredPath([string]$RelativePath) {
    & git -C $repo check-ignore --quiet --no-index -- $RelativePath
    if ($LASTEXITCODE -ne 0) { $script:failures.Add("${RelativePath}: generated or private path is not ignored.") }
}

function Reject-IgnoredPath([string]$RelativePath) {
    & git -C $repo check-ignore --quiet --no-index -- $RelativePath
    if ($LASTEXITCODE -eq 0) { $script:failures.Add("${RelativePath}: repository source path is unexpectedly ignored.") }
}

# Static server safety properties that are naturally proven from the mutation scripts.
Require-Text 'server/install-path-components.sh' 'HYSTERIA_VERSION=v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)' 'The supported Hysteria2 binary is not pinned to a plain release version.'
Require-Text 'server/install-path-components.sh' 'HYSTERIA_SHA256=[0-9a-f]{64}' 'The supported Hysteria2 binary is not hash-pinned.'
Require-Text 'server/install-path-components.sh' 'RST_BIN_DIR=/usr/local/lib/route-steward' 'The RST-owned binary namespace is missing.'
Require-Text 'server/install-path-components.sh' 'HYSTERIA_BIN=\$\{RST_BIN_DIR\}/hysteria' 'Hysteria2 is not installed inside the RST-owned namespace.'
Require-Text 'server/config/99-route-steward-ssh.conf' 'PasswordAuthentication no' 'SSH password login is not disabled.'
Require-Text 'server/config/route-steward-hysteria.service' 'User=route-steward-hysteria' 'Direct-route service does not use the RST-owned runtime identity.'
Require-Text 'server/configure-relay-entry.sh' 'bindDevice:\s*\$\{INTERFACE\}' 'Relay ingress is not bound to its WireGuard interface.'
Require-Text 'server/configure-relay-entry.sh' 'Requires=wg-quick@\$\{INTERFACE\}' 'Relay service does not require its WireGuard interface.'
Require-Text 'server/configure-ingress.sh' '--port-hopping-range' 'Direct ingress does not carry the bounded port-hopping deployment argument.'
Require-Text 'server/configure-relay-entry.sh' '--port-hopping-range' 'Relay ingress does not carry the bounded port-hopping deployment argument.'
Require-Text 'server/configure-ingress.sh' 'CAP_NET_BIND_SERVICE CAP_NET_ADMIN' 'Direct port hopping does not grant the required scoped capability.'
Require-Text 'server/configure-relay-entry.sh' 'CAP_NET_RAW CAP_NET_ADMIN' 'Relay port hopping does not grant the required scoped capability.'
Require-Text 'server/base-setup.sh' nftables 'Port hopping cannot rely on an unavailable packet-filter helper.'
Require-Text 'server/configure-relay-exit.sh' 'POSTROUTING.+MASQUERADE' 'Relay exit NAT is missing.'
Reject-Text 'server/base-setup.sh' 'ufw --force reset' 'Base deployment must not erase unrelated firewall rules.'
Reject-Text 'server/configure-relay-exit.sh' 'ufw --force reset' 'Relay deployment must not erase unrelated firewall rules.'
Reject-Text 'server/configure-ingress.sh' '(?i)\b(?:reality|vless)\b' 'Unsupported Reality/VLESS leaked into the supported direct-route path.'
Reject-Text 'server/configure-relay-entry.sh' '(?i)\b(?:reality|vless)\b' 'Unsupported Reality/VLESS leaked into the supported relay path.'
Reject-Text 'server/uninstall.sh' 'ufw --force reset|swapoff|apt(?:-get)?\s+remove' 'Uninstall must not reset or remove unrelated host state.'
Reject-Text 'server/uninstall.sh' '(?:/usr/local/bin/hysteria|/etc/hysteria|/var/lib/hysteria|/usr/local/bin/xray|/etc/xray|/var/lib/xray)' 'Uninstall must not delete generic or unrelated proxy software state.'
Reject-Text 'server/uninstall.sh' '(?:/etc/modules-load\.d/bbr\.conf|/etc/systemd/journald\.conf\.d/99-limits\.conf|/etc/apt/apt\.conf\.d/52unattended-upgrades-local)' 'Uninstall must not delete generic host policy filenames.'
Reject-Text 'server/audit.sh' '(?:/usr/local/bin/xray|/etc/xray|/var/lib/xray|xray\.service)' 'Direct audit must not require unrelated Xray to be absent.'
Reject-Text 'server/audit-relay.sh' '(?:/usr/local/bin/xray|/etc/xray|/var/lib/xray|xray-relay)' 'Relay audit must not require unrelated Xray to be absent.'

# The private subscription Worker is intentionally a narrow no-log/no-cache surface.
Require-Text 'worker/wrangler.jsonc' '"preview_urls": false' 'Worker preview URLs must be disabled.'
Require-Text 'worker/wrangler.jsonc' '"enabled": false' 'Worker observability must remain disabled.'
Require-Text 'worker/src/index.ts' 'timingSafeEqual' 'Worker token comparison is not timing safe.'
Require-Text 'worker/src/index.ts' 'private, no-store, max-age=0' 'Worker responses must not be cached.'
Reject-Text 'worker/src/index.ts' 'console\.(?:log|debug|info|warn|error)' 'Worker must not log private subscription requests.'
Require-Text 'internal/steward/subscription.go' '"ci", "--ignore-scripts", "--no-audit", "--no-fund"' 'Standalone publication does not install the embedded Worker lockfile.'
Require-Text 'internal/steward/subscription.go' '"--no-install", "wrangler"' 'Standalone publication may resolve an unpinned transient Wrangler tree.'
Reject-Text 'internal/steward/subscription.go' 'wrangler@|--yes' 'Standalone publication must not bypass the embedded Worker lockfile.'

# Standard hosted CI is PR/manual only. Dependency installation remains reproducible.
Require-Text '.github/workflows/ci.yml' '(?m)^\s*pull_request:\s*$' 'Standard CI is missing the PR validation boundary.'
Require-Text '.github/workflows/ci.yml' 'types:\s*\[opened, reopened, synchronize\]' 'Standard CI does not use the bounded PR lifecycle.'
Require-Text '.github/workflows/ci.yml' '(?m)^\s*workflow_dispatch:\s*$' 'Manual validation trigger is missing.'
Reject-Text '.github/workflows/ci.yml' '(?m)^\s*push:\s*$' 'Standard CI must not run on push.'
Require-Text '.github/workflows/ci.yml' 'cancel-in-progress:\s*true' 'Superseded validation runs are not cancelled.'
Require-Text '.github/workflows/ci.yml' 'go test ./\.\.\.' 'The native Go control plane is not tested.'
Require-Text '.github/workflows/ci.yml' 'Build-ReleaseArtifacts\.sh' 'The real release archives are not validated in CI.'
Require-Text '.github/workflows/release.yml' 'Build-ReleaseArtifacts\.sh' 'The release workflow does not use the validated archive builder.'
Require-Text 'scripts/Build-ReleaseArtifacts.sh' 'linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64' 'The six supported release targets are incomplete.'
Require-Text '.github/workflows/release.yml' 'ref:\s*\$\{\{ github\.sha \}\}' 'Release checkout is not pinned to the workflow event SHA.'
Require-Text '.github/workflows/release.yml' 'git tag -a "\$tag" -m "\$tag" "\$EVENT_SHA"' 'Release tag is not created from the immutable workflow event SHA.'
Reject-Text '.github/workflows/release.yml' 'git pull' 'Release workflow must not replace the event source with moving branch state.'
foreach ($ignoredPath in @(
    'private/inventory.json',
    'nested/private/secrets/index.json',
    'migrations.json',
    'dist/route-steward_1.6.0_linux_amd64.tar.gz',
    'bin/route-steward.exe',
    'route-steward.exe',
    'coverage/coverage.out',
    'worker/node_modules/example/package.json',
    'worker/dist/index.js',
    'worker/.wrangler/state.json',
    'worker/.dev.vars.local',
    '.env.local',
    'credentials.json',
    'go.work',
    'docs/notes.zh-CN.md'
)) { Require-IgnoredPath $ignoredPath }
foreach ($sourcePath in @(
    '.env.example',
    'credentials-guide.md',
    'README.zh-CN.md',
    'docs/TEST-PLAN.md',
    'mcp/src/index.ts',
    'worker/src/index.ts'
)) { Reject-IgnoredPath $sourcePath }

$trackedChineseDocs = @(& git -C $repo ls-files -- 'docs/*.zh-CN.md' | Where-Object { Test-Path -LiteralPath (Join-Path $repo $_) -PathType Leaf })
if ($LASTEXITCODE -ne 0) {
    $failures.Add('Unable to enumerate tracked localized documentation.')
}
elseif ($trackedChineseDocs.Count) {
    $failures.Add('Chinese localized documentation is limited to README.zh-CN.md; remove tracked docs/*.zh-CN.md: ' + ($trackedChineseDocs -join ', '))
}

# GFM treats CJK text immediately after a bare URL as part of the link target.
$unsafeCjkAutolinkPattern = 'https?://[A-Za-z0-9._~:/?#\[\]@!$&''()*+,;=%-]+(?=[\p{IsCJKUnifiedIdeographs}\u3000-\u303F\uFF00-\uFFEF])'
$markdownFiles = @(& git -C $repo ls-files -- '*.md')
if ($LASTEXITCODE -ne 0) { $failures.Add('Unable to enumerate tracked Markdown files.') }
foreach ($relativePath in $markdownFiles) {
    $lines = [IO.File]::ReadAllLines((Join-Path $repo $relativePath), [Text.Encoding]::UTF8)
    for ($lineIndex = 0; $lineIndex -lt $lines.Count; $lineIndex++) {
        if ($lines[$lineIndex] -match $unsafeCjkAutolinkPattern) {
            $failures.Add("${relativePath}:$($lineIndex + 1): bare URL runs into CJK text.")
        }
    }
}

$examplePath = Join-Path $repo 'examples/inventory.example.json'
if (Test-Path -LiteralPath $examplePath -PathType Leaf) {
    try {
        $example = [IO.File]::ReadAllText($examplePath, [Text.Encoding]::UTF8) | ConvertFrom-Json
        if ([int]$example.schema -ne 2) { $failures.Add('examples/inventory.example.json: public inventory schema must be 2.') }
    }
    catch { $failures.Add('examples/inventory.example.json: invalid JSON.') }
}
else { $failures.Add('examples/inventory.example.json: required file is missing.') }

$psFiles = Get-ChildItem -Path $repo -Recurse -Filter '*.ps1' -File -ErrorAction SilentlyContinue | Where-Object { $_.FullName -notmatch '[\\/](?:private|\.cache|node_modules)[\\/]' }
$strictUtf8 = [Text.UTF8Encoding]::new($false, $true)
foreach ($file in $psFiles) {
    $bytes = [IO.File]::ReadAllBytes($file.FullName)
    try { $null = $strictUtf8.GetString($bytes) }
    catch { $failures.Add("$($file.FullName): file is not valid UTF-8.") }
    $tokens = $null
    $parseErrors = $null
    [Management.Automation.Language.Parser]::ParseFile($file.FullName, [ref]$tokens, [ref]$parseErrors) | Out-Null
    foreach ($parseError in $parseErrors) { $failures.Add("$($file.FullName):$($parseError.Extent.StartLineNumber): $($parseError.Message)") }
}

if ($failures.Count) {
    $failures | Sort-Object -Unique | ForEach-Object { Write-Error $_ }
    exit 1
}
Write-Host 'Static server safety, Worker privacy, CI, repository layout, and portability validation passed.'
