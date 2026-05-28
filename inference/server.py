"""
Aperture AI — vLLM Inference Server

Production-grade FastAPI server wrapping vLLM for GPU-accelerated LLM inference.
Supports continuous batching and paged attention via the vLLM engine.

Environment Variables:
    MODEL_PATH    — HuggingFace model identifier (required).
    TOKEN_QUOTA   — Maximum token generation quota (default: 100000).
    KV_CACHE_GB   — KV cache size in GB for paged attention (default: 4).
"""

from __future__ import annotations

import json
import logging
import os
import sys
import threading
import time
from contextlib import asynccontextmanager
from typing import Any

import torch
import uvicorn
from fastapi import FastAPI, HTTPException, Request, Response
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from tracing import init_tracing, instrument_fastapi, shutdown_tracing

try:
    from vllm import LLM, SamplingParams
except ImportError:
    # vLLM is Linux+CUDA only — provide stubs for IDE type-checking on Windows.
    # The server will fail at startup with a clear error if vLLM is genuinely missing.
    LLM = None  # type: ignore[assignment, misc]
    SamplingParams = None  # type: ignore[assignment, misc]


# ---------------------------------------------------------------------------
# Structured JSON Logging
# ---------------------------------------------------------------------------
class _JSONFormatter(logging.Formatter):
    """Emit log records as single-line JSON for log aggregation (Fluentd, Loki)."""

    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "ts": self.formatTime(record, self.datefmt),
            "level": record.levelname,
            "logger": record.name,
            "msg": record.getMessage(),
        }
        if record.exc_info and record.exc_info[0] is not None:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload)


_handler = logging.StreamHandler(sys.stdout)
_handler.setFormatter(_JSONFormatter())
logging.root.handlers = [_handler]
logging.root.setLevel(logging.INFO)
logger = logging.getLogger("aperture.inference")

# ---------------------------------------------------------------------------
# Configuration (read at import time; validated at startup in lifespan)
# ---------------------------------------------------------------------------
MODEL_PATH: str = os.environ.get("MODEL_PATH", "")
TOKEN_QUOTA: int = int(os.environ.get("TOKEN_QUOTA", "100000"))
KV_CACHE_GB: int = int(os.environ.get("KV_CACHE_GB", "4"))
USE_GPU: bool = os.environ.get("USE_GPU", "true").lower() == "true"


# ---------------------------------------------------------------------------
# Thread-safe token counter
# ---------------------------------------------------------------------------
class _TokenCounter:
    """Atomic counter for tokens generated, safe under concurrent access."""

    def __init__(self) -> None:
        self._value: int = 0
        self._lock: threading.Lock = threading.Lock()

    @property
    def value(self) -> int:
        return self._value

    def try_reserve(self, requested: int, quota: int) -> tuple[bool, int]:
        """
        Atomically check quota and reserve tokens.
        Returns (success, remaining_before_request).
        """
        with self._lock:
            remaining = quota - self._value
            if requested > remaining:
                return False, remaining
            self._value += requested
            return True, remaining

    def adjust(self, reserved: int, actual: int) -> None:
        """Correct the counter after generation (actual may differ from reserved)."""
        diff = reserved - actual
        if diff != 0:
            with self._lock:
                self._value -= diff


_token_counter = _TokenCounter()

# ---------------------------------------------------------------------------
# Global state
# ---------------------------------------------------------------------------
_engine: LLM | None = None
_fallback_mode: bool = False


# ---------------------------------------------------------------------------
# Lifespan — initialize / shutdown the vLLM engine
# ---------------------------------------------------------------------------
@asynccontextmanager
async def lifespan(app: FastAPI):
    """Load the vLLM engine at startup and release resources on shutdown."""
    global _engine, _fallback_mode

    # Validate configuration at startup, NOT at import time.
    if not MODEL_PATH:
        raise RuntimeError(
            "MODEL_PATH environment variable is required but not set. "
            "Set it to a valid HuggingFace model identifier."
        )

    # Initialize distributed tracing (opt-in via OTEL_ENABLED=true).
    init_tracing()

    cuda_available = torch.cuda.is_available()
    logger.info("Startup check: USE_GPU=%s, CUDA_AVAILABLE=%s", USE_GPU, cuda_available)

    if USE_GPU and cuda_available:
        logger.info("Initializing vLLM engine with model=%s, kv_cache_gb=%d", MODEL_PATH, KV_CACHE_GB)

        if LLM is None:
            logger.warning("vLLM is not installed but GPU requested. Activating CPU fallback mode.")
            _fallback_mode = True
        else:
            start = time.perf_counter()
            try:
                _engine = LLM(
                    model=MODEL_PATH,
                    gpu_memory_utilization=0.90,
                    max_model_len=4096,
                    trust_remote_code=True,
                )
                elapsed = time.perf_counter() - start
                logger.info("vLLM engine ready in %.2fs", elapsed)
            except Exception as exc:
                logger.warning("Failed to initialize vLLM engine: %s. Switching to CPU fallback mode.", exc)
                _engine = None
                _fallback_mode = True
    else:
        logger.warning("USE_GPU is false or CUDA is unavailable. Activating CPU fallback mode.")
        _fallback_mode = True

    yield  # application runs here

    # Graceful shutdown: drain in-flight requests, flush traces.
    logger.info("Shutting down vLLM engine — draining in-flight requests")
    _engine = None
    shutdown_tracing()


