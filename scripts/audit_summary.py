#!/usr/bin/env python3
"""Summarize ghost-driver-mcp tool usage from the audit log.

Usage:
    python scripts/audit_summary.py [path/to/audit.jsonl]

Defaults to ./memory/audit.jsonl.
"""
import json
import sys
from collections import defaultdict
from pathlib import Path


def main():
    log_path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("./memory/audit.jsonl")
    if not log_path.exists():
        print(f"No audit log found at {log_path}")
        sys.exit(1)

    stats = defaultdict(lambda: {"count": 0, "total_ms": 0, "errors": 0})
    total_lines = 0

    for line in log_path.read_text(encoding="utf-8", errors="replace").splitlines():
        total_lines += 1
        # Lines look like: [AUDIT] {"request_id":"req-1",...}
        if not line.startswith("[AUDIT] "):
            continue
        try:
            entry = json.loads(line[len("[AUDIT] "):])
        except json.JSONDecodeError:
            continue
        name = entry.get("tool_name", "?")
        stats[name]["count"] += 1
        stats[name]["total_ms"] += entry.get("duration_ms", 0)
        if entry.get("status") == "error":
            stats[name]["errors"] += 1

    if not stats:
        print(f"Log has {total_lines} lines but no parseable audit entries.")
        return

    # Sort by invocation count descending.
    rows = sorted(stats.items(), key=lambda x: -x[1]["count"])

    print(f"{'Tool':<30} {'Calls':>6} {'Avg ms':>8} {'Errors':>7} {'Err%':>6}")
    print("-" * 60)
    for name, s in rows:
        avg = s["total_ms"] / s["count"] if s["count"] else 0
        err_pct = 100 * s["errors"] / s["count"] if s["count"] else 0
        print(f"{name:<30} {s['count']:>6} {avg:>8.0f} {s['errors']:>7} {err_pct:>5.1f}%")
    print("-" * 60)
    print(f"Total: {sum(s['count'] for _, s in rows)} calls across {len(rows)} tools")


if __name__ == "__main__":
    main()
