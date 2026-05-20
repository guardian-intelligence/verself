"""Pydantic v2 base for generated response models (DESIGN §5).

`_request_id` exposes the `x-request-id` of the originating response
(RESEARCH §4 #5). Generated models subclass this; field aliasing lets the
wire name (`currentTime`) differ from the idiomatic Python name
(`current_time`) the emitter chooses.
"""

from __future__ import annotations

from typing import Any, Optional

import pydantic

__all__ = ["BaseModel", "to_jsonable"]


def to_jsonable(obj: Any) -> Any:
    """Recursively turn request inputs (models / lists / dicts) into JSON.

    Accepts pydantic models *or* plain dicts (the Stainless-style typed-dict
    input), so generated method kwargs serialize correctly either way.
    """
    if isinstance(obj, pydantic.BaseModel):
        return obj.model_dump(by_alias=True, exclude_none=True)
    if isinstance(obj, dict):
        return {k: to_jsonable(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [to_jsonable(v) for v in obj]
    return obj


class BaseModel(pydantic.BaseModel):
    model_config = pydantic.ConfigDict(
        populate_by_name=True,   # accept both wire alias and python name
        extra="allow",           # forward-compatible: unknown fields don't break
    )

    # Set by the client after construction; not a wire field.
    _request_id: Optional[str] = pydantic.PrivateAttr(default=None)
