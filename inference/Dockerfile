# =============================================================================
# Stage 1: Builder (Compilers and Build Tools)
# =============================================================================
FROM nvidia/cuda:12.1.1-devel-ubuntu22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        python3.11 python3.11-venv python3-pip curl git && \
    rm -rf /var/lib/apt/lists/*

# Create a virtual environment to easily copy dependencies
RUN python3.11 -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

WORKDIR /build
COPY requirements.txt .
# Use uv for lightning-fast deterministic builds
RUN pip install uv && uv pip install --no-cache -r requirements.txt

# =============================================================================
# Stage 2: Production Runtime (Minimal Attack Surface)
# =============================================================================
FROM nvidia/cuda:12.1.1-base-ubuntu22.04 AS runtime

ENV DEBIAN_FRONTEND=noninteractive
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1

# Install only the bare minimum runtime dependencies (no git, no pip)
RUN apt-get update && \
    apt-get install -y --no-install-recommends python3.11 curl && \
    rm -rf /var/lib/apt/lists/*

# Copy the pre-built virtual environment from the builder stage
COPY --from=builder /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

WORKDIR /app
COPY server.py tracing.py model_preloader.py ./

# Security: create user with UID 1000 to match Kubernetes runAsUser=1000.
# A proper /etc/passwd entry is required so that torch and other libs can
# resolve the username via getpwuid() without crashing.
RUN groupadd -r -g 1000 aperture && \
    useradd -r -u 1000 -g 1000 -d /app -s /sbin/nologin aperture && \
    chown -R aperture:aperture /app && \
    mkdir -p /tmp && chown aperture:aperture /tmp

USER aperture
EXPOSE 8080

# ── Application config ────────────────────────────────────────────────────────
ENV MODEL_PATH=""
ENV TOKEN_QUOTA="100000"
ENV KV_CACHE_GB="4"
ENV USE_GPU="true"

# ── Torch / HuggingFace cache dirs ────────────────────────────────────────────
# torch reads /etc/passwd at import time to resolve UID→username for cache dirs.
# Pinning these to /tmp (which is an emptyDir mount) avoids any filesystem issue
# and works whether running with GPU or in CPU-fallback mode.
ENV HOME="/tmp"
ENV TORCH_HOME="/tmp/torch"
ENV TORCHINDUCTOR_CACHE_DIR="/tmp/torchinductor"
ENV TRITON_CACHE_DIR="/tmp/triton"
ENV HF_HOME="/tmp/huggingface"
ENV TRANSFORMERS_CACHE="/tmp/huggingface"
ENV HF_DATASETS_CACHE="/tmp/huggingface/datasets"
ENV XDG_CACHE_HOME="/tmp/cache"

# Health Check & Startup Validation
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["python3.11", "-m", "uvicorn", "server:app", \
            "--host", "0.0.0.0", \
            "--port", "8080", \
            "--workers", "1", \
            "--log-level", "info"]
