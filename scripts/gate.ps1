$ErrorActionPreference = "Stop"
if ((Test-Path "C:\msys64\ucrt64\bin\gcc.exe") -and ($env:PATH -notlike "*C:\msys64\ucrt64\bin*")) {
    $env:PATH = "C:\msys64\ucrt64\bin;" + $env:PATH
}
go build ./...
go vet ./...
go test ./... -count=1
if ($args -contains "-race") { go test -race ./... }
if ($args -contains "-vuln") { govulncheck ./... }
