#!/usr/bin/env python3
"""Fresh source build with pinned provenance. Cache records never authorize a build."""
import hashlib
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(args, cwd, env=None):
    subprocess.run(args, cwd=cwd, env=env, check=True, stdout=sys.stderr)


def publish(dest, binary, src, record):
    # Concurrent packaging/check jobs share the cache. Stage privately, then publish
    # the binary and its source provenance together under a per-target writer lock.
    with tempfile.TemporaryDirectory(prefix=".publish-", dir=dest) as stage:
        stage = Path(stage)
        shutil.copyfile(binary, stage / "nebula")
        (stage / "nebula").chmod(0o755)
        for name in ("go.mod", "go.sum"):
            shutil.copyfile(src / name, stage / name)
        (stage / "nebula.build").write_text(json.dumps(record, sort_keys=True) + "\n")
        with (dest / ".publish.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            for name in ("go.mod", "go.sum", "nebula", "nebula.build"):
                (stage / name).replace(dest / name)


def build(product, target_os, arch):
    if product != "nebula" or target_os not in ("darwin", "linux") or arch not in ("amd64", "arm64"):
        raise ValueError("expected nebula darwin|linux amd64|arm64")
    manifest = (ROOT / "internal/team/infra/nebula/manifest.go").read_text()
    version = re.search(r'const Version = "([^"]+)"', manifest)[1]
    commit = re.search(r'const SourceCommit = "([0-9a-f]{40})"', manifest)[1]
    env = dict(os.environ, GOTOOLCHAIN="go1.27.3", CGO_ENABLED="0", GOOS=target_os, GOARCH=arch)
    # Clear inherited compiler flags; the dependency build has an explicit, recorded configuration.
    for name in ("GOFLAGS", "GOWORK", "CGO_CFLAGS", "CGO_LDFLAGS"):
        env.pop(name, None)
    env["GOWORK"] = "off"
    cache = Path(os.environ.get("NEBULA_CACHE", ROOT / ".cache/nebula"))
    dest = cache / version / f"{target_os}_{arch}"
    dest.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="werkbord-nebula-") as work:
        src = Path(work) / "src"
        run(["git", "-c", "advice.detachedHead=false", "clone", "-q", "--depth", "1", "--branch", f"v{version}", "https://github.com/slackhq/nebula.git", str(src)], ROOT)
        actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=src, text=True).strip()
        if actual != commit:
            raise ValueError("Nebula tag does not match the reviewed source commit")
        run(["go", "mod", "edit", "-go=1.27.3"], src, env)
        run(["go", "get", "golang.org/x/net@v0.60.0", "golang.org/x/crypto@v0.57.0"], src, env)
        binary = Path(work) / "nebula"
        run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", f"-s -w -X main.Build={version}", "-o", str(binary), "./cmd/nebula"], src, env)
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        # Source/module inputs are retained so artifact audits include the actual patched graph.
        record = {"version": version, "commit": commit, "sha256": digest, "toolchain": "go1.27.3", "goos": target_os, "goarch": arch,
                  "goModSHA256": hashlib.sha256((src / "go.mod").read_bytes()).hexdigest(),
                  "goSumSHA256": hashlib.sha256((src / "go.sum").read_bytes()).hexdigest()}
        publish(dest, binary, src, record)
    print(dest.resolve())


if __name__ == "__main__":
    try:
        build(*sys.argv[1:])
    except (ValueError, TypeError, subprocess.CalledProcessError) as error:
        sys.exit(f"build-infrastructure: {error}")
