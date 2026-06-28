$port = 8000
$processes = netstat -ano | Select-String ":$port" | Where-Object { $_ -match "LISTENING" } | ForEach-Object { ($_ -split "\s+")[-1] }

if ($processes) {
    foreach ($proc_id in $processes) {  # ✅ Use `$proc_id` instead of `$pid`
        Write-Host "🔍 Uvicorn process PID to stop: $proc_id"
        Stop-Process -Id $proc_id -Force -ErrorAction SilentlyContinue
        Write-Host "✅ PID $proc_id stopped successfully!"
    }
} else {
    Write-Host "❌ No running Uvicorn process found."
}
