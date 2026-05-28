"""
Aperture AI — Load Test Benchmark

Simulates concurrent request loads against the inference server at
10, 50, and 100 concurrent users. Records P50, P95, and P99 latencies.

Usage:
    python load_test.py [--url URL] [--output FILE]
"""

from __future__ import annotations

import argparse
import csv
import logging
import os
import statistics
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import httpx

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("aperture.benchmark.load")

DEFAULT_URL = os.environ.get("INFERENCE_URL", "http://localhost:8080")
DEFAULT_OUTPUT = "results/load_test_results.csv"
CONCURRENCY_LEVELS = [10, 50, 100]
PROMPT = "What is the attention mechanism in transformers?"
MAX_TOKENS = 30


@dataclass
class RequestResult:
    concurrency: int
    request_id: int
    latency_ms: float
    tokens: int
    success: bool
    error: str = ""
    timestamp: str = ""


@dataclass
class LevelSummary:
    concurrency: int
    total_requests: int
    successful: int
    failed: int
    p50_ms: float
    p95_ms: float
    p99_ms: float
    mean_ms: float
    min_ms: float
    max_ms: float
    throughput_rps: float


def send_request(client: httpx.Client, url: str, concurrency: int, req_id: int) -> RequestResult:
    payload = {"prompt": PROMPT, "max_tokens": MAX_TOKENS}
    ts = datetime.now(timezone.utc).isoformat()
    start = time.perf_counter()
    try:
        resp = client.post(f"{url}/generate", json=payload, timeout=180.0)
        elapsed = (time.perf_counter() - start) * 1000.0
        resp.raise_for_status()
        data = resp.json()
        return RequestResult(
            concurrency=concurrency, request_id=req_id,
            latency_ms=round(elapsed, 3),
            tokens=data.get("tokens_generated", 0),
            success=True, timestamp=ts,
        )
    except Exception as exc:
        elapsed = (time.perf_counter() - start) * 1000.0
        return RequestResult(
            concurrency=concurrency, request_id=req_id,
            latency_ms=round(elapsed, 3), tokens=0,
            success=False, error=str(exc), timestamp=ts,
        )


def percentile(sorted_data: list[float], pct: float) -> float:
    idx = int(len(sorted_data) * pct / 100.0)
    idx = min(idx, len(sorted_data) - 1)
    return sorted_data[idx]


def run_load_level(url: str, concurrency: int) -> tuple[list[RequestResult], LevelSummary]:
    logger.info("Running load test: concurrency=%d", concurrency)
    results: list[RequestResult] = []
    start_all = time.perf_counter()

    with httpx.Client() as client:
        with ThreadPoolExecutor(max_workers=concurrency) as pool:
            futures = {
                pool.submit(send_request, client, url, concurrency, i): i
                for i in range(1, concurrency + 1)
            }
            for future in as_completed(futures):
                results.append(future.result())

    total_time = time.perf_counter() - start_all
    ok = [r for r in results if r.success]
    latencies = sorted(r.latency_ms for r in ok) if ok else [0.0]

    summary = LevelSummary(
        concurrency=concurrency,
        total_requests=len(results),
        successful=len(ok),
        failed=len(results) - len(ok),
        p50_ms=round(percentile(latencies, 50), 3) if ok else 0,
        p95_ms=round(percentile(latencies, 95), 3) if ok else 0,
        p99_ms=round(percentile(latencies, 99), 3) if ok else 0,
        mean_ms=round(statistics.mean(latencies), 3) if ok else 0,
        min_ms=round(min(latencies), 3) if ok else 0,
        max_ms=round(max(latencies), 3) if ok else 0,
        throughput_rps=round(len(ok) / total_time, 3) if total_time > 0 else 0,
    )

    logger.info(
        "  concurrency=%d | ok=%d fail=%d | P50=%.1fms P95=%.1fms P99=%.1fms | rps=%.1f",
        concurrency, summary.successful, summary.failed,
        summary.p50_ms, summary.p95_ms, summary.p99_ms, summary.throughput_rps,
    )
    return results, summary


def run_benchmark(url: str, output_path: Path) -> None:
    all_results: list[RequestResult] = []
    summaries: list[LevelSummary] = []

    # Warm-up
    logger.info("Warm-up request...")
    with httpx.Client() as c:
        r = send_request(c, url, 0, 0)
        if not r.success:
            logger.error("Warm-up failed: %s — ensure server is running at %s", r.error, url)
            return
    logger.info("Warm-up OK")

    for level in CONCURRENCY_LEVELS:
        results, summary = run_load_level(url, level)
        all_results.extend(results)
        summaries.append(summary)

    # Write per-request CSV
    output_path.parent.mkdir(parents=True, exist_ok=True)
    fields = ["concurrency", "request_id", "latency_ms", "tokens", "success", "error", "timestamp"]
    with open(output_path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for r in all_results:
            w.writerow({
                "concurrency": r.concurrency, "request_id": r.request_id,
                "latency_ms": r.latency_ms, "tokens": r.tokens,
                "success": r.success, "error": r.error, "timestamp": r.timestamp,
            })
    logger.info("Per-request results saved to %s", output_path)

    # Write summary CSV
    summary_path = output_path.parent / "load_test_summary.csv"
    s_fields = [
        "concurrency", "total_requests", "successful", "failed",
        "p50_ms", "p95_ms", "p99_ms", "mean_ms", "min_ms", "max_ms", "throughput_rps",
    ]
    with open(summary_path, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=s_fields)
        w.writeheader()
        for s in summaries:
            w.writerow({
                "concurrency": s.concurrency, "total_requests": s.total_requests,
                "successful": s.successful, "failed": s.failed,
                "p50_ms": s.p50_ms, "p95_ms": s.p95_ms, "p99_ms": s.p99_ms,
                "mean_ms": s.mean_ms, "min_ms": s.min_ms, "max_ms": s.max_ms,
                "throughput_rps": s.throughput_rps,
            })
    logger.info("Summary saved to %s", summary_path)

    # Print final summary table
    logger.info("=" * 70)
    logger.info("Load Test Summary")
    logger.info("=" * 70)
    logger.info("%-12s %-8s %-8s %-10s %-10s %-10s %-8s",
                "Concurrency", "OK", "Fail", "P50(ms)", "P95(ms)", "P99(ms)", "RPS")
    logger.info("-" * 70)
    for s in summaries:
        logger.info("%-12d %-8d %-8d %-10.1f %-10.1f %-10.1f %-8.1f",
                     s.concurrency, s.successful, s.failed,
                     s.p50_ms, s.p95_ms, s.p99_ms, s.throughput_rps)
    logger.info("=" * 70)


def main() -> None:
    parser = argparse.ArgumentParser(description="Aperture AI — Load Test Benchmark")
    parser.add_argument("--url", default=DEFAULT_URL, help="Inference server URL")
    parser.add_argument("--output", default=DEFAULT_OUTPUT, help="Output CSV path")
    args = parser.parse_args()
    run_benchmark(args.url, Path(args.output))


if __name__ == "__main__":
    main()
