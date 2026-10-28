#!/usr/bin/env python3
"""Collect repeatable inventories and advisory evidence without customer data or credentials."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def reviewed_preview_advisory(raw, lock, review, version, today=None, runtime_sources=()):
    """Accept only the reviewed build-only finding; malformed/changed scans fail closed."""
    today = today or datetime.date.today()
    if any("deepmerge" in source for source in runtime_sources):
        return False
    if not re.fullmatch(r"\d+\.\d+\.\d+-preview\.[1-9]\d*", version):
        return False
    if not (datetime.date.fromisoformat(review["reviewedAt"]) <= today <= datetime.date.fromisoformat(review["expiresAt"])):
        return False
    if raw.get("error") or raw.get("auditReportVersion") != 2:
        return False
    findings = raw.get("vulnerabilities", {})
    expected = review["packages"]
    if not findings or set(findings) != set(expected):
        return False
    for name, version in expected.items():
        node = "node_modules/" + name
        dependency = lock.get("packages", {}).get(node, {})
        finding = findings[name]
        if dependency.get("version") != version or dependency.get("dev") is not True or dependency.get("integrity") != review["integrities"][name]:
            return False
        if finding.get("severity") != review["severity"] or finding.get("nodes") != [node] or finding.get("fixAvailable") is not False:
            return False
        via = finding.get("via", [])
        if name == "deepmerge":
            if len(via) != 1 or not isinstance(via[0], dict) or via[0].get("url") != review["advisory"] or via[0].get("severity") != review["severity"]:
                return False
        elif via != ["deepmerge"]:
            return False
    return True


def objects(raw):
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset == len(raw):
            break
        value, offset = decoder.raw_decode(raw, offset)
        yield value


def capture(args, cwd=ROOT, timeout=900):
    result = subprocess.run(args, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    return result.returncode, result.stdout, result.stderr


def inventory(out):
    components = []
    evidence = []
    notice_entries = {}
    def collect_notices(name, version, folder, scope):
        identity = name+"@"+version
        if identity in notice_entries:
            return
        entry = {"name": name, "version": version, "scope": scope, "files": []}
        if folder and folder.is_dir() and not folder.is_symlink():
            for source in sorted(folder.iterdir()):
                if source.is_file() and not source.is_symlink() and source.suffix.lower() not in (".go", ".js", ".ts", ".json", ".py", ".sh", ".c", ".h", ".cc", ".rs") and source.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS", "OFL", "UNLICENSE", "COPYRIGHT")):
                    if source.stat().st_size > 2*1024*1024:
                        continue
                    raw = source.read_bytes()
                    filename = hashlib.sha256(identity.encode()).hexdigest()[:16]+"-"+source.name+".txt"
                    (out / "notices" / filename).write_bytes(raw)
                    entry["files"].append({"name": source.name, "copy": filename, "sha256": hashlib.sha256(raw).hexdigest()})
        entry["status"] = "collected" if entry["files"] else "unavailable; review whether shipped and obtain license before redistribution"
        notice_entries[identity] = entry
    (out / "notices").mkdir()
    for module in (".", "desktop"):
        code, raw, err = capture(["go", "list", "-m", "-json", "all"], ROOT / module)
        if code:
            raise RuntimeError(f"cannot inventory Go module {module}: {err}")
        modules = list(objects(raw))
        (out / ("go-root.json" if module == "." else "go-desktop.json")).write_text(json.dumps(modules, sort_keys=True, indent=2)+"\n")
        for dependency in modules:
            if dependency.get("Main"):
                continue
            resolved = dependency.get("Replace", dependency)
            collect_notices(dependency["Path"], dependency.get("Version", "local"), Path(resolved["Dir"]) if resolved.get("Dir") else None, "Go source/build graph")
            components.append({"type": "library", "name": dependency["Path"], "version": dependency.get("Version", "local"),
                               "purl": "pkg:golang/"+dependency["Path"]+"@"+dependency.get("Version", "local"),
                               "properties": [{"name": "werkbord:module", "value": module}, {"name": "werkbord:dependency", "value": "transitive" if dependency.get("Indirect") else "direct"}]})
    for manifest in ("web/package-lock.json", "relay/worker/package-lock.json"):
        path = ROOT / manifest
        if not path.exists():
            evidence.append({"inventory": manifest, "status": "missing"})
            continue
        lock = json.loads(path.read_text())
        direct = dict(lock["packages"][""].get("dependencies", {}))
        direct.update(lock["packages"][""].get("devDependencies", {}))
        for name, dependency in sorted(lock.get("packages", {}).items()):
            if not name or "version" not in dependency:
                continue
            package = name.rsplit("node_modules/", 1)[-1]
            component = {"type": "library", "name": package, "version": dependency["version"], "purl": "pkg:npm/"+package+"@"+dependency["version"],
                         "properties": [{"name": "werkbord:manifest", "value": manifest}, {"name": "werkbord:dependency", "value": "direct" if package in direct else "transitive"},
                                        {"name": "werkbord:usage", "value": "development" if dependency.get("dev") else "runtime"}]}
            if dependency.get("license"):
                component["licenses"] = [{"license": {"name": dependency["license"]}}]
            folder = path.parent / name
            if name.startswith("node_modules/") and ".." not in Path(name).parts:
                collect_notices(package, dependency["version"], folder, "development" if dependency.get("dev") else "runtime")
            components.append(component)
    # Stable ordering, no wall-clock metadata or random document IDs.
    components.sort(key=lambda c: (c["purl"], json.dumps(c.get("properties", []), sort_keys=True)))
    (out / "source-sbom.cdx.json").write_text(json.dumps({"bomFormat": "CycloneDX", "specVersion": "1.5", "version": 1, "components": components}, indent=2, sort_keys=True)+"\n")
    entries = sorted(notice_entries.values(), key=lambda entry: (entry["name"], entry["version"]))
    (out / "notices/INDEX.json").write_text(json.dumps(entries, sort_keys=True, indent=2)+"\n")
    with (out / "notices/THIRD_PARTY_LICENSES.txt").open("w") as text:
        text.write("Collected third-party notices for the resolved source/build graph.\nUninstalled optional/build dependencies and packages without root license files\nare listed as unavailable in INDEX.json; this collection is not a legal review.\n")
        for entry in entries:
            if not entry["files"]:
                continue
            text.write("\n\nPackage: "+entry["name"]+"@"+entry["version"]+" ("+entry["scope"]+")\n")
            for license_file in entry["files"]:
                text.write("\n["+license_file["name"]+"]\n")
                text.write((out / "notices" / license_file["copy"]).read_text(errors="replace"))
                text.write("\n")
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True)
    parser.add_argument("--scan", action="store_true")
    parser.add_argument("--native-desktop", action="store_true", help="also analyze the actual desktop,production entrypoint (requires native build dependencies)")
    parser.add_argument("--reviewed-preview-build-advisories", action="store_true", help="allow only the expiring, reviewed development-tool advisory in preview builds")
    parser.add_argument("--artifact", action="append", default=[], help="explicit executable to inventory/scan; universal Macs are scanned per slice")
    options = parser.parse_args()
    version = (ROOT / "cmd/werkbord/VERSION").read_text().strip()
    if options.reviewed_preview_build_advisories and not re.fullmatch(r"\d+\.\d+\.\d+-preview\.[1-9]\d*", version):
        raise ValueError("reviewed build advisories are permitted only for preview versions")
    out = Path(options.out).resolve()
    if out == ROOT or ROOT in out.parents and out.parts[len(ROOT.parts)] not in ("output", ".cache", "dist"):
        raise ValueError("write evidence outside sources or under output/, .cache/, dist/")
    out.mkdir(parents=True, exist_ok=False)
    results = inventory(out)
    commit = capture(["git", "rev-parse", "HEAD"])[1].strip()
    dirty = bool(capture(["git", "status", "--porcelain"])[1].strip())
    tools = {name: capture(command)[1].strip() for name, command in {"go": ["go", "version"], "node": ["node", "--version"], "npm": ["npm", "--version"]}.items()}
    external_clis = {}
    for executable in ("git", "gh", "claude", "codex", "gemini", "opencode"):
        if shutil.which(executable) is None:
            external_clis[executable] = {"installed": False, "liveAcceptance": "not tested"}
            continue
        try:
            code, raw, _ = capture([executable, "--version"], timeout=10)
            match = re.search(r"[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9_.-]+)?", raw)
            external_clis[executable] = {"installed": True, "version": match.group(0) if code == 0 and match else "unknown", "liveAcceptance": "not tested"}
        except subprocess.TimeoutExpired:
            external_clis[executable] = {"installed": True, "version": "unknown (version probe timed out)", "liveAcceptance": "not tested"}
    artifacts = []
    for index, argument in enumerate(options.artifact):
        binary = Path(argument).resolve(strict=True)
        info = {"name": binary.name, "sha256": hashlib.sha256(binary.read_bytes()).hexdigest()}
        slices = [None]
        if platform.system() == "Darwin":
            code, raw, _ = capture(["lipo", "-archs", str(binary)])
            if code == 0:
                slices = raw.split()
        info["slices"] = []
        for arch in slices:
            with tempfile.TemporaryDirectory(prefix="werkbord-scan-") as temp:
                target = binary
                if arch and len(slices) > 1:
                    target = Path(temp) / binary.name
                    subprocess.run(["lipo", str(binary), "-thin", arch, "-output", str(target)], check=True)
                label = f"artifact-{index}-{arch or 'native'}"
                code, raw, err = capture(["go", "version", "-m", "-json", str(target)])
                (out / (label+"-buildinfo.json")).write_text(raw)
                results.append({"check": label+"-buildinfo", "exit": code})
                info["slices"].append({"arch": arch or "native", "buildInfoExit": code})
                if code == 0:
                    info_data = json.loads(raw)
                    binary_components = [{"type": "framework", "name": "Go runtime", "version": info_data["GoVersion"]}]
                    for dependency in [info_data.get("Main", {})] + info_data.get("Deps", []):
                        if "Path" in dependency:
                            resolved = dependency.get("Replace", dependency)
                            binary_components.append({"type": "library", "name": dependency["Path"], "version": resolved.get("Version", "source build"), "properties": [{"name": "werkbord:source-replacement", "value": resolved.get("Path", dependency["Path"])}]})
                    (out / (label+"-sbom.cdx.json")).write_text(json.dumps({"bomFormat": "CycloneDX", "specVersion": "1.5", "version": 1, "components": binary_components}, sort_keys=True, indent=2)+"\n")
                if options.scan:
                    code, raw, err = capture(["go", "run", "golang.org/x/vuln/cmd/govulncheck@v1.8.0", "-json", "-mode=binary", str(target)])
                    (out / (label+".json")).write_text(raw)
                    (out / (label+".stderr.txt")).write_text(err)
                    results.append({"check": label, "exit": code})
        artifacts.append(info)
    if options.scan:
        for module, label in ((".", "go-root"), ("desktop", "go-desktop")):
            command = ["go", "run", "golang.org/x/vuln/cmd/govulncheck@v1.8.0", "-json"]
            if module == "desktop" and options.native_desktop:
                command += ["-tags", "desktop,production"]
            code, raw, err = capture(command + ["./..."], ROOT / module)
            (out / (label+"-vuln.json")).write_text(raw)
            (out / (label+"-vuln.stderr.txt")).write_text(err)
            results.append({"check": label, "exit": code, "nativeDesktopTags": module == "desktop" and options.native_desktop})
        for module in ("web", "relay/worker"):
            code, raw, err = capture(["npm", "audit", "--json"], ROOT / module)
            name = module.replace("/", "-")
            (out / (name+"-audit.json")).write_text(raw)
            (out / (name+"-audit.stderr.txt")).write_text(err)
            result = {"check": module, "exit": code}
            if code == 1 and module == "web" and options.reviewed_preview_build_advisories:
                review = json.loads((ROOT / "scripts/preview-build-advisories.json").read_text())
                lock = json.loads((ROOT / "web/package-lock.json").read_text())
                sources = (path.read_text() for folder in (ROOT / "web/src", ROOT / "web/shell", ROOT / "internal/uipresentation/static") for path in folder.rglob("*") if path.is_file() and path.suffix in (".ts", ".js", ".svelte"))
                if reviewed_preview_advisory(json.loads(raw), lock, review, version, runtime_sources=sources):
                    result.update(exit=0, scannerExit=code, reviewedPreviewAdvisory=review)
                    print(f"WARNING: preview build-only advisory retained: {review['advisory']} (review expires {review['expiresAt']})", file=sys.stderr)
            results.append(result)
            if code:
                try:
                    for dependency, finding in json.loads(raw).get("vulnerabilities", {}).items():
                        print(f"npm audit: {module}: {dependency}: {finding.get('severity', 'unknown')}", file=sys.stderr)
                except (ValueError, AttributeError):
                    pass
    report = {"schema": 1, "version": version, "sourceCommit": commit, "dirty": dirty,
              "tools": tools, "externalCLIs": external_clis, "artifacts": artifacts, "checks": results,
              "acceptance": "pending", "limitations": ["Source SBOM is not a binary/license completeness claim", "No customer account, credentials, customer data or private key collected", "No clean-machine, independent-host, live-provider or soak acceptance inferred"]}
    (out / "audit.json").write_text(json.dumps(report, sort_keys=True, indent=2)+"\n")
    for result in results:
        if result.get("exit", 0) or result.get("status") == "missing":
            print(f"audit failed: {result.get('check', result.get('inventory'))}: exit {result.get('exit', 'missing')}; details in {out}", file=sys.stderr)
    print(out / "audit.json")
    return int(any(result.get("exit", 0) != 0 or result.get("status") == "missing" for result in results))


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, OSError, RuntimeError, subprocess.SubprocessError) as error:
        sys.exit(f"audit-release: {error}")
