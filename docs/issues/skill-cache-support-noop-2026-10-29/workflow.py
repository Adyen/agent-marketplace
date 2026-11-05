#!/usr/bin/env python3
"""Investigation driver: warm `sync` wall time and source-revision build counts
for locked Git registries of different support-file shapes, using the real
binary in isolated sandboxes. Fixture setup and the cold sync are untimed.

Usage: WORKFLOW_ROOT=<scratch dir> AM_BIN=<binary> [R05_RUNS=11] python3 workflow.py

Build AM_BIN with `go build -tags e2e ./cmd/agent-marketplace` so the sandbox's
AGENT_MARKETPLACE_V3_ORG_CONFIG replaces the host organization config.
Existing sandboxes under WORKFLOW_ROOT/sandboxes are recreated."""
import json
import os
import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(os.environ["WORKFLOW_ROOT"]).resolve()
BIN = Path(os.environ["AM_BIN"]).resolve()
OUT = ROOT / "results"
MARKER = b"built direct-Git source revision"
SHAPES = [
    ("control_330x0", 330, 0, 0),
    ("observed_330x2x15KiB", 330, 2, 15 << 10),
    ("heavy_330x8x16KiB", 330, 8, 16 << 10),
]
WARMUP, RUNS = 2, int(os.environ.get("R05_RUNS", "11"))


GIT_ENV = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")} | {"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1"}


def git(cwd, *args):
    # Host global hooks/maintenance must not race the fixture's local clone.
    subprocess.run(["git", *args], cwd=cwd, env=GIT_ENV, check=True, capture_output=True)


def environment(box):
    env = {k: v for k, v in os.environ.items() if not k.startswith(("GIT_", "AGENT_MARKETPLACE_")) and k not in ("CODEX_HOME", "CLAUDE_CONFIG_DIR", "CI")}
    home = box / "home"
    env.update({
        "HOME": str(home), "XDG_CACHE_HOME": str(home / ".cache"), "XDG_STATE_HOME": str(home / ".local/state"),
        "XDG_CONFIG_HOME": str(home / ".config"), "CODEX_HOME": str(home / ".codex"), "CLAUDE_CONFIG_DIR": "",
        "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0",
        "AGENT_MARKETPLACE_V3_ORG_CONFIG": str(box / "etc" / "absent.yaml"),
        "AGENT_MARKETPLACE_TELEMETRY_LEVEL": "off", "AGENT_MARKETPLACE_NO_UPDATE_CHECK": "1",
        "AGENT_MARKETPLACE_LOG_ENABLED": "true", "AGENT_MARKETPLACE_LOG_LEVEL": "debug",
        "AGENT_MARKETPLACE_LOG_FILE": str(box / "am.log"),
    })
    return env


def fixture(box, skills, files, size):
    source, origin, repo = box / "source", box / "origin.git", box / "repo"
    for name in range(skills):
        skill = source / "skills" / f"skill-{name:03d}"
        (skill / "references").mkdir(parents=True)
        (skill / "SKILL.md").write_text(f"---\nname: skill-{name:03d}\ndescription: benchmark skill\n---\nBody {name}.\n")
        for j in range(files):
            prefix = f"skill-{name:03d}/{j:02d}\n".encode()
            (skill / "references" / f"ref-{j:02d}.txt").write_bytes(prefix + b"s" * (size - len(prefix)))
    git(box, "init", "-q", "-b", "main", str(source))
    git(source, "add", ".")
    # A detached auto-gc after commit can repack objects while a local clone
    # hardlinks them, leaving the bare origin with missing trees.
    git(source, "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "commit.gpgsign=false", "-c", "user.name=Bench", "-c", "user.email=bench@example.com", "commit", "-qm", "seed")
    git(box, "clone", "-q", "--bare", "--no-local", str(source), str(origin))
    git(origin, "fsck", "--no-progress")
    (repo / ".agents").mkdir(parents=True)
    git(repo, "init", "-q")
    (repo / ".agents" / "config.yaml").write_text(
        f"apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  bench:\n    git: file://{origin}\n    ref: main\nresources:\n  bench:\n    all: true\n")
    return repo


RUN_ID = iter(range(1 << 30))


def run(repo, env, *args):
    # A fresh log per invocation: rotation cannot distort the build count.
    log = Path(env["AGENT_MARKETPLACE_LOG_FILE"]).with_name(f"run-{next(RUN_ID):04d}.log")
    start = time.perf_counter()
    proc = subprocess.run([str(BIN), *args], cwd=repo, env=env | {"AGENT_MARKETPLACE_LOG_FILE": str(log)}, capture_output=True)
    elapsed = time.perf_counter() - start
    if proc.returncode != 0:
        sys.exit(f"{args} failed: {proc.stdout.decode()}{proc.stderr.decode()}")
    return elapsed, log.read_bytes().count(MARKER)


def main():
    OUT.mkdir(exist_ok=True)
    results = []
    for name, skills, files, size in SHAPES:
        box = ROOT / "sandboxes" / name
        if box.exists():
            shutil.rmtree(box)
        box.mkdir(parents=True)
        env = environment(box)
        repo = fixture(box, skills, files, size)
        run(repo, env, "sync")
        lock = (repo / ".agents" / "lock.yaml").read_bytes()
        installed = len(list((repo / ".factory" / "skills").iterdir()))
        if installed != skills:
            sys.exit(f"{name}: installed {installed} skills, want {skills}")
        row = {"shape": name, "skills": skills, "support_files": skills * files, "support_MiB": skills * files * size / 1048576}
        for command in (["sync"], ["sync", "--check"]):
            for _ in range(WARMUP):
                run(repo, env, *command)
            samples = [run(repo, env, *command) for _ in range(RUNS)]
            times = sorted(s[0] for s in samples)
            builds = {s[1] for s in samples}
            key = " ".join(command)
            row[key] = {"median_ms": round(statistics.median(times) * 1000, 1), "min_ms": round(times[0] * 1000, 1),
                        "max_ms": round(times[-1] * 1000, 1), "source_revision_builds_per_run": sorted(builds), "runs": RUNS}
        if (repo / ".agents" / "lock.yaml").read_bytes() != lock:
            sys.exit(f"{name}: warm runs changed the lock")
        results.append(row)
        print(json.dumps(row), flush=True)
    (OUT / os.environ.get("WORKFLOW_OUT", "workflow.json")).write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
