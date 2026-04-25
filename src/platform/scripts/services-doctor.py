#!/usr/bin/env python3
"""Cross-check declared services against live listeners on the box.

Declared source: src/platform/ansible/group_vars/all/generated/services.yml
Live source: `sudo ss -Hltnp` on the inventory host (TCP listening sockets).

Output modes (FORMAT env):
    table     - default, human-readable drift report + undeclared listener list
    json      - structured dump of declared, listeners, and drift
    nftables  - suggested host-firewall rules derived from generated services (not
                authoritative; real rules live in roles/*/templates/*.nft.j2)

Exit codes:
    0 - no drift
    1 - drift found (missing listener, wrong bind, or undeclared listener in
        a product port range)
    2 - usage / infrastructure error (missing file, ssh failure, etc.)
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable

import yaml

REPO_ROOT = Path(__file__).resolve().parents[3]
SERVICES_YAML = REPO_ROOT / "src/platform/ansible/group_vars/all/generated/services.yml"
INVENTORY = REPO_ROOT / "src/platform/ansible/inventory/hosts.ini"

# Ports in these ranges are "ours" — an undeclared listener here is drift.
# Ports outside these ranges (e.g. 22 sshd, 53 systemd-resolved) are noise
# and get reported but don't flip the exit code.
PRODUCT_PORT_RANGES = [
    (3000, 3099),    # Forgejo, electric
    (3300, 3399),    # TigerBeetle
    (4200, 4399),    # Go services, frontends, Grafana
    (4800, 4899),    # Verdaccio
    (8080, 8199),    # Stalwart HTTP, Zitadel, StatsD
    (9000, 9499),    # ClickHouse secure native + Keeper
    (18000, 18099),  # vm-orchestrator host services
]

# Port-field names that designate UDP listeners. Default is TCP. Extend this
# set if you add a new UDP service instead of hand-editing per-entry schema.
UDP_FIELD_NAMES = {"statsd_port"}


@dataclass(frozen=True)
class Declared:
    """One declared (service, port-field) entry from the generated service registry."""

    service: str
    field: str
    host: str
    port: int
    listen_host: str | None

    @property
    def expected_bind(self) -> str:
        """Where the listener should appear in `ss` output."""
        return self.listen_host or self.host

    @property
    def label(self) -> str:
        return self.service if self.field == "port" else f"{self.service}.{self.field}"

    @property
    def proto(self) -> str:
        return "udp" if self.field in UDP_FIELD_NAMES else "tcp"


@dataclass
class Listener:
    bind: str
    port: int
    process: str
    proto: str  # "tcp" or "udp"


@dataclass
class DriftRow:
    label: str
    proto: str
    expected: str
    actual: str
    status: str  # OK | MISSING | WRONG_BIND | UNDECLARED
    process: str = ""


@dataclass
class Report:
    declared: list[Declared] = field(default_factory=list)
    listeners: list[Listener] = field(default_factory=list)
    rows: list[DriftRow] = field(default_factory=list)
    undeclared: list[Listener] = field(default_factory=list)

    @property
    def has_drift(self) -> bool:
        bad = any(r.status != "OK" for r in self.rows)
        product_undeclared = any(in_product_range(l.port) for l in self.undeclared)
        return bad or product_undeclared


def in_product_range(port: int) -> bool:
    return any(lo <= port <= hi for lo, hi in PRODUCT_PORT_RANGES)


def load_declared(path: Path) -> list[Declared]:
    data = yaml.safe_load(path.read_text())
    services = data.get("services") or {}
    out: list[Declared] = []

    def collect_ports(name: str, host: str, listen_host: str | None, value: object, prefix: str = "") -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                field = f"{prefix}.{key}" if prefix else str(key)
                if str(key) == "port" or str(key).endswith("_port"):
                    if isinstance(child, int):
                        out.append(
                            Declared(
                                service=name,
                                field=field,
                                host=str(host),
                                port=child,
                                listen_host=str(listen_host) if listen_host else None,
                            )
                        )
                    continue
                collect_ports(name, host, listen_host, child, field)
        elif isinstance(value, list):
            for index, child in enumerate(value):
                collect_ports(name, host, listen_host, child, f"{prefix}[{index}]")

    for name, spec in services.items():
        if not isinstance(spec, dict):
            continue
        host = spec.get("host", "127.0.0.1")
        listen_host = spec.get("listen_host")
        collect_ports(name, str(host), str(listen_host) if listen_host else None, spec)
    return out


def read_inventory(path: Path) -> tuple[str, str]:
    text = path.read_text()
    host_match = re.search(r"ansible_host=(\S+)", text)
    user_match = re.search(r"ansible_user=(\S+)", text)
    if not host_match:
        die(f"no ansible_host found in {path}", code=2)
    if not user_match:
        die(f"no ansible_user found in {path}", code=2)
    return host_match.group(1), user_match.group(1)


def fetch_listeners(user: str, host: str) -> list[Listener]:
    """Run `ss -Hltnp` + `ss -Hlunp` on the remote host and parse listeners."""
    ssh = shutil.which("ssh")
    if not ssh:
        die("ssh not found on PATH", code=2)
    # -l listening, -t tcp, -u udp, -n numeric, -p processes, -H no header.
    cmd = [
        ssh,
        "-o", "IPQoS=none",
        "-o", "StrictHostKeyChecking=no",
        f"{user}@{host}",
        "sudo -n ss -Hltnp && echo ---UDP--- && sudo -n ss -Hlunp",
    ]
    try:
        proc = subprocess.run(
            cmd, check=True, capture_output=True, text=True, timeout=15
        )
    except subprocess.CalledProcessError as exc:
        die(
            f"ssh {user}@{host} 'sudo ss' failed ({exc.returncode}): {exc.stderr.strip()}",
            code=2,
        )
    except subprocess.TimeoutExpired:
        die(f"ssh to {user}@{host} timed out", code=2)

    out: list[Listener] = []
    proto = "tcp"
    for line in proc.stdout.splitlines():
        if line.strip() == "---UDP---":
            proto = "udp"
            continue
        parsed = parse_ss_line(line, proto)
        if parsed:
            out.append(parsed)
    return out


# ss -Hltnp / -Hlunp line examples:
#   LISTEN 0 4096   127.0.0.1:4242 0.0.0.0:* users:(("billing-service",pid=1234,fd=7))
#   UNCONN 0 0      127.0.0.1:8125 0.0.0.0:* users:(("otelcol-contrib",pid=123,fd=9))
_SS_LINE_RE = re.compile(
    r"""
    ^(?:LISTEN|UNCONN)\s+\d+\s+\d+\s+
    (?P<bind>\S+?):(?P<port>\d+)\s+
    \S+\s*                              # peer
    (?:users:\(\("(?P<proc>[^"]+)"      # optional process name
       ,pid=\d+,fd=\d+\)\))?
    """,
    re.VERBOSE,
)


def parse_ss_line(line: str, proto: str) -> Listener | None:
    line = line.strip()
    if not (line.startswith("LISTEN") or line.startswith("UNCONN")):
        return None
    m = _SS_LINE_RE.match(line)
    if not m:
        return None
    bind = m.group("bind")
    # ss renders IPv6 addresses bracketed: [::]:port, [::ffff:127.0.0.1]:port.
    if bind.startswith("[") and bind.endswith("]"):
        bind = bind[1:-1]
    if bind == "*":
        bind = "0.0.0.0"
    return Listener(
        bind=bind,
        port=int(m.group("port")),
        process=m.group("proc") or "",
        proto=proto,
    )


def reconcile(declared: list[Declared], listeners: list[Listener]) -> Report:
    report = Report(declared=declared, listeners=listeners)

    # Index listeners by (proto, port); multiple binds per port are possible (v4+v6).
    by_key: dict[tuple[str, int], list[Listener]] = {}
    for l in listeners:
        by_key.setdefault((l.proto, l.port), []).append(l)

    matched_keys: set[tuple[str, str, int]] = set()

    for d in sorted(declared, key=lambda x: (x.port, x.label)):
        hits = by_key.get((d.proto, d.port), [])
        expected = f"{d.expected_bind}:{d.port}"
        if not hits:
            report.rows.append(
                DriftRow(
                    label=d.label,
                    proto=d.proto,
                    expected=expected,
                    actual="-",
                    status="MISSING",
                )
            )
            continue

        def matches(l: Listener) -> bool:
            if l.bind == d.expected_bind:
                return True
            # 0.0.0.0 / :: listener satisfies any expectation on that host.
            return l.bind in ("0.0.0.0", "::")

        matching = [l for l in hits if matches(l)]
        if matching:
            best = matching[0]
            report.rows.append(
                DriftRow(
                    label=d.label,
                    proto=d.proto,
                    expected=expected,
                    actual=f"{best.bind}:{best.port}",
                    status="OK",
                    process=best.process,
                )
            )
            for l in hits:
                matched_keys.add((l.proto, l.bind, l.port))
        else:
            wrong = hits[0]
            report.rows.append(
                DriftRow(
                    label=d.label,
                    proto=d.proto,
                    expected=expected,
                    actual=f"{wrong.bind}:{wrong.port}",
                    status="WRONG_BIND",
                    process=wrong.process,
                )
            )
            for l in hits:
                matched_keys.add((l.proto, l.bind, l.port))

    for l in listeners:
        if (l.proto, l.bind, l.port) not in matched_keys:
            report.undeclared.append(l)

    return report


# ─────────────────────────────────────────────────────────────────────────
# Output renderers
# ─────────────────────────────────────────────────────────────────────────


def render_table(report: Report) -> str:
    rows = report.rows
    headers = ("SERVICE", "PROTO", "EXPECTED", "ACTUAL", "STATUS", "PROC")
    data = [
        (r.label, r.proto, r.expected, r.actual, r.status, r.process) for r in rows
    ]
    widths = [len(h) for h in headers]
    for row in data:
        for i, cell in enumerate(row):
            widths[i] = max(widths[i], len(cell))

    lines: list[str] = []
    fmt = "  ".join(f"{{:<{w}}}" for w in widths)
    lines.append(fmt.format(*headers))
    lines.append(fmt.format(*("-" * w for w in widths)))
    for row in data:
        lines.append(fmt.format(*row))

    if report.undeclared:
        lines.append("")
        lines.append("UNDECLARED LISTENERS (not in generated services registry):")
        for l in sorted(report.undeclared, key=lambda x: (x.port, x.proto)):
            marker = "  !" if in_product_range(l.port) else "   "
            lines.append(f"{marker} {l.proto:3s} {l.bind}:{l.port}  {l.process}")
        lines.append("")
        lines.append("  ! = inside a product port range — investigate.")

    return "\n".join(lines) + "\n"


def render_json(report: Report) -> str:
    payload = {
        "declared": [
            {
                "service": d.service,
                "field": d.field,
                "host": d.host,
                "listen_host": d.listen_host,
                "port": d.port,
            }
            for d in report.declared
        ],
        "rows": [
            {
                "label": r.label,
                "proto": r.proto,
                "expected": r.expected,
                "actual": r.actual,
                "status": r.status,
                "process": r.process,
            }
            for r in report.rows
        ],
        "undeclared": [
            {
                "proto": l.proto,
                "bind": l.bind,
                "port": l.port,
                "process": l.process,
                "in_product_range": in_product_range(l.port),
            }
            for l in report.undeclared
        ],
        "has_drift": report.has_drift,
    }
    return json.dumps(payload, indent=2, sort_keys=True) + "\n"


def render_nftables(report: Report) -> str:
    """Suggested rules derived from the generated service registry.

    The real firewall is assembled by src/platform/ansible/roles/nftables and
    per-service roles/*/templates/*.nft.j2. This output is a debugging aid for
    answering 'what would a minimal ruleset for the declared topology look like?'
    """
    loopback: list[Declared] = []
    public: list[Declared] = []
    vm_subnet: list[Declared] = []

    for d in report.declared:
        bind = d.expected_bind
        if bind in ("127.0.0.1", "::1"):
            loopback.append(d)
        elif bind == "10.255.0.1":
            vm_subnet.append(d)
        else:
            public.append(d)

    lines: list[str] = []
    lines.append("#!/usr/sbin/nft -f")
    lines.append("# Derived from src/platform/ansible/group_vars/all/generated/services.yml")
    lines.append("# SUGGESTION ONLY — authoritative rules live in Ansible role templates.")
    lines.append("")
    lines.append("table inet verself_services_suggested")
    lines.append("delete table inet verself_services_suggested")
    lines.append("table inet verself_services_suggested {")
    lines.append("    chain input {")
    lines.append("        type filter hook input priority filter; policy drop;")
    lines.append("        ct state established,related accept")
    lines.append("        ct state invalid drop")
    lines.append("        iifname \"lo\" accept")
    lines.append("")

    if loopback:
        lines.append("        # Loopback-only services (declared host=127.0.0.1).")
        lines.append("        # Belt-and-suspenders: drop non-lo traffic destined to these ports.")
        for d in sorted(loopback, key=lambda x: (x.proto, x.port)):
            lines.append(
                f"        {d.proto} dport {d.port} iifname != \"lo\" drop "
                f"comment \"{d.label}\""
            )
        lines.append("")

    for proto in ("tcp", "udp"):
        ports = [d for d in sorted(public, key=lambda x: x.port) if d.proto == proto]
        if not ports:
            continue
        port_list = ", ".join(str(d.port) for d in ports)
        labels = ", ".join(d.label for d in ports)
        lines.append(f"        # Public {proto} services: {labels}")
        lines.append(f"        {proto} dport {{ {port_list} }} accept")
        lines.append("")

    for proto in ("tcp", "udp"):
        ports = [d for d in sorted(vm_subnet, key=lambda x: x.port) if d.proto == proto]
        if not ports:
            continue
        port_list = ", ".join(str(d.port) for d in ports)
        labels = ", ".join(d.label for d in ports)
        lines.append(f"        # Firecracker host-service plane ({proto}): {labels}")
        lines.append(
            f"        iifname \"fc-tap-*\" ip daddr 10.255.0.1 {proto} dport {{ {port_list} }} accept"
        )
        lines.append("")

    lines.append("        ip protocol icmp accept")
    lines.append("        ip6 nexthdr icmpv6 accept")
    lines.append("    }")
    lines.append("}")
    return "\n".join(lines) + "\n"


def die(msg: str, code: int = 2) -> None:
    print(f"ERROR: {msg}", file=sys.stderr)
    sys.exit(code)


def main(argv: list[str]) -> int:
    fmt = os.environ.get("FORMAT", "table").lower()
    if fmt not in ("table", "json", "nftables"):
        die(f"unknown FORMAT={fmt} (want: table, json, nftables)", code=2)

    if not SERVICES_YAML.is_file():
        die(f"{SERVICES_YAML} not found", code=2)
    if not INVENTORY.is_file():
        die(
            f"{INVENTORY} not found. Run: cd src/platform/ansible && "
            "ansible-playbook playbooks/provision.yml",
            code=2,
        )

    declared = load_declared(SERVICES_YAML)
    host, user = read_inventory(INVENTORY)

    skip_ssh = os.environ.get("SKIP_SSH") == "1"
    if skip_ssh:
        listeners: list[Listener] = []
    else:
        listeners = fetch_listeners(user, host)

    report = reconcile(declared, listeners)

    if fmt == "table":
        sys.stdout.write(render_table(report))
    elif fmt == "json":
        sys.stdout.write(render_json(report))
    elif fmt == "nftables":
        sys.stdout.write(render_nftables(report))

    if skip_ssh:
        # Can't assess drift without live data; table/json still useful.
        return 0
    return 1 if report.has_drift else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
