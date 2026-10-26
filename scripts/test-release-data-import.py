#!/usr/bin/env python3
"""Check real release data against a current controller in disposable directories.

Supply checksum-verified executables for this machine. No installed app, service,
credentials or data is used. --manual-layout explicitly copies the previous SQLite
database into the current filename; it does not claim automatic path migration.
"""

import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


TOKEN = "release-import-disposable-owner-credential"


def request(base, method, path, body=None, token=TOKEN):
    payload = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=payload, method=method,
                                 headers={"Authorization": "Bearer " + token,
                                          "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as response:
        return json.load(response)


@contextlib.contextmanager
def controller(binary, data, root, label):
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(("WERKBORD_", "DEVBOARD_", "GITHUB_", "GH_"))}
    child_home = root / (label + "-home")
    child_home.mkdir()
    env.update(HOME=str(child_home), XDG_CONFIG_HOME=str(child_home / "config"),
               PATH="/usr/bin:/bin", GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
    base = "http://127.0.0.1:" + str(port)
    with (root / (label + ".log")).open("w+") as log:
        process = subprocess.Popen([str(binary), "serve", "--data-dir", str(data), "--addr",
                                    "127.0.0.1:" + str(port)], env=env, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 40
            while True:
                if process.poll() is not None:
                    log.seek(0)
                    raise RuntimeError(label + " exited:\n" + log.read())
                try:
                    request(base, "GET", "/api/health")
                    break
                except (OSError, ValueError):
                    if time.monotonic() >= deadline:
                        log.seek(0)
                        raise RuntimeError(label + " did not become ready:\n" + log.read())
                    time.sleep(0.1)
            yield base
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--previous", required=True, type=Path)
    parser.add_argument("--current", required=True, type=Path)
    parser.add_argument("--label", required=True)
    parser.add_argument("--manual-layout", action="store_true")
    args = parser.parse_args()
    previous, current = args.previous.resolve(strict=True), args.current.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="werkbord-release-import-") as folder:
        root = Path(folder).resolve()
        original, upgraded, repo = root / "original", root / "upgraded", root / "repo"
        original.mkdir()
        subprocess.run(["git", "init", "-q", str(repo)], check=True)
        # Both releases understand these settings. Unknown future settings are ignored
        # by old releases; no private network, relay, update request or agent is started.
        (original / "config.json").write_text(json.dumps({
            "noUpdateCheck": True, "network": {"enabled": False},
            "relay": {"disabled": True}, "assistant": {"disabled": True},
        }))
        (original / "token").write_text(TOKEN)
        (original / "token").chmod(0o600)
        with controller(previous, original, root, "previous") as base:
            project = request(base, "POST", "/api/projects", {"path": str(repo), "name": "Release import"})
            path = "/api/projects/" + project["id"] + "/tasks"
            task = request(base, "POST", path, {"title": "Keep this task", "description": "Created by " + args.label})
            task = request(base, "PATCH", path + "/" + task["id"],
                           {"state": "review", "version": task["version"]})

        databases = list(original.glob("*.db"))
        assert len(databases) == 1, databases
        source = databases[0]
        before = hashlib.sha256(source.read_bytes()).hexdigest()
        shutil.copytree(original, upgraded)
        if args.manual_layout and source.name != "werkbord.db":
            # SQLite's backup API includes committed WAL data and leaves the source intact.
            with sqlite3.connect(source.as_uri() + "?mode=ro", uri=True) as src:
                with sqlite3.connect(str(upgraded / "werkbord.db")) as dst:
                    src.backup(dst)
            for suffix in ("", "-wal", "-shm"):
                old = upgraded / (source.name + suffix)
                if old.exists():
                    old.unlink()

        with controller(current, upgraded, root, "current") as base:
            imported = request(base, "GET", "/api/projects/" + project["id"])
            assert imported["id"] == project["id"] and imported["name"] == project["name"] and imported["repoPath"] == str(repo), imported
            tasks = request(base, "GET", path)["tasks"]
            kept = next(t for t in tasks if t["id"] == task["id"])
            assert (kept["title"], kept["description"], kept["state"]) == (
                task["title"], task["description"], "review"), kept
            assert kept["version"] == task["version"], kept
            changed = request(base, "PATCH", path + "/" + kept["id"],
                              {"title": "Current version can edit", "version": kept["version"]})
            assert changed["version"] > kept["version"], changed
            try:
                request(base, "GET", path, token="wrong-disposable-credential")
            except urllib.error.HTTPError as error:
                assert error.code in (401, 403), error
            else:
                raise AssertionError("the imported owner token is not enforced")
        assert (upgraded / "token").read_text() == TOKEN
        assert hashlib.sha256(source.read_bytes()).hexdigest() == before, "the original database changed"
        mode = "explicit layout import" if args.manual_layout else "unchanged layout"
        print("PASS " + args.label + " (" + mode + "): project, task, review state, versions and owner token retained; original untouched")


if __name__ == "__main__":
    main()
