$ErrorActionPreference = "Stop"

Write-Output "Starting Aperture Inference Docker build pipeline..."

Write-Output "1. Building image..."
docker build -t aperture-inference:latest .

Write-Output "2. Verifying GPU support..."
$VerifyOutput = docker run --rm --gpus all aperture-inference:latest python -c "import torch; print(torch.cuda.is_available())" 2>&1

Write-Output "GPU check output: $VerifyOutput"

if ($VerifyOutput -notmatch "True") {
    Write-Output "ERROR: GPU verification failed! Expected True, got: $VerifyOutput"
    exit 1
}

Write-Output "GPU verification passed."

Write-Output "3. Tagging image..."
docker tag aperture-inference:latest mohithsai18/aperture-inference:latest

Write-Output "4. Pushing image to Docker Hub..."
docker push mohithsai18/aperture-inference:latest

Write-Output "========================================"
Write-Output "Build Status: SUCCESS"
Write-Output "GPU Verification Result: TRUE"
Write-Output "Docker Image URL: https://hub.docker.com/r/mohithsai18/aperture-inference"
Write-Output "========================================"
