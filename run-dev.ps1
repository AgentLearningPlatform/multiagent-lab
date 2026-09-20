# eino-multiagent-lab 一键启动（Windows PowerShell）
# 用法: .\run-dev.ps1 [-Port 8080]
param(
    [int]$Port = 8080
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[run-dev] 未找到 go，请先安装 Go 1.25+（https://go.dev/dl/）" -ForegroundColor Red
    exit 1
}

# 前端构建（dist 缺失或 FORCE_BUILD=1 时）
if (-not (Test-Path "web/dist/index.html") -or $env:FORCE_BUILD -eq "1") {
    Write-Host "[run-dev] 构建前端..."
    Push-Location web
    npm install --no-audit --no-fund
    npm run build
    Pop-Location
}

Write-Host "[run-dev] 启动 backend: http://localhost:$Port"
Set-Location backend
$env:ADDR = ":$Port"
go run ./cmd/backend
