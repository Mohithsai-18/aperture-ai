# Aperture AI — Local Execution & Endpoint Screenshots

This document showcases the local execution of the Aperture AI inference server running in CPU fallback mode (due to local CPU/Windows environment limits). 

Below are the screenshots capturing the main system endpoints, API schemas, health checks, and prometheus exposition metrics.

---

## 1. Interactive Swagger API Documentation (`/docs`)

FastAPI's interactive Swagger UI documentation showing all registered endpoints:
- `POST /generate` (Text Generation)
- `GET /health` (System Health & Uptime)
- `GET /metrics` (Prometheus Metrics Exposition)
- `GET /metrics/json` (JSON Dashboard Metrics)

![Aperture AI Swagger API Documentation](docs/images/swagger_docs.png)

---

## 2. Server Health Check (`/health`)

The health check response endpoint. Since the server runs locally on Windows without CUDA and the `vllm` package (which is Linux-only), it dynamically activates **CPU fallback mode** to ensure service continuity. It shows the status, target model name, total tokens generated, token quota, and server uptime.

![Aperture AI Health Check Endpoint](docs/images/health_check.png)

---

## 3. Prometheus Metrics (`/metrics`)

The Prometheus metrics exposition endpoint returning the system's operational metrics in standard Prometheus text format (`text/plain`). These metrics are periodically scraped by the cluster monitoring stack for Grafana dashboards:

- `aperture_requests_total`: Total number of requests processed.
- `aperture_tokens_generated_total`: Total count of generated tokens.
- `aperture_token_quota`: Total assigned token quota.
- `aperture_token_quota_remaining`: Dynamic remaining quota before throttling.
- `aperture_avg_request_latency_ms`: Running average latency of the requests.
- `aperture_uptime_seconds`: Running uptime.
- `aperture_engine_loaded`: Indicator if vLLM GPU engine is active (`1`) or CPU fallback (`0`).

![Aperture AI Prometheus Metrics](docs/images/metrics.png)

---

## 4. Grafana Observability Dashboard

A dynamic Grafana dashboard that provides real-time visualization of system metrics scraped from the `/metrics` endpoint. The dashboard contains panels for:
- **Total Requests**: Real-time counter of total inference requests processed.
- **Tokens Generated**: Cumulative and instantaneous rate of token generation.
- **Average Latency**: Average and percentile (P50, P95, P99) requests latency.
- **Quota Throttling**: Gauge showing remaining token quota and usage.
- **Engine Status**: Active tracking of model loaded status (CPU fallback vs. GPU active).
- **System Uptime & Resource Consumption**: Tracks host memory/CPU usage and system up time.

![Aperture AI Grafana Dashboard](docs/images/grafana_dashboard.png)

