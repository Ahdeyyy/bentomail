$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    $env:CGO_ENABLED = '0'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    go tool mygo vet .
    if ($LASTEXITCODE -ne 0) { throw 'MyGo checks failed' }
    go tool mygo build
    if ($LASTEXITCODE -ne 0) { throw 'Build failed' }
} finally {
    Pop-Location
}
