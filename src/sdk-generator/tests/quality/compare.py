"""Score a generated SDK's surface against a real Stainless oracle.

Recall-oriented: "what fraction of the oracle's public surface does stainful
reproduce?" Drop-in compat is about the oracle's symbols still resolving, so
recall (not precision) is the contract. Precision is reported for visibility.
"""

from __future__ import annotations

from quality.surface import Surface


def _recall(have: set[str], want: set[str]) -> float:
    return 1.0 if not want else len(have & want) / len(want)


def fidelity(stainful: Surface, oracle: Surface) -> dict:
    # 1. catchable exception classes (config-independent drop-in contract)
    o_exc, s_exc = oracle.exceptions(), stainful.exceptions()
    exc_recall = _recall(s_exc, o_exc)

    # 2/3. resource classes + method signatures (config-dependent; only
    #      meaningful when the same stainless.yml drove both — OneBusAway).
    o_res, s_res = oracle.resource_classes(), stainful.resource_classes()
    o_methods: set[str] = set()
    matched = sig_ok = 0
    for cname, oc in o_res.items():
        sc = s_res.get(cname)
        for mname, om in oc.methods.items():
            o_methods.add(f"{cname}.{mname}")
            if sc and mname in sc.methods:
                matched += 1
                if sc.methods[mname].structural() == om.structural():
                    sig_ok += 1
    method_recall = (matched / len(o_methods)) if o_methods else 1.0
    sig_match = (sig_ok / matched) if matched else 1.0

    # 4. model classes by name (types/*) — name recall only in v1
    def models(s: Surface) -> set[str]:
        return {
            n for n, c in s.classes.items()
            if "types/" in c.module or c.module.startswith("types")
        }

    model_recall = _recall(models(stainful), models(oracle))

    # 5. top-level exported names (the literal `from pkg import X` surface)
    export_recall = _recall(stainful.exports, oracle.exports)

    missing_exc = sorted(o_exc - s_exc)
    return {
        "exception_recall": round(exc_recall, 4),
        "missing_exceptions": missing_exc,
        "resource_method_recall": round(method_recall, 4),
        "method_signature_match": round(sig_match, 4),
        "model_name_recall": round(model_recall, 4),
        "export_recall": round(export_recall, 4),
        "counts": {
            "oracle_exceptions": len(o_exc),
            "oracle_resource_methods": len(o_methods),
            "oracle_models": len(models(oracle)),
            "oracle_exports": len(oracle.exports),
        },
        # QUALITY_PLAN.md §2 acceptance targets, for visibility next to reality
        "targets": {
            "exception_recall": 0.99,
            "method_signature_match": 0.95,
        },
    }


def not_worse_than(report: dict, baseline: dict, eps: float = 1e-6) -> list[str]:
    """Return regressions (metric dropped below the checked-in baseline)."""
    keys = [
        "exception_recall", "resource_method_recall",
        "method_signature_match", "model_name_recall", "export_recall",
    ]
    bad = []
    for k in keys:
        if report.get(k, 0) + eps < baseline.get(k, 0):
            bad.append(f"{k}: {report.get(k)} < baseline {baseline.get(k)}")
    return bad
