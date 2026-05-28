"""
Type stubs for vLLM — satisfies Pylance on Windows where vLLM cannot be installed.
vLLM is a Linux+CUDA-only package; these stubs exist solely for IDE support.
"""

from typing import Any, List, Optional, Sequence

class SamplingParams:
    def __init__(
        self,
        *,
        max_tokens: int = 16,
        temperature: float = 1.0,
        top_p: float = 1.0,
        top_k: int = -1,
        frequency_penalty: float = 0.0,
        presence_penalty: float = 0.0,
        stop: Optional[List[str]] = None,
        **kwargs: Any,
    ) -> None: ...

class CompletionOutput:
    text: str
    token_ids: Sequence[int]

class RequestOutput:
    outputs: List[CompletionOutput]

class LLM:
    def __init__(
        self,
        model: str,
        *,
        gpu_memory_utilization: float = 0.9,
        max_model_len: Optional[int] = None,
        trust_remote_code: bool = False,
        **kwargs: Any,
    ) -> None: ...

    def generate(
        self,
        prompts: List[str],
        sampling_params: Optional[SamplingParams] = None,
        **kwargs: Any,
    ) -> List[RequestOutput]: ...
