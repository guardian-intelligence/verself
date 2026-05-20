"""`with_raw_response` / `with_streaming_response` method wrappers (§5a).

Symbol- and signature-compatible with openai-python. v1 cutline (DESIGN §5b):
the wrappers exist, are callable, and preserve the wrapped signature. The
richer `APIResponse` object (`.parse()`, `.headers`, `.http_response`) is v1.1
polish — the drop-in *contract* is the symbol surface, pinned now.
"""

from __future__ import annotations

import functools
from typing import Any, Callable

__all__ = [
    "to_raw_response_wrapper",
    "async_to_raw_response_wrapper",
    "to_streamed_response_wrapper",
    "async_to_streamed_response_wrapper",
]


def to_raw_response_wrapper(func: Callable[..., Any]) -> Callable[..., Any]:
    @functools.wraps(func)
    def wrapped(*args: Any, **kwargs: Any) -> Any:
        return func(*args, **kwargs)

    return wrapped


def async_to_raw_response_wrapper(func: Callable[..., Any]) -> Callable[..., Any]:
    @functools.wraps(func)
    async def wrapped(*args: Any, **kwargs: Any) -> Any:
        return await func(*args, **kwargs)

    return wrapped


def to_streamed_response_wrapper(func: Callable[..., Any]) -> Callable[..., Any]:
    @functools.wraps(func)
    def wrapped(*args: Any, **kwargs: Any) -> Any:
        return func(*args, **kwargs)

    return wrapped


def async_to_streamed_response_wrapper(func: Callable[..., Any]) -> Callable[..., Any]:
    @functools.wraps(func)
    async def wrapped(*args: Any, **kwargs: Any) -> Any:
        return await func(*args, **kwargs)

    return wrapped
