"""
Aperture AI — Time To First Token (TTFT) Benchmark

Measures the latency from request submission to response received
from the inference server. Results are saved as CSV.

Usage:
    python ttft_measure.py [--url URL] [--runs N] [--output FILE]
"""

from __future__ import annotations

import argparse
import csv
import logging
import os
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

import httpx

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("aperture.benchmark.ttft")

DEFAULT_URL = os.environ.get("INFERENCE_URL", "http://localhost:8080")
DEFAULT_PROMPT = "Explain the architecture of a transformer model in detail."
DEFAULT_RUNS = 20
DEFAULT_OUTPUT = "results/ttft_results.csv"


@dataclass
class TTFTResult:
    run_id: int
    prompt_length: int
    max_tokens: int
    ttft_ms: float
    tokens_generated: int
    total_time_ms: float
    timestamp: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )
    error: str = ""


def measure_ttft(
    client: httpx.Client, url: str, prompt: str, max_tokens: int = 50
) -> TTFTResult:
    """Measure TTFT by timing the request to /generate."""
    payload = {"prompt": prompt, "max_tokens": max_tokens}
    start = time.perf_counter()
    try:
        resp = client.post(f"{url}/generate", json=payload, timeout=120.0)
        ttft = time.perf_counter()
        resp.raise_for_status()
        data = resp.json()
        ttft_ms = (ttft - start) * 1000.0
        return TTFTResult(
            run_id=0,
            prompt_length=len(prompt),
            max_tokens=max_tokens,
            ttft_ms=round(ttft_ms, 3),
            tokens_generated=data.get("tokens_generated", 0),
            total_time_ms=round(ttft_ms, 3),
        )
    except Exception as exc:
        return TTFTResult(
            run_id=0,
            prompt_length=len(prompt),
            max_tokens=max_tokens,
            ttft_ms=-1.0,
            tokens_generated=0,
            total_time_ms=-1.0,
            error=str(exc),
        )


def write_csv(results: list[TTFTResult], output_path: Path) -> None:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    fields = [
        "run_id", "prompt_length", "max_tokens", "ttft_ms",
        "tokens_generated", "total_time_ms", "timestamp", "error",
    ]
    with open(output_path, "w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fields)
        writer.writeheader()
        for r in results:
            writer.writerow({
                "run_id": r.run_id, "prompt_length": r.prompt_length,
                "max_tokens": r.max_tokens, "ttft_ms": r.ttft_ms,
                "tokens_generated": r.tokens_generated,
                "total_time_ms": r.total_time_ms,
                "timestamp": r.timestamp, "error": r.error,
            })
    logger.info("Results saved to %s", output_path)


def percentile(sorted_data: list[float], pct: float) -> float:
    idx = min(int(len(sorted_data) * pct / 100.0), len(sorted_data) - 1)
    return sorted_data[idx]


def run_benchmark(
    url: str, prompt: str, runs: int, max_tokens: int, output_path: Path
) -> list[TTFTResult]:
    results: list[TTFTResult] = []
    logger.info("TTFT benchmark: url=%s, runs=%d, max_tokens=%d", url, runs, max_tokens)

    # Warm-up
    logger.info("Sending warm-up request...")
    with httpx.Client() as client:
        warmup = measure_ttft(client, url, prompt, max_tokens)
        if warmup.error:
            logger.error("Warm-up failed: %s", warmup.error)
            return results
        logger.info("Warm-up OK: ttft=%.2fms", warmup.ttft_ms)

    # Benchmark runs
    with httpx.Client() as client:
        for i in range(1, runs + 1):
            result = measure_ttft(client, url, prompt, max_tokens)
            result.run_id = i
            if result.error:
                logger.warning("Run %d/%d FAILED: %s", i, runs, result.error)
            else:
                logger.info("Run %d/%d: ttft=%.2fms, tokens=%d",
                            i, runs, result.ttft_ms, result.tokens_generated)
            results.append(result)

    write_csv(results, output_path)

    # Summary
    ok = [r for r in results if not r.error]
    if ok:
        ttfts = sorted(r.ttft_ms for r in ok)
        logger.info("=" * 60)
        logger.info("TTFT Benchmark Summary")
        logger.info("=" * 60)
        logger.info("  Runs: %d/%d successful", len(ok), runs)
        logger.info("  Min:  %.2f ms", min(ttfts))
        logger.info("  Max:  %.2f ms", max(ttfts))
        logger.info("  Mean: %.2f ms", sum(ttfts) / len(ttfts))
        logger.info("  P50:  %.2f ms", percentile(ttfts, 50))
        logger.info("  P95:  %.2f ms", percentile(ttfts, 95))
        logger.info("  P99:  %.2f ms", percentile(ttfts, 99))
        logger.info("=" * 60)
    else:
        logger.error("All runs failed.")

    return results


def main() -> None:
    parser = argparse.ArgumentParser(description="Aperture AI — TTFT Benchmark")
    parser.add_argument("--url", default=DEFAULT_URL)
    parser.add_argument("--prompt", default=DEFAULT_PROMPT)
    parser.add_argument("--runs", type=int, default=DEFAULT_RUNS)
    parser.add_argument("--max-tokens", type=int, default=50)
    parser.add_argument("--output", default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    run_benchmark(args.url, args.prompt, args.runs, args.max_tokens, Path(args.output))


if __name__ == "__main__":
    main()
