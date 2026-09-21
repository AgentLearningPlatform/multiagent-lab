# eino-multiagent-lab 一键启动（Windows PowerShell）
# 用法: .\run-dev.ps1 [-Port 8080]
#   同时启动本体侧后端：ontology-service(:8091) + runtime-manager(:8090)
param(
    [int]$Port = 8080
)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "[run-dev] 未找到 go，请先安装 Go 1.25+（https://go.dev/dl/）" -ForegroundColor Red
    exit 1
}

# 本体侧可选依赖提示（缺了不阻塞：影响的是本体导入/AI草稿/运行方案启动）
if (Get-Command python -ErrorAction SilentlyContinue) {
    python -c "import rdflib" 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[run-dev] 警告: python 缺少 rdflib，本体导入(OWL/TTL)与导出不可用（pip install rdflib）" -ForegroundColor Yellow
    }
} else {
    Write-Host "[run-dev] 警告: 未找到 python，本体导入(OWL/TTL)与导出不可用" -ForegroundColor Yellow
}
if (-not (Get-Command oxigraph_server -ErrorAction SilentlyContinue)) {
    Write-Host "[run-dev] 警告: 未找到 oxigraph_server，运行方案启动(start)不可用（见 docs/04 §4.2）" -ForegroundColor Yellow
}

# 前端构建：dist 缺失、源码比 dist 新（如 git pull 之后）、或 FORCE_BUILD=1 时执行
$needBuild = (-not (Test-Path "web/dist/index.html")) -or ($env:FORCE_BUILD -eq "1")
if (-not $needBuild) {
    $distTime = (Get-Item "web/dist/index.html").LastWriteTime
    $newer = Get-ChildItem "web/src", "web/index.html", "web/package.json" -Recurse -File -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTime -gt $distTime }
    if ($newer) { $needBuild = $true }
}
if ($needBuild) {
    Write-Host "[run-dev] 构建前端..."
    Push-Location web
    npm install --no-audit --no-fund
    npm run build
    Pop-Location
}

# ---- 本体侧后端（构建平面 + 运行平面）----
New-Item -ItemType Directory -Force -Path "data\bin", "data\engines", "data\engine_logs" | Out-Null
Write-Host "[run-dev] 构建本体侧服务..."
Push-Location ontology-service
go build -o ..\data\bin\ontologyd.exe ./cmd/ontologyd
Pop-Location
Push-Location runtime-manager
go build -o ..\data\bin\runtimed.exe ./cmd/runtimed
Pop-Location

Write-Host "[run-dev] 启动 ontology-service: http://localhost:8091"
$root = (Get-Location).Path
# Start-Process 继承当前进程环境：逐服务注入，启动后清理，避免污染 backend
$env:ADDR = ":8091"; $env:DB_PATH = "data/ontology.db"
$env:MIGRATIONS_DIR = "ontology-service/migrations"
$env:SIDECAR_SCRIPT = "$root/tools/rdf-sidecar/sidecar.py"
$ont = Start-Process -FilePath "$root\data\bin\ontologyd.exe" -WorkingDirectory $root `
    -WindowStyle Hidden -PassThru `
    -RedirectStandardOutput "data\ontology-service.log" -RedirectStandardError "data\ontology-service.err.log"

Write-Host "[run-dev] 启动 runtime-manager: http://localhost:8090"
$env:ADDR = ":8090"; $env:DB_PATH = "data/runtime.db"
$env:MIGRATIONS_DIR = "runtime-manager/migrations"
$env:BUILD_SVC_URL = "http://127.0.0.1:8091"; $env:OXIGRAPH_BIN = "oxigraph_server"
$env:ENGINE_DATA_DIR = "data/engines"; $env:ENGINE_LOG_DIR = "data/engine_logs"
$rt = Start-Process -FilePath "$root\data\bin\runtimed.exe" -WorkingDirectory $root `
    -WindowStyle Hidden -PassThru `
    -RedirectStandardOutput "data\runtime-manager.log" -RedirectStandardError "data\runtime-manager.err.log"
Remove-Item Env:DB_PATH, Env:MIGRATIONS_DIR, Env:SIDECAR_SCRIPT, Env:BUILD_SVC_URL, Env:OXIGRAPH_BIN, Env:ENGINE_DATA_DIR, Env:ENGINE_LOG_DIR -ErrorAction SilentlyContinue

# 等本体侧就绪（最多 ~10s，失败不阻塞主平台启动，仅提示）
$ontAddr = "http://127.0.0.1:8091/healthz"
$rtAddr  = "http://127.0.0.1:8090/healthz"
$ready = $false
for ($i = 0; $i -lt 20; $i++) {
    try {
        $a = Invoke-WebRequest -UseBasicParsing -Uri $ontAddr -TimeoutSec 2
        $b = Invoke-WebRequest -UseBasicParsing -Uri $rtAddr  -TimeoutSec 2
        $ready = ($a.StatusCode -eq 200) -and ($b.StatusCode -eq 200)
        if ($ready) { break }
    } catch { Start-Sleep -Milliseconds 500 }
}
if (-not $ready) {
    Write-Host "[run-dev] 警告: 本体侧服务未完全就绪（详情见 data\ontology-service.log、data\runtime-manager.log）" -ForegroundColor Yellow
}

try {
    Write-Host "[run-dev] 启动 backend: http://localhost:$Port（本体页面经反代对接 :8091/:8090）"
    Set-Location backend
    $env:ADDR = ":$Port"
    go run ./cmd/backend
}
finally {
    foreach ($p in @($ont, $rt)) {
        if ($p -and -not $p.HasExited) {
            try { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue } catch {}
        }
    }
}
