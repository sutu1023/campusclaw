#!/usr/bin/env python3
"""Run the CampusClaw API test cases and write a self-contained HTML report.

Each case is a Python file under ``apitest/cases`` whose path mirrors the URL it
exercises (for example ``cases/api/login/POST.py`` for ``POST /api/login``). A
case exposes ``ORDER``, ``METHOD``, ``PATH`` and a ``run(ctx)`` function that
returns a list of ``(description, passed)`` assertions. Cases share state
through ``ctx`` (tokens, ids created earlier).

Usage:
    python3 apitest/run_all.py [--base-url http://localhost:8080]
"""

import argparse
import html
import importlib.util
import json
import os
import sys
from datetime import datetime
from pathlib import Path

HERE = Path(__file__).resolve().parent
CASES_DIR = HERE / "cases"
DEFAULT_REPORT = HERE / "report" / "report.html"

sys.path.insert(0, str(HERE))  # let cases `import client`


def load_cases():
    cases = []
    for path in CASES_DIR.rglob("*.py"):
        if path.name.startswith("_") or path.name == "__init__.py":
            continue
        rel = path.relative_to(CASES_DIR).as_posix()
        module_name = "apitest_case_" + rel.replace("/", "_").replace(".", "_")
        spec = importlib.util.spec_from_file_location(module_name, path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        order = getattr(module, "ORDER", 100)
        cases.append((order, rel, module))
    cases.sort(key=lambda item: (item[0], item[1]))
    return cases


def run_case(module, ctx):
    try:
        results = module.run(ctx)
    except Exception as exc:  # noqa: BLE001 - a crashing case is a failed case
        results = [(f"case raised {type(exc).__name__}: {exc}", False)]
    if not isinstance(results, list):
        results = [("case returned no assertions", False)]
    return results


def render_report(entries, base_url, duration, failures, gateway_evidence="unspecified"):
    rows = []
    for rel, results in entries:
        rows.append(f"<tr class=\"case\"><td colspan=\"2\">{html.escape(rel)}</td></tr>")
        for desc, ok in results:
            status = "PASS" if ok else "FAIL"
            css = "ok" if ok else "bad"
            rows.append(
                f"<tr><td class=\"status {css}\">{status}</td>"
                f"<td>{html.escape(desc)}</td></tr>"
            )
    total = sum(len(r) for _, r in entries)
    passed = sum(1 for _, r in entries for _, ok in r if ok)
    summary = f"{passed}/{total} 项断言通过"
    if failures:
        summary += f"，{failures} 项失败"
    return f"""<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8" />
<title>CampusClaw API 测试报告</title>
<style>
 body {{ font-family: -apple-system, "Segoe UI", "PingFang SC", sans-serif; margin: 24px; color: #1c2530; }}
 h1 {{ color: #94070a; font-size: 22px; }}
 .meta {{ color: #666; margin-bottom: 16px; }}
 table {{ border-collapse: collapse; width: 100%; }}
 td {{ border-bottom: 1px solid #e6e8ec; padding: 6px 10px; vertical-align: top; }}
 tr.case td {{ background: #f5f6f8; font-weight: 700; }}
 td.status {{ width: 70px; font-weight: 700; }}
 td.ok {{ color: #1a7f37; }}
 td.bad {{ color: #b3261e; }}
</style>
</head>
<body>
<h1>CampusClaw API 测试报告</h1>
<p class="meta">目标：{html.escape(base_url)} · 生成时间：{datetime.now().isoformat(timespec="seconds")} · 用时 {duration:.2f}s · {summary}</p>
<p class="meta">网关证据：{html.escape(gateway_evidence)}（deterministic-fixture / unspecified 不代表真实模型验收）</p>
<table>
<tbody>
{chr(10).join(rows)}
</tbody>
</table>
</body>
</html>
"""


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default=os.environ.get("APITEST_BASE_URL", "http://localhost:8080"))
    parser.add_argument("--report", default=str(DEFAULT_REPORT))
    parser.add_argument("--gateway-evidence", choices=("unspecified", "deterministic-fixture", "real-gateway"), default="unspecified")
    args = parser.parse_args()

    ctx = {"base_url": args.base_url, "log": []}
    cases = load_cases()
    if not cases:
        print("no cases found under", CASES_DIR)
        return 1

    import time

    started = time.time()
    entries = []
    failures = 0
    for _, rel, module in cases:
        results = run_case(module, ctx)
        entries.append((rel, results))
        for desc, ok in results:
            marker = "PASS" if ok else "FAIL"
            print(f"[{marker}] {rel} - {desc}")
            if not ok:
                failures += 1
    duration = time.time() - started

    report_path = Path(args.report)
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(render_report(entries, args.base_url, duration, failures, args.gateway_evidence), encoding="utf-8")
    print(f"\nreport written to {report_path}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
