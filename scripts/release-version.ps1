$ErrorActionPreference = 'Stop'
$config = Get-Content "$PSScriptRoot/../mygo.json" -Raw | ConvertFrom-Json
$version = [version]$config.version
$prefix = [regex]::Escape($config.updates.tagPrefix)

$releases = gh release list --limit 100 --json tagName
if ($LASTEXITCODE -ne 0) { throw 'Could not read release versions' }

# Include legacy releases during migration, and drafts to reserve their versions.
foreach ($release in ($releases | ConvertFrom-Json)) {
    if ($release.tagName -match "^(?:$prefix|build-)(\d+\.\d+\.\d+)$") {
        $released = [version]$Matches[1]
        if ($released -ge $version) {
            $version = [version]::new($released.Major, $released.Minor, $released.Build + 1)
        }
    }
}

$version.ToString(3)
