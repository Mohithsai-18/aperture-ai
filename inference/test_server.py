"""
Aperture AI — Inference Server Unit Tests

Tests for the FastAPI server endpoints, token counter, and request validation.
Run with: pytest test_server.py -v
"""

from __future__ import annotations

import os
import threading

import pytest

# Set MODEL_PATH before importing server (lifespan validates, not import).
os.environ.setdefault("MODEL_PATH", "facebook/opt-125m")
os.environ.setdefault("TOKEN_QUOTA", "1000")

# Import after env setup. Note: we cannot test /generate without vLLM,
# but we CAN test schemas, token counter, and health/metrics when engine is None.


class TestTokenCounter:
    """Tests for the thread-safe _TokenCounter class."""

    def _make_counter(self):
        # Import here to avoid circular issues with env setup.
        from server import _TokenCounter
        return _TokenCounter()

    def test_initial_value_is_zero(self):
        counter = self._make_counter()
        assert counter.value == 0

    def test_try_reserve_success(self):
        counter = self._make_counter()
        ok, remaining = counter.try_reserve(100, 1000)
        assert ok is True
        assert remaining == 1000
        assert counter.value == 100

    def test_try_reserve_exact_quota(self):
        counter = self._make_counter()
        ok, _ = counter.try_reserve(1000, 1000)
        assert ok is True
        assert counter.value == 1000

    def test_try_reserve_exceeds_quota(self):
        counter = self._make_counter()
        counter.try_reserve(900, 1000)
        ok, remaining = counter.try_reserve(200, 1000)
        assert ok is False
        assert remaining == 100
        assert counter.value == 900  # unchanged

    def test_adjust_reduces_overcount(self):
        counter = self._make_counter()
        counter.try_reserve(100, 1000)
        counter.adjust(reserved=100, actual=80)
        assert counter.value == 80

    def test_adjust_increases_undercount(self):
        counter = self._make_counter()
        counter.try_reserve(100, 1000)
        counter.adjust(reserved=100, actual=120)
        assert counter.value == 120

    def test_adjust_no_change(self):
        counter = self._make_counter()
        counter.try_reserve(100, 1000)
        counter.adjust(reserved=100, actual=100)
        assert counter.value == 100

    def test_concurrent_reservations(self):
        counter = self._make_counter()
        quota = 10000
        per_thread = 100
        num_threads = 50
        results = []

        def reserve():
            ok, _ = counter.try_reserve(per_thread, quota)
            results.append(ok)

        threads = [threading.Thread(target=reserve) for _ in range(num_threads)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        assert counter.value == per_thread * num_threads
        assert all(results)

    def test_concurrent_quota_enforcement(self):
        counter = self._make_counter()
        quota = 500
        per_thread = 100
        num_threads = 10  # 10 * 100 = 1000 > 500
        successes = []

        def reserve():
            ok, _ = counter.try_reserve(per_thread, quota)
            successes.append(ok)

        threads = [threading.Thread(target=reserve) for _ in range(num_threads)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        ok_count = sum(1 for s in successes if s)
        assert ok_count == 5  # exactly 500 / 100
        assert counter.value == 500


class TestRequestSchemas:
    """Tests for Pydantic request/response model validation."""

    def test_generate_request_valid(self):
        from server import GenerateRequest
        req = GenerateRequest(prompt="Hello world", max_tokens=50)
        assert req.prompt == "Hello world"
        assert req.max_tokens == 50

    def test_generate_request_defaults(self):
        from server import GenerateRequest
        req = GenerateRequest(prompt="Test")
        assert req.max_tokens == 100  # default

    def test_generate_request_empty_prompt_rejected(self):
        from server import GenerateRequest
        with pytest.raises(Exception):
            GenerateRequest(prompt="", max_tokens=10)

    def test_generate_request_max_tokens_too_high(self):
        from server import GenerateRequest
        with pytest.raises(Exception):
            GenerateRequest(prompt="Test", max_tokens=5000)

    def test_generate_request_max_tokens_zero(self):
        from server import GenerateRequest
        with pytest.raises(Exception):
            GenerateRequest(prompt="Test", max_tokens=0)

    def test_generate_response_model(self):
        from server import GenerateResponse
        resp = GenerateResponse(text="output text", tokens_generated=42)
        assert resp.text == "output text"
        assert resp.tokens_generated == 42

    def test_health_response_model(self):
        from server import HealthResponse
        resp = HealthResponse(
            status="healthy",
            model="test",
            tokens_generated=100,
            token_quota=1000,
            uptime_seconds=60.5,
        )
        assert resp.status == "healthy"
        assert resp.uptime_seconds == 60.5

    def test_metrics_response_model(self):
        from server import MetricsResponse
        resp = MetricsResponse(
            total_requests=10,
            total_tokens_generated=500,
            token_quota=1000,
            token_quota_remaining=500,
            avg_request_latency_ms=123.456,
        )
        assert resp.token_quota_remaining == 500


class TestFastAPIEndpoints:
    """Integration tests for HTTP endpoints (engine is None — tests error paths)."""

    @pytest.fixture
    def client(self):
        from fastapi.testclient import TestClient
        from server import app
        return TestClient(app, raise_server_exceptions=False)

    def test_health_returns_503_when_engine_not_loaded(self, client):
        resp = client.get("/health")
        assert resp.status_code == 503

    def test_generate_returns_503_when_engine_not_loaded(self, client):
        resp = client.post("/generate", json={"prompt": "Hello", "max_tokens": 10})
        assert resp.status_code == 503

    def test_metrics_returns_200(self, client):
        resp = client.get("/metrics")
        assert resp.status_code == 200
        assert "aperture_requests_total" in resp.text

    def test_metrics_json_returns_200(self, client):
        resp = client.get("/metrics/json")
        assert resp.status_code == 200
        data = resp.json()
        assert "total_requests" in data
        assert "token_quota" in data

    def test_generate_invalid_body_returns_422(self, client):
        resp = client.post("/generate", json={"wrong_field": "test"})
        assert resp.status_code == 422

    def test_openapi_schema_available(self, client):
        resp = client.get("/openapi.json")
        assert resp.status_code == 200
        schema = resp.json()
        assert schema["info"]["title"] == "Aperture Inference Server"
