#!/bin/bash
set -e

echo "Starting Aperture Inference Docker build pipeline..."

echo "1. Building image..."
docker build -t aperture-inference:latest .

echo "2. Verifying GPU support..."
VERIFY_OUTPUT=$(docker run --rm --gpus all aperture-inference:latest python -c "import torch; print(torch.cuda.is_available())" 2>&1)

echo "GPU check output: $VERIFY_OUTPUT"
if [[ "$VERIFY_OUTPUT" != *"True"* ]]; then
    echo "ERROR: GPU verification failed! Expected True, got: $VERIFY_OUTPUT"
    exit 1
fi
echo "GPU verification passed."

echo "3. Tagging image..."
docker tag aperture-inference:latest mohithsai18/aperture-inference:latest

echo "4. Pushing image to Docker Hub..."
docker push mohithsai18/aperture-inference:latest

echo "========================================"
echo "✔ Build Status: SUCCESS"
echo "✔ GPU Verification Result: TRUE"
echo "✔ Docker Image URL: https://hub.docker.com/r/mohithsai18/aperture-inference"
echo "========================================"