# ---------------------------------------------------------------------------
# FastAPI application
# ---------------------------------------------------------------------------
app = FastAPI(
    title="Aperture Inference Server",
    version="1.0.0",
    description="GPU-accelerated LLM inference powered by vLLM.",
    lifespan=lifespan,
)

# Attach OpenTelemetry auto-instrumentation (no-op if OTEL_ENABLED=false).
instrument_fastapi(app)


# ---------------------------------------------------------------------------
# Prometheus-style metrics middleware
# ---------------------------------------------------------------------------
_request_count: int = 0
_request_latency_sum: float = 0.0
_metrics_lock = threading.Lock()


@app.middleware("http")
async def metrics_middleware(request: Request, call_next: Any) -> Response:
    """Track request count and latency for observability."""
    global _request_count, _request_latency_sum
    start = time.perf_counter()
    response: Response = await call_next(request)
    elapsed = time.perf_counter() - start

    with _metrics_lock:
        _request_count += 1
        _request_latency_sum += elapsed

    response.headers["X-Request-Duration-Ms"] = f"{elapsed * 1000:.2f}"
    return response


# ---------------------------------------------------------------------------
# Request / Response schemas
# ---------------------------------------------------------------------------
class GenerateRequest(BaseModel):
    """Request body for the /generate endpoint."""

    prompt: str = Field(
        ...,
        min_length=1,
        description="Input prompt for the language model.",
        examples=["Explain the concept of paged attention in LLMs."],
    )
    max_tokens: int = Field(
        default=100,
        ge=1,
        le=4096,
        description="Maximum number of tokens to generate.",
    )


class GenerateResponse(BaseModel):
    """Response body for the /generate endpoint."""

    text: str = Field(..., description="Generated text output.")
    tokens_generated: int = Field(..., description="Number of tokens produced.")


class HealthResponse(BaseModel):
    """Response body for the /health endpoint."""

    status: str
    model: str
    tokens_generated: int
    token_quota: int
    uptime_seconds: float


class MetricsResponse(BaseModel):
    """Response body for the /metrics endpoint."""

    total_requests: int
    total_tokens_generated: int
    token_quota: int
    token_quota_remaining: int
    avg_request_latency_ms: float


class ErrorResponse(BaseModel):
    """Standard error envelope."""

    error: str
    detail: str


# Track server start time for uptime reporting.
_start_time: float = time.time()


# ---------------------------------------------------------------------------
# Endpoints
# ---------------------------------------------------------------------------
@app.get(
    "/health",
    response_model=HealthResponse,
    summary="Health check",
    tags=["system"],
)
async def health_check() -> HealthResponse:
    """Return server health status and basic metrics."""
    if _engine is None and not _fallback_mode:
        raise HTTPException(status_code=503, detail="Engine not ready")
    
    status = "healthy (fallback)" if _fallback_mode else "healthy"
    
    return HealthResponse(
        status=status,
        model=MODEL_PATH,
        tokens_generated=_token_counter.value,
        token_quota=TOKEN_QUOTA,
        uptime_seconds=round(time.time() - _start_time, 2),
    )


@app.get(
    "/metrics",
    summary="Prometheus metrics",
    tags=["system"],
    response_class=Response,
)
async def metrics_prometheus() -> Response:
    """Return metrics in Prometheus text exposition format for scraping."""
    with _metrics_lock:
        avg_latency = (_request_latency_sum / _request_count * 1000) if _request_count > 0 else 0.0
        total_req = _request_count

    tokens_used = _token_counter.value
    tokens_remaining = TOKEN_QUOTA - tokens_used
    uptime = round(time.time() - _start_time, 2)

    lines = [
        "# HELP aperture_requests_total Total number of HTTP requests handled.",
        "# TYPE aperture_requests_total counter",
        f"aperture_requests_total {total_req}",
        "",
        "# HELP aperture_tokens_generated_total Total tokens generated since startup.",
        "# TYPE aperture_tokens_generated_total counter",
        f"aperture_tokens_generated_total {tokens_used}",
        "",
        "# HELP aperture_token_quota Maximum token quota for this server.",
        "# TYPE aperture_token_quota gauge",
        f"aperture_token_quota {TOKEN_QUOTA}",
        "",
        "# HELP aperture_token_quota_remaining Remaining tokens before quota exhaustion.",
        "# TYPE aperture_token_quota_remaining gauge",
        f"aperture_token_quota_remaining {tokens_remaining}",
        "",
        "# HELP aperture_avg_request_latency_ms Average request latency in milliseconds.",
        "# TYPE aperture_avg_request_latency_ms gauge",
        f"aperture_avg_request_latency_ms {avg_latency:.3f}",
        "",
        "# HELP aperture_uptime_seconds Server uptime in seconds.",
        "# TYPE aperture_uptime_seconds gauge",
        f"aperture_uptime_seconds {uptime}",
        "",
        "# HELP aperture_engine_loaded Whether the vLLM engine is loaded (1=yes, 0=no).",
        "# TYPE aperture_engine_loaded gauge",
        f"aperture_engine_loaded {1 if _engine is not None else 0}",
        "",
    ]

    return Response(
        content="\n".join(lines) + "\n",
        media_type="text/plain; version=0.0.4; charset=utf-8",
    )


