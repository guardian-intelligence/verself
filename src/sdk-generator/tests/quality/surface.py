"""Extract the public API *surface* of a Python SDK package via stdlib `ast`.

No imports of the target code (safe, no side effects), comment/whitespace
insensitive. We key symbols by identity that matters for drop-in compat —
class name and method name — not by file layout, because the real Stainless
SDK splits models per-file while stainful v1 emits one `models.py` (a known,
deliberate difference that must not count as a fidelity loss).
"""

from __future__ import annotations

import ast
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class Method:
    name: str
    posonly: tuple[str, ...]
    args: tuple[str, ...]          # normal positional-or-keyword
    kwonly: tuple[str, ...]
    returns: str | None            # ast.unparse of the return annotation
    is_async: bool

    def structural(self) -> str:
        """Signature identity ignoring annotations/defaults (drop-in shape)."""
        kw = ",".join(sorted(self.kwonly))
        return f"({','.join((*self.posonly, *self.args))}|*{kw})->{self.returns or ''}"


@dataclass
class Class:
    name: str
    bases: tuple[str, ...]
    methods: dict[str, Method] = field(default_factory=dict)
    module: str = ""


@dataclass
class Surface:
    classes: dict[str, Class] = field(default_factory=dict)   # by class name
    functions: dict[str, Method] = field(default_factory=dict)
    exports: set[str] = field(default_factory=set)            # __init__ __all__

    # --- derived views the comparator scores on --------------------------
    def exceptions(self) -> set[str]:
        """Class names that are exceptions (by base-chain or -Error suffix)."""
        out: set[str] = set()
        for c in self.classes.values():
            if c.name.endswith("Error") or c.name.endswith("Exception"):
                out.add(c.name)
            elif any(b.split(".")[-1] in _EXC_BASES for b in c.bases):
                out.add(c.name)
        return out

    def resource_classes(self) -> dict[str, Class]:
        return {
            n: c for n, c in self.classes.items()
            if any("APIResource" in b for b in c.bases)
        }


_EXC_BASES = {
    "Exception", "APIError", "APIStatusError", "APIConnectionError",
    "OpenAIError", "AnthropicError", "OnebusawaySDKError", "StainfulError",
}


def _method(node: ast.AST) -> Method | None:
    if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
        return None
    a = node.args
    ret = ast.unparse(node.returns) if node.returns is not None else None
    return Method(
        name=node.name,
        posonly=tuple(p.arg for p in a.posonlyargs),
        args=tuple(p.arg for p in a.args),
        kwonly=tuple(p.arg for p in a.kwonlyargs),
        returns=ret,
        is_async=isinstance(node, ast.AsyncFunctionDef),
    )


def _all_names(tree: ast.Module) -> set[str]:
    for n in ast.walk(tree):
        if isinstance(n, ast.Assign) and any(
            isinstance(t, ast.Name) and t.id == "__all__" for t in n.targets
        ):
            if isinstance(n.value, (ast.List, ast.Tuple)):
                return {
                    e.value for e in n.value.elts
                    if isinstance(e, ast.Constant) and isinstance(e.value, str)
                }
    return set()


def extract(package_dir: str | Path) -> Surface:
    """Build a Surface from a package directory (e.g. .../src/onebusaway)."""
    root = Path(package_dir)
    surf = Surface()
    for py in sorted(root.rglob("*.py")):
        rel = py.relative_to(root).as_posix()
        try:
            tree = ast.parse(py.read_text(), filename=str(py))
        except SyntaxError:
            continue
        if py.name == "__init__.py" and py.parent == root:
            surf.exports |= _all_names(tree)
        for node in tree.body:
            if isinstance(node, ast.ClassDef):
                if node.name.startswith("_"):
                    continue
                cls = Class(
                    name=node.name,
                    bases=tuple(ast.unparse(b) for b in node.bases),
                    module=rel,
                )
                for sub in node.body:
                    m = _method(sub)
                    if m and not m.name.startswith("_"):
                        cls.methods[m.name] = m
                # last definition wins; SDKs don't redefine public classes
                surf.classes[node.name] = cls
            elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                if not node.name.startswith("_"):
                    m = _method(node)
                    if m:
                        surf.functions[m.name] = m
            elif isinstance(node, ast.Assign):
                # Top-level `PublicName = SomeClass` alias (e.g. the brand-root
                # `OnebusawaySDKError = APIError`) is a real catchable symbol
                # for drop-in purposes — record it as a class aliasing its base.
                if (
                    len(node.targets) == 1
                    and isinstance(node.targets[0], ast.Name)
                    and isinstance(node.value, ast.Name)
                    and not node.targets[0].id.startswith("_")
                ):
                    nm = node.targets[0].id
                    surf.classes.setdefault(
                        nm, Class(name=nm, bases=(node.value.id,), module=rel)
                    )
    return surf
