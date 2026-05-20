"""The IR type system — the moat (DESIGN.md §3).

Language-agnostic, zero runtime deps (plain dataclasses). OpenAPI conflates
required/optional/nullable; this ADT keeps cardinality **3-valued and explicit**,
which is the single highest-leverage correctness decision in the project.

Nothing here knows about Python, pydantic, or httpx — that lives in emit/.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import Enum


class PrimitiveKind(str, Enum):
    STRING = "string"
    INTEGER = "integer"
    NUMBER = "number"
    BOOLEAN = "boolean"
    BYTES = "bytes"
    DATE = "date"
    DATETIME = "datetime"
    DECIMAL = "decimal"
    UUID = "uuid"


# --- Type ADT --------------------------------------------------------------
# `Type` is the union of the dataclasses below. We use a marker base purely for
# typing/isinstance; it carries no behavior (behavior belongs to the emitter).


class Type:
    """Marker base for the IR type ADT. No behavior by design."""


@dataclass(frozen=True)
class PrimitiveType(Type):
    kind: PrimitiveKind
    format: str | None = None


@dataclass(frozen=True)
class NullType(Type):
    pass


@dataclass(frozen=True)
class AnyType(Type):
    """Genuinely unconstrained (`{}` / no schema). Distinct from a loose object."""


@dataclass(frozen=True)
class EnumMember:
    name: str
    value: object


@dataclass(frozen=True)
class EnumType(Type):
    name: str
    base: PrimitiveType
    members: tuple[EnumMember, ...]


@dataclass(frozen=True)
class ArrayType(Type):
    item: Type


@dataclass(frozen=True)
class MapType(Type):
    """OpenAPI additionalProperties — homogeneous string-keyed map."""

    value: Type


@dataclass(frozen=True)
class Property:
    name: str
    type: Type
    # 3-valued cardinality — required, optional, nullable are orthogonal.
    # required == key always present; optional == key may be absent
    # (rendered with NotGiven sentinel, not None); nullable == value may be null.
    required: bool
    nullable: bool = False
    docs: str | None = None
    deprecated: bool = False
    default: object | None = None


@dataclass(frozen=True)
class ObjectType(Type):
    properties: tuple[Property, ...]
    # additionalProperties as a fallback value type, if the object is open.
    extra: Type | None = None
    # allOf members that resolve to *shared* models become base classes
    # (Stainless emits `class XResponse(ResponseWrapper): ...`), not a merge.
    bases: tuple[ModelRef, ...] = ()


@dataclass(frozen=True)
class Discriminator:
    property_name: str
    # tag value -> referenced model name
    mapping: dict[str, str]


@dataclass(frozen=True)
class UnionType(Type):
    """oneOf / anyOf. Tagged (real discriminated union) when `discriminator` is set."""

    variants: tuple[Type, ...]
    discriminator: Discriminator | None = None


@dataclass(frozen=True)
class ModelRef(Type):
    """Reference to a named Model. Also the cycle-breaker for recursive schemas."""

    name: str


@dataclass(frozen=True)
class Model:
    """A named type from `components` — becomes a pydantic class or alias."""

    name: str
    type: Type
    docs: str | None = None