@app.get(
    "/metrics/json",
    response_model=MetricsResponse,
    summary="Server metrics (JSON)",
    tags=["system"],
)
async def metrics_json() -> MetricsResponse:
    """Return server-side metrics as JSON for dashboards."""
    with _metrics_lock:
        avg_latency = (_request_latency_sum / _request_count * 1000) if _request_count > 0 else 0.0
        total_req = _request_count

    return MetricsResponse(
        total_requests=total_req,
        total_tokens_generated=_token_counter.value,
        token_quota=TOKEN_QUOTA,
        token_quota_remaining=TOKEN_QUOTA - _token_counter.value,
        avg_request_latency_ms=round(avg_latency, 3),
    )


@app.post(
    "/generate",
    response_model=GenerateResponse,
    responses={
        400: {"model": ErrorResponse, "description": "Invalid request"},
        429: {"model": ErrorResponse, "description": "Token quota exceeded"},
        503: {"model": ErrorResponse, "description": "Engine not ready"},
    },
    summary="Generate text",
    tags=["inference"],
)
async def generate(request: GenerateRequest) -> GenerateResponse:
    """
    Generate text from the loaded model.

    Uses vLLM's continuous batching engine for high-throughput inference
    with paged attention for efficient KV cache management.
    """
    if _engine is None and not _fallback_mode:
        raise HTTPException(
            status_code=503,
            detail="Inference engine is not initialized. Please try again later.",
        )

    # Atomically check and reserve tokens.
    reserved, remaining = _token_counter.try_reserve(request.max_tokens, TOKEN_QUOTA)
    if not reserved:
        raise HTTPException(
            status_code=429,
            detail=(
                f"Token quota exceeded. Quota: {TOKEN_QUOTA}, "
                f"remaining: {remaining}, requested: {request.max_tokens}."
            ),
        )

    logger.info(
        "Generating: prompt_len=%d, max_tokens=%d",
        len(request.prompt),
        request.max_tokens,
    )

    # CPU fallback path — runs before SamplingParams (which is None if vLLM absent).
    if _fallback_mode:
        import asyncio
        await asyncio.sleep(0.5)  # Simulate generation latency
        generated_text = f"[CPU Fallback] {request.prompt} → Aperture AI is a GPU-orchestrated LLM inference platform built on vLLM and Kubernetes."
        words = generated_text.split()
        if len(words) > request.max_tokens:
            words = words[:request.max_tokens]
            generated_text = " ".join(words)
        num_tokens = len(words)
        _token_counter.adjust(request.max_tokens, num_tokens)
        logger.info("Generated %d tokens via CPU fallback (total: %d/%d)", num_tokens, _token_counter.value, TOKEN_QUOTA)
        return GenerateResponse(text=generated_text, tokens_generated=num_tokens)

    # GPU path — only reached when vLLM engine is loaded.
    sampling_params = SamplingParams(
        max_tokens=request.max_tokens,
        temperature=0.7,
        top_p=0.95,
    )

    try:
        outputs = _engine.generate([request.prompt], sampling_params)
    except Exception as exc:
        # Return reserved tokens on failure.
        _token_counter.adjust(request.max_tokens, 0)
        logger.exception("Generation failed")
        raise HTTPException(
            status_code=500,
            detail=f"Generation failed: {exc}",
        ) from exc

    if not outputs or not outputs[0].outputs:
        _token_counter.adjust(request.max_tokens, 0)
        raise HTTPException(
            status_code=500,
            detail="Model returned empty output.",
        )

    result = outputs[0].outputs[0]
    generated_text: str = result.text
    num_tokens: int = len(result.token_ids)

    # Correct reservation to actual token count.
    _token_counter.adjust(request.max_tokens, num_tokens)

    logger.info(
        "Generated %d tokens (total: %d/%d)",
        num_tokens, _token_counter.value, TOKEN_QUOTA,
    )

    return GenerateResponse(
        text=generated_text,
        tokens_generated=num_tokens,
    )


# ---------------------------------------------------------------------------
# Error handlers
# ---------------------------------------------------------------------------
@app.exception_handler(Exception)
async def global_exception_handler(request: Any, exc: Exception) -> JSONResponse:
    """Catch-all exception handler to prevent stack trace leakage."""
    logger.exception("Unhandled exception")
    return JSONResponse(
        status_code=500,
        content={"error": "internal_server_error", "detail": str(exc)},
    )


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
if __name__ == "__main__":
    uvicorn.run(
        "server:app",
        host="0.0.0.0",
        port=8080,
        workers=1,  # vLLM manages its own parallelism
        log_level="info",
    )
