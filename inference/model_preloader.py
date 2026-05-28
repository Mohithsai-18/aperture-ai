"""
Aperture AI — Model Pre-loader Init Container

Downloads HuggingFace model weights to a shared PVC before the inference
server starts. Skips download if the model is already cached.

Usage (inside init container):
    python model_preloader.py

Environment Variables:
    MODEL_PATH   — HuggingFace model identifier (required).
    CACHE_DIR    — Directory to cache models (default: /models).
    HF_TOKEN     — HuggingFace API token for gated models (optional).
"""

from __future__ import annotations

import hashlib
import logging
import os
import sys
import time
from pathlib import Path

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("aperture.preloader")


def get_model_cache_path(cache_dir: str, model_name: str) -> Path:
    """Generate a deterministic cache path for a model name."""
    safe_name = model_name.replace("/", "--")
    return Path(cache_dir) / safe_name


def is_model_cached(cache_path: Path) -> bool:
    """Check if a model is already fully downloaded to the cache."""
    if not cache_path.exists():
        return False

    # Check for HuggingFace snapshot marker files.
    config_file = cache_path / "config.json"
    model_files = list(cache_path.glob("*.safetensors")) + list(cache_path.glob("*.bin"))

    if config_file.exists() and len(model_files) > 0:
        logger.info("Model cache hit: %s (%d weight files found)", cache_path, len(model_files))
        return True

    logger.info("Model cache incomplete: config=%s, weight_files=%d",
                config_file.exists(), len(model_files))
    return False


def download_model(model_name: str, cache_path: Path, hf_token: str | None) -> None:
    """Download model from HuggingFace Hub to the cache directory."""
    try:
        from huggingface_hub import snapshot_download
    except ImportError:
        logger.error("huggingface_hub is not installed. Install it with: pip install huggingface-hub")
        sys.exit(1)

    logger.info("Downloading model '%s' to '%s'...", model_name, cache_path)
    start = time.perf_counter()

    try:
        snapshot_download(
            repo_id=model_name,
            local_dir=str(cache_path),
            local_dir_use_symlinks=False,
            token=hf_token,
            resume_download=True,
        )
    except Exception as exc:
        logger.exception("Failed to download model '%s'", model_name)
        sys.exit(1)

    elapsed = time.perf_counter() - start
    total_size = sum(f.stat().st_size for f in cache_path.rglob("*") if f.is_file())
    total_size_gb = total_size / (1024 ** 3)

    logger.info(
        "Model downloaded successfully: %.2f GB in %.1f seconds (%.1f MB/s)",
        total_size_gb, elapsed,
        (total_size / (1024 ** 2)) / elapsed if elapsed > 0 else 0,
    )


def create_ready_marker(cache_path: Path) -> None:
    """Create a marker file indicating the model is fully cached."""
    marker = cache_path / ".aperture-ready"
    marker.write_text(f"cached_at={time.time()}\n", encoding="utf-8")
    logger.info("Ready marker written: %s", marker)


def main() -> None:
    model_name = os.environ.get("MODEL_PATH", "")
    cache_dir = os.environ.get("CACHE_DIR", "/models")
    hf_token = os.environ.get("HF_TOKEN")

    if not model_name:
        logger.error("MODEL_PATH environment variable is required")
        sys.exit(1)

    cache_path = get_model_cache_path(cache_dir, model_name)
    logger.info("Model: %s", model_name)
    logger.info("Cache path: %s", cache_path)

    if is_model_cached(cache_path):
        logger.info("Model already cached — skipping download")
        create_ready_marker(cache_path)
        return

    cache_path.mkdir(parents=True, exist_ok=True)
    download_model(model_name, cache_path, hf_token)
    create_ready_marker(cache_path)
    logger.info("Pre-loader complete")


if __name__ == "__main__":
    main()
