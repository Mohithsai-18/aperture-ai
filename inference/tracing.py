import os
import logging
from contextlib import contextmanager

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.trace import NoOpTracerProvider, Status, StatusCode

logger = logging.getLogger(__name__)

_TRACER = None

def get_tracer():
    """
    Returns the configured tracer.
    If OTEL is disabled or fails to initialize, returns a no-op tracer.
    """
    global _TRACER
    if _TRACER is not None:
        return _TRACER
        
    enabled = os.environ.get("OTEL_ENABLED", "false").lower() == "true"
    if not enabled:
        trace.set_tracer_provider(NoOpTracerProvider())
        _TRACER = trace.get_tracer(__name__)
        return _TRACER
        
    try:
        endpoint = os.environ.get("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
        service_version = os.environ.get("APP_VERSION", "0.1.0")
        
        resource = Resource.create({
            "service.name": "aperture-inference",
            "service.version": service_version
        })
        
        provider = TracerProvider(resource=resource)
        exporter = OTLPSpanExporter(endpoint=endpoint)
        processor = BatchSpanProcessor(exporter)
        provider.add_span_processor(processor)
        
        trace.set_tracer_provider(provider)
        _TRACER = trace.get_tracer("aperture-inference")
        return _TRACER
    except Exception as e:
        logger.warning(f"Failed to initialize OpenTelemetry: {e}. Falling back to NoOp tracer.")
        trace.set_tracer_provider(NoOpTracerProvider())
        _TRACER = trace.get_tracer(__name__)
        return _TRACER

@contextmanager
def trace_request(prompt: str, model_name: str):
    """
    Context manager to trace an inference request.
    Records prompt length, model name, and partition mode.
    Safely catches exceptions and records them to the span.
    Safe fallback if OTEL fails.
    """
    tracer = get_tracer()
    partition_mode = os.environ.get("PARTITION_MODE", "unknown")
    
    try:
        span_cm = tracer.start_as_current_span("inference.generate")
    except Exception as e:
        logger.warning(f"Failed to start OTEL span: {e}")
        yield None
        return
        
    with span_cm as span:
        try:
            span.set_attribute("model.name", model_name)
            span.set_attribute("prompt.length", len(prompt) if prompt else 0)
            span.set_attribute("partition.mode", partition_mode)
        except Exception as e:
            logger.warning(f"Failed to set OTEL attributes: {e}")
            
        try:
            yield span
        except Exception as user_exc:
            try:
                span.record_exception(user_exc)
                span.set_status(Status(StatusCode.ERROR, str(user_exc)))
            except Exception as e:
                logger.warning(f"Failed to record OTEL exception: {e}")
            raise user_exc

def init_tracing():
    """Initialize tracer."""
    get_tracer()

def shutdown_tracing():
    """Shutdown tracer."""
    pass

def instrument_fastapi(app):
    """Instrument FastAPI app if OTEL is enabled."""
    enabled = os.environ.get("OTEL_ENABLED", "false").lower() == "true"
    if enabled:
        try:
            from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
            FastAPIInstrumentor.instrument_app(app)
        except ImportError:
            logger.warning("opentelemetry.instrumentation.fastapi not found.")
