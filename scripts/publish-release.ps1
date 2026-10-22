param(
    [Parameter(Mandatory)][string]$Version,
    [string]$ArtifactDirectory = 'artifacts'
)

$ErrorActionPreference = 'Stop'
$config = Get-Content "$PSScriptRoot/../mygo.json" -Raw | ConvertFrom-Json
$tag = "$($config.updates.tagPrefix)$Version"
$targets = 'windows-amd64', 'windows-arm64', 'linux-amd64', 'linux-arm64', 'darwin-universal'

# Never expose an update feed until every platform and its archive are present.
foreach ($target in $targets) {
    $manifest = Get-Content "$ArtifactDirectory/$target/update-$target.json" -Raw | ConvertFrom-Json
    $archiveName = "bentomail-$Version-$target.tar.gz"
    $archive = Get-Item "$ArtifactDirectory/$target/$archiveName"
    $expectedURL = "https://github.com/$($config.updates.github)/releases/download/$tag/$archiveName"
    if ($manifest.version -ne $Version -or $manifest.url -ne $expectedURL -or
        !$manifest.signature -or $manifest.size -ne $archive.Length) {
        throw "Invalid signed update manifest for $target"
    }
}

$assets = @(Get-ChildItem $ArtifactDirectory -Recurse -File | Where-Object {
    $_.Name -match ' Setup .*\.exe$|\.dmg$|\.deb$|\.tar\.gz$|\.delta$|^update-.*\.json$'
} | Select-Object -ExpandProperty FullName)

# Published versions are immutable. A rerun can finish an interrupted draft.
$release = gh release view $tag --json isDraft
if ($LASTEXITCODE -eq 0) {
    if (!(($release | ConvertFrom-Json).isDraft)) {
        Write-Host "$tag is already published."
        exit 0
    }
    gh release upload $tag @assets --clobber
} else {
    gh release create $tag @assets --draft --target $env:GITHUB_SHA --title $tag --generate-notes
}
if ($LASTEXITCODE -ne 0) { throw 'Release upload failed' }

gh release edit $tag --draft=false --latest
if ($LASTEXITCODE -ne 0) { throw 'Release publication failed' }
