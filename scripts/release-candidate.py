#!/usr/bin/env python3
"""Assemble or check exact-byte release evidence; never grant production approval."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
REQUIRED = ("keyCustody", "dependencyReview", "cleanMacLifecycle", "independentTeamNetwork", "replacementHostRecovery", "soak", "independentSecurityReview")


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def create(directory, tag, commit, linux):
    if not re.fullmatch(r"werkbord-v\d+\.\d+\.\d+(-preview\.[1-9]\d*)?", tag) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("invalid candidate version/source")
    if (directory / "checksums.txt.sig").exists():
        raise ValueError("refuse to rebuild an already signed candidate")
    version = tag[len("werkbord-v"): ]
    required = [f"Werkbord_{version}_darwin_universal.dmg", f"Werkbord_{version}_darwin_universal.zip", "Werkbord.dmg", "appcast.xml", "release-audit.tar.gz"]
    required += [f"werkbord_{version}_darwin_{arch}.tar.gz" for arch in ("amd64", "arm64")]
    if linux:
        required += [f"Werkbord_{version}_linux_{arch}.{ext}" for arch in ("amd64", "arm64") for ext in ("deb", "tar.gz")]
        required += [f"werkbord_{version}_linux_{arch}.tar.gz" for arch in ("amd64", "arm64")]
    if any(not (directory / name).is_file() or (directory / name).is_symlink() for name in required):
        raise ValueError("candidate lacks required platform assets")
    acceptance = json.loads((ROOT / "docs/RELEASE_ACCEPTANCE.json").read_text())
    if "-preview." not in tag:
        if acceptance.get("version") != tag[len("werkbord-v"): ] or any(acceptance.get("requirements", {}).get(name, {}).get("status") != "accepted" or not acceptance["requirements"][name].get("evidence") for name in REQUIRED):
            raise ValueError("stable candidate lacks complete version-specific acceptance evidence")
        if linux and acceptance.get("requirements", {}).get("cleanLinuxLifecycle", {}).get("status") != "accepted":
            raise ValueError("stable Linux candidate lacks independent Linux lifecycle acceptance")
    scope = ["darwin/universal"] + (["linux/amd64", "linux/arm64"] if linux else [])
    evidence = {"schema": 1, "tag": tag, "sourceCommit": commit, "platforms": scope, "acceptance": acceptance,
                "workflowRun": os.environ.get("GITHUB_RUN_ID", "local-unaccepted"),
                "runtimeScope": "disposable Team evaluation" if "-preview." in tag else "reviewed production",
                "audit": {"name": "release-audit.tar.gz", "sha256": digest(directory / "release-audit.tar.gz")},
                "limitations": "A signature approves these bytes and referenced evidence; packaging is not independent-machine acceptance."}
    (directory / "release-evidence.json").write_text(json.dumps(evidence, sort_keys=True, indent=2)+"\n")
    entries = []
    for path in sorted(directory.iterdir()):
        if path.name == "checksums.txt":
            continue
        if not path.is_file() or path.is_symlink() or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", path.name):
            raise ValueError("candidate contains a non-regular/unsafe asset")
        entries.append(f"{digest(path)}  {path.name}\n")
    (directory / "checksums.txt").write_text("".join(entries))


def verify(directory, tag, commit):
    # Signature and key come from a separately administered protected review environment.
    key = base64.urlsafe_b64decode(os.environ["RELEASE_PUBLIC_KEY"]+"=")
    sig = base64.b64decode(os.environ["RELEASE_MANIFEST_SIGNATURE"], validate=True)
    if len(key) != 32 or len(sig) != 64 or base64.urlsafe_b64encode(key).rstrip(b"=").decode() != os.environ["RELEASE_PUBLIC_KEY"]:
        raise ValueError("invalid independently configured release key/signature")
    pem = "-----BEGIN PUBLIC KEY-----\n" + base64.b64encode(bytes.fromhex("302a300506032b6570032100")+key).decode()+"\n-----END PUBLIC KEY-----\n"
    # This public key is provisioned independently, never retrieved from the candidate download.
    public = directory.parent / "trusted-release-public.pem"
    public.write_text(pem)
    (directory / "checksums.txt.sig").write_bytes(sig)
    subprocess.run([str(ROOT / "scripts/verify-release-manifest.sh"), tag, str(directory / "checksums.txt"), str(directory / "checksums.txt.sig"), str(public)], check=True)
    listed = set()
    for line in (directory / "checksums.txt").read_text().splitlines():
        expected, name = line.split()
        name = name[1:] if name.startswith("*") else name
        path = directory / name
        if path.is_symlink() or not path.is_file() or digest(path) != expected:
            raise ValueError("candidate asset no longer matches signed bytes")
        listed.add(name)
    if listed != {p.name for p in directory.iterdir()} - {"checksums.txt", "checksums.txt.sig"}:
        raise ValueError("candidate has unsigned or missing assets")
    evidence = json.loads((directory / "release-evidence.json").read_text())
    if evidence["tag"] != tag or evidence["sourceCommit"] != commit:
        raise ValueError("candidate source/tag substitution")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["create", "verify"])
    parser.add_argument("--dir", required=True, type=Path)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--linux", action="store_true")
    args = parser.parse_args()
    if args.action == "create":
        create(args.dir, args.tag, args.commit, args.linux)
    else:
        verify(args.dir, args.tag, args.commit)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(f"release-candidate: {error}")
