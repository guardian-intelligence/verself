"""Shared diagnostics. Precise, quotable errors > hidden exception flow.

Every user-facing failure carries source position when we have it (ruamel.yaml
gives us line/column), so messages can point at the exact offending line.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class SourceLoc:
    file: str
    line: int | None = None
    col: int | None = None

    def __str__(self) -> str:
        if self.line is None:
            return self.file
        if self.col is None:
            return f"{self.file}:{self.line}"
        return f"{self.file}:{self.line}:{self.col}"


class StainfulError(Exception):
    """Base for all stainful errors. Carries an optional source location."""

    def __init__(self, message: str, loc: SourceLoc | None = None) -> None:
        self.loc = loc
        super().__init__(f"{loc}: {message}" if loc else message)


class ConfigError(StainfulError):
    """Invalid or unsupported stainful.yml / stainless.yml."""


class SpecError(StainfulError):
    """Invalid or unsupported OpenAPI document."""


class IRBuildError(StainfulError):
    """Spec + config could not be reconciled into a coherent IR."""
