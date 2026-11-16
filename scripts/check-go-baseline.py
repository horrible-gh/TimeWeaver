#!/usr/bin/env python3
"""Read-only legacy source/route inventory; never imports or starts the server."""
import argparse
import ast
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASELINE = "d6bdb751e08e68ff98781b5b41e5f90b09383ce2"
FROZEN_ROOTS = ("server", "agent")
PREFIXES = {
    "login/login.py": "/login",
    "login/logout.py": "/logout",
    "dashboard/charts.py": "/dashboard/charts",
    "dashboard/dashboard.py": "/dashboard",
    "dashboard/devices.py": "/dashboard/devices",
    "dashboard/schedule.py": "/dashboard/schedule",
    "dashboard/tasks.py": "/dashboard/tasks",
    "dashboard/groups.py": "/dashboard/groups",
    "dashboard/manual_execution.py": "/dashboard/manual_execution",
    "dashboard/agent_enrollment_tokens.py": "/dashboard/agent-enrollment-tokens",
    "agent/enroll.py": "/api/agent/v1",
    "agent/runtime.py": "/api/agent/v1",
    "agent/execution.py": "/api/agent/v1",
    "agent/health.py": "/api/agent/v1",
}
METHODS = {"get", "post", "put", "patch", "delete", "head", "options"}


def git(root, *args):
    result = subprocess.run(
        ["git", "-C", str(root), *args], capture_output=True, check=False
    )
    if result.returncode:
        # Do not dump arbitrary command output, configuration, or credentials.
        raise ValueError("Git baseline unavailable; use a checkout with full history")
    return result.stdout


def inventory(root):
    """Extract literal router contracts without executing Python application code."""
    routes = []
    directory = root / "server" / "routers"
    for source in sorted(directory.rglob("*.py")):
        relative = source.relative_to(directory).as_posix()
        module = ast.parse(source.read_text(encoding="utf-8-sig"), filename=relative)
        for node in ast.walk(module):
            if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                continue
            for item in node.decorator_list:
                if not (isinstance(item, ast.Call) and isinstance(item.func, ast.Attribute)):
                    continue
                if not (isinstance(item.func.value, ast.Name) and item.func.value.id == "router"):
                    continue
                if item.func.attr not in METHODS:
                    continue
                if relative not in PREFIXES:
                    raise ValueError("Router prefix not inventoried: " + relative)
                if not item.args or not isinstance(item.args[0], ast.Constant) or not isinstance(item.args[0].value, str):
                    raise ValueError("Non-literal route requires review: " + relative)
                routes.append({
                    "method": item.func.attr.upper(),
                    "path": "{CONTEXT}" + PREFIXES[relative] + item.args[0].value,
                    "source": "server/routers/" + relative,
                    "handler": node.name,
                })
    return sorted(routes, key=lambda r: (r["path"], r["method"], r["source"]))


def verify(root, baseline=BASELINE):
    root = Path(root).resolve()
    entries = git(root, "ls-tree", "-r", "-z", baseline, "--", *FROZEN_ROOTS)
    expected = {}
    for entry in entries.split(b"\0"):
        if not entry:
            continue
        metadata, name = entry.split(b"\t", 1)
        mode, kind, sha = metadata.decode("ascii").split()
        if kind != "blob" or mode == "120000":
            raise ValueError("Unexpected object in frozen legacy source")
        expected[name.decode("utf-8")] = sha
    if not expected:
        raise ValueError("The baseline contains no legacy source")
    tracked = {
        p.decode("utf-8") for p in git(root, "ls-files", "-z", "--", *FROZEN_ROOTS).split(b"\0") if p
    }
    problems = ["added: " + p for p in sorted(tracked - expected.keys())]
    problems += ["deleted: " + p for p in sorted(expected.keys() - tracked)]
    checksums = []
    for name, sha in sorted(expected.items()):
        source = root / name
        # Reject links in every component, not just the final file.
        if any(p.is_symlink() for p in [source, *source.parents] if p != root.parent):
            problems.append("symlink: " + name)
            continue
        if not source.is_file():
            problems.append("missing: " + name)
            continue
        original = git(root, "cat-file", "blob", sha)
        checksum = hashlib.sha256(original).hexdigest()
        if hashlib.sha256(source.read_bytes()).hexdigest() != checksum:
            problems.append("changed: " + name)
        checksums.append({"path": name, "sha256": checksum})
    if problems:
        raise ValueError("Legacy source changed:\n" + "\n".join(problems))
    return {
        "baseline_commit": baseline,
        "checked_files": len(checksums),
        "frozen_files": checksums,
        "routes": inventory(root),
        "scope": "static inventory only; not a runtime API or database compatibility test",
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        print(json.dumps(verify(args.root), indent=2, ensure_ascii=False))
    except (OSError, ValueError, SyntaxError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
