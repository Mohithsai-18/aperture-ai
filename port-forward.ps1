# Aperture AI Port-Forward Keeper
# Keeps Grafana (3050) and Inference (8080) port-forwards alive permanently.
# Run once: powershell -ExecutionPolicy Bypass -File "e:\aperture ai\port-forward.ps1"

Write-Host "=== Aperture AI Port-Forward Service ===" -ForegroundColor Cyan
Write-Host "Grafana  -> http://localhost:3050" -ForegroundColor Green
Write-Host "Inference -> http://localhost:8080" -ForegroundColor Green
Write-Host "Press Ctrl+C to stop." -ForegroundColor Yellow
Write-Host ""

function Start-PortForward {
    param($Name, $Target, $LocalPort, $RemotePort, $Namespace)
    $job = Start-Job -ScriptBlock {
        param($t, $lp, $rp, $ns)
        while ($true) {
            kubectl port-forward $t "${lp}:${rp}" -n $ns 2>&1 | Out-Null
            Start-Sleep -Seconds 2
        }
    } -ArgumentList $Target, $LocalPort, $RemotePort, $Namespace
    Write-Host "Started $Name port-forward (Job: $($job.Id))" -ForegroundColor DarkGray
    return $job
}

# Start Grafana port-forward (service, more stable than pod)
$grafanaJob = Start-PortForward "Grafana" "svc/kube-prometheus-stack-grafana" 3050 80 "aperture-monitoring"

# Get current inference pod name dynamically
$inferencePod = kubectl get pod -n default -l app=aperture-inference -o jsonpath="{.items[0].metadata.name}" 2>$null
if ($inferencePod) {
    $inferenceJob = Start-PortForward "Inference" "pod/$inferencePod" 8080 8080 "default"
} else {
    Write-Host "No inference pod found - skipping port 8080" -ForegroundColor Yellow
}

# Health check loop
while ($true) {
    Start-Sleep -Seconds 10
    try {
        $g = Invoke-WebRequest "http://localhost:3050/api/health" -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        $time = (Get-Date).ToString("HH:mm:ss")
        Write-Host "$time | Grafana: OK" -ForegroundColor Green
    } catch {
        $time = (Get-Date).ToString("HH:mm:ss")
        Write-Host "$time | Grafana: RECONNECTING..." -ForegroundColor Yellow
    }
}
