#!/usr/bin/env python3
"""Security regressions for exact-byte candidate review and preview package ordering."""
import base64
import copy
import datetime
import importlib.util
import json
import os
from pathlib import Path
import functools
import http.server
import io
import platform
import subprocess
import tarfile
import tempfile
import threading
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("candidate", ROOT / "scripts/release-candidate.py")
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)
audit_spec = importlib.util.spec_from_file_location("audit", ROOT / "scripts/audit-release.py")
audit = importlib.util.module_from_spec(audit_spec)
audit_spec.loader.exec_module(audit)


class InfrastructurePublicationTests(unittest.TestCase):
    def test_concurrent_builds_publish_complete_matching_outputs(self):
        spec = importlib.util.spec_from_file_location("infrastructure", ROOT / "scripts/build-infrastructure.py")
        infrastructure = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(infrastructure)
        with tempfile.TemporaryDirectory() as work:
            root = Path(work)
            dest = root / "cache"
            dest.mkdir()
            errors = []
            barrier = threading.Barrier(8)
            def writer(index):
                try:
                    source = root / str(index)
                    source.mkdir()
                    for name in ("nebula", "go.mod", "go.sum"):
                        (source / name).write_text(str(index))
                    barrier.wait(timeout=10)
                    infrastructure.publish(dest, source / "nebula", source, {"build": index})
                except Exception as error:
                    errors.append(error)
            writers = [threading.Thread(target=writer, args=(index,)) for index in range(8)]
            for writer_thread in writers:
                writer_thread.start()
            for writer_thread in writers:
                writer_thread.join(timeout=15)
            self.assertFalse(any(writer_thread.is_alive() for writer_thread in writers))
            self.assertEqual(errors, [])
            winner = str(json.loads((dest / "nebula.build").read_text())["build"])
            for name in ("nebula", "go.mod", "go.sum"):
                self.assertEqual((dest / name).read_text(), winner)
            self.assertEqual((dest / "nebula").stat().st_mode & 0o777, 0o755)
            self.assertEqual(list(dest.glob(".publish-*")), [])


class PreviewAdvisoryTests(unittest.TestCase):
    def setUp(self):
        self.review = json.loads((ROOT / "scripts/preview-build-advisories.json").read_text())
        self.lock = {"packages": {"node_modules/"+name: {"version": version, "dev": True, "integrity": self.review["integrities"][name]} for name, version in self.review["packages"].items()}}
        self.scan = {"auditReportVersion": 2, "vulnerabilities": {
            "deepmerge": {"severity": "low", "nodes": ["node_modules/deepmerge"], "fixAvailable": False, "via": [{"url": self.review["advisory"], "severity": "low"}]},
            "@sveltejs/vite-plugin-svelte": {"severity": "low", "nodes": ["node_modules/@sveltejs/vite-plugin-svelte"], "fixAvailable": False, "via": ["deepmerge"]}
        }}

    def accepted(self, version="5.6.0-preview.4", date="2026-10-28"):
        return audit.reviewed_preview_advisory(self.scan, self.lock, self.review, version, datetime.date.fromisoformat(date))

    def test_only_the_exact_reviewed_preview_build_finding_is_accepted(self):
        self.assertTrue(self.accepted())
        self.assertFalse(self.accepted("5.6.0"))
        self.assertFalse(self.accepted("5.6.0-rc.1"))
        self.assertFalse(self.accepted(date="2026-11-29"))
        self.assertFalse(self.accepted(date="2026-10-27"))
        self.assertFalse(audit.reviewed_preview_advisory(self.scan, self.lock, self.review, "5.6.0-preview.4", datetime.date(2026, 10, 28), ["import merge from 'deepmerge';"]))

    def test_runtime_changes_new_findings_and_scanner_errors_stay_blocked(self):
        for change in (
            lambda: self.lock["packages"]["node_modules/deepmerge"].update(dev=False),
            lambda: self.lock["packages"]["node_modules/deepmerge"].update(version="4.3.2"),
            lambda: self.lock["packages"]["node_modules/deepmerge"].update(integrity="changed"),
            lambda: self.scan.update(error={"code": "EAUDITNOLOCK"}),
            lambda: self.scan["vulnerabilities"].update(other={}),
            lambda: self.scan["vulnerabilities"]["deepmerge"].update(severity="high"),
            lambda: self.scan["vulnerabilities"]["deepmerge"].update(fixAvailable={"version": "4.3.2"}),
            lambda: self.scan["vulnerabilities"]["deepmerge"]["via"].append({"url": "https://github.com/advisories/another", "severity": "low"}),
            lambda: self.scan["vulnerabilities"]["deepmerge"]["nodes"].append("node_modules/another/node_modules/deepmerge"),
        ):
            saved_scan, saved_lock = copy.deepcopy(self.scan), copy.deepcopy(self.lock)
            change()
            self.assertFalse(self.accepted())
            self.scan, self.lock = saved_scan, saved_lock

    def test_release_notes_disclose_only_a_retained_advisory(self):
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "audit.tar.gz"
            notes = Path(tmp) / "notes.md"
            finding = [{"reviewedPreviewAdvisory": self.review}]
            for source, native, expected in (([], [], False), (finding, [], True), ([], finding, True), (finding, finding, True)):
                with tarfile.open(archive, "w:gz") as bundle:
                    for scope, checks in (("source", source), ("native", native)):
                        data = json.dumps({"checks": checks}).encode()
                        info = tarfile.TarInfo(f"./{scope}/audit.json")
                        info.size = len(data)
                        bundle.addfile(info, io.BytesIO(data))
                notes.write_text("Release notes\n")
                subprocess.run(["python3", str(ROOT / "scripts/release-audit-notes.py"), str(archive), str(notes)], check=True)
                self.assertEqual(self.review["advisory"] in notes.read_text(), expected)
                self.assertLessEqual(notes.read_text().count(self.review["advisory"]), 1)


class CandidateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="werkbord-release-test-")
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.assets = self.work / "candidate"
        self.assets.mkdir()
        self.tag = "werkbord-v4.89.0-preview.1"
        self.commit = "a"*40
        for name in ("Werkbord_4.89.0-preview.1_darwin_universal.dmg", "Werkbord_4.89.0-preview.1_darwin_universal.zip", "Werkbord.dmg", "appcast.xml", "release-audit.tar.gz", "werkbord_4.89.0-preview.1_darwin_amd64.tar.gz", "werkbord_4.89.0-preview.1_darwin_arm64.tar.gz"):
            (self.assets / name).write_bytes(b"explicit packaging fixture, not a real release")

    def sign(self):
        private = self.work / "fixture-private.pem"
        subprocess.run(["openssl", "genpkey", "-algorithm", "ED25519", "-out", str(private)], check=True, capture_output=True)
        public = subprocess.check_output(["openssl", "pkey", "-in", str(private), "-pubout", "-outform", "DER"])[-32:]
        message = self.work / "message"
        message.write_bytes(b"werkbord/release/v1\0"+self.tag.encode()+b"\0"+(self.assets / "checksums.txt").read_bytes())
        sig = subprocess.check_output(["openssl", "pkeyutl", "-sign", "-inkey", str(private), "-rawin", "-in", str(message)])
        return {"RELEASE_PUBLIC_KEY": base64.urlsafe_b64encode(public).rstrip(b"=").decode(), "RELEASE_MANIFEST_SIGNATURE": base64.b64encode(sig).decode()}

    def test_exact_bytes_source_and_signature_are_required(self):
        candidate.create(self.assets, self.tag, self.commit, False)
        env = self.sign()
        with patch.dict(os.environ, env):
            candidate.verify(self.assets, self.tag, self.commit)
            with self.assertRaises(ValueError):
                candidate.verify(self.assets, self.tag, "b"*40)
            (self.assets / "Werkbord.dmg").write_bytes(b"changed after review")
            with self.assertRaises(ValueError):
                candidate.verify(self.assets, self.tag, self.commit)

    def test_a_preview_exception_checks_every_byte_but_never_applies_to_stable_or_a_signed_candidate(self):
        candidate.create(self.assets, self.tag, self.commit, False)
        candidate.verify(self.assets, self.tag, self.commit, allow_unsigned_manifest=True)
        self.assertIn("without the owner's offline release-manifest signature", (self.assets / "manifest-unsigned.txt").read_text())
        self.assertFalse((self.assets / "checksums.txt.sig").exists())
        (self.assets / "manifest-unsigned.txt").unlink()
        with self.assertRaises(ValueError):
            candidate.verify(self.assets, self.tag, "b"*40, allow_unsigned_manifest=True)
        (self.assets / "Werkbord.dmg").write_bytes(b"changed after assembly")
        with self.assertRaises(ValueError):
            candidate.verify(self.assets, self.tag, self.commit, allow_unsigned_manifest=True)
        with self.assertRaisesRegex(ValueError, "only a preview"):
            candidate.verify(self.assets, "werkbord-v4.89.0", self.commit, allow_unsigned_manifest=True)
        (self.assets / "Werkbord.dmg").write_bytes(b"explicit packaging fixture, not a real release")
        (self.assets / "checksums.txt.sig").write_bytes(b"x"*64)
        with self.assertRaisesRegex(ValueError, "must be verified"):
            candidate.verify(self.assets, self.tag, self.commit, allow_unsigned_manifest=True)

    def test_unsigned_extra_asset_is_rejected(self):
        candidate.create(self.assets, self.tag, self.commit, False)
        with patch.dict(os.environ, self.sign()):
            (self.assets / "injected.exe").write_bytes(b"unsigned")
            with self.assertRaises(ValueError):
                candidate.verify(self.assets, self.tag, self.commit)

    def test_linux_cannot_be_declared_without_both_architectures(self):
        with self.assertRaises(ValueError):
            candidate.create(self.assets, self.tag, self.commit, True)

    def test_stable_cannot_use_pending_acceptance(self):
        # Match stable artifact names so the actual admission ledger is the refusing boundary.
        for path in self.assets.iterdir():
            if "-preview.1" in path.name: path.rename(path.with_name(path.name.replace("-preview.1", "")))
        fixtures = self.work / "fixture-root"
        (fixtures / "docs").mkdir(parents=True)
        (fixtures / "docs/RELEASE_ACCEPTANCE.json").write_text('{"version":null,"requirements":{}}')
        with patch.object(candidate,"ROOT",fixtures), self.assertRaisesRegex(ValueError,"acceptance evidence"):
            candidate.create(self.assets,"werkbord-v4.89.0",self.commit,False)

    def test_stable_requires_unified_platform_evidence(self):
        for path in self.assets.iterdir():
            if "-preview.1" in path.name:
                path.rename(path.with_name(path.name.replace("-preview.1", "")))
        fixtures = self.work / "fixture-root"
        (fixtures / "docs").mkdir(parents=True)
        requirements = {name: {"status": "accepted", "evidence": "disposable test evidence"}
                        for name in candidate.REQUIRED if name != "unifiedPlatform"}
        ledger = {"version": "4.89.0", "requirements": requirements}
        def write_ledger():
            (fixtures / "docs/RELEASE_ACCEPTANCE.json").write_text(json.dumps(ledger))
        with patch.object(candidate, "ROOT", fixtures):
            write_ledger()
            with self.assertRaisesRegex(ValueError, "acceptance evidence"):
                candidate.create(self.assets, "werkbord-v4.89.0", self.commit, False)
            requirements["unifiedPlatform"] = {"status": "pending", "evidence": "checklist only"}
            write_ledger()
            with self.assertRaisesRegex(ValueError, "acceptance evidence"):
                candidate.create(self.assets, "werkbord-v4.89.0", self.commit, False)
            requirements["unifiedPlatform"] = {"status": "accepted", "evidence": "disposable test evidence"}
            write_ledger()
            candidate.create(self.assets, "werkbord-v4.89.0", self.commit, False)

    def test_preview_version_preserves_debian_order(self):
        script = ROOT / "scripts/debian-version.sh"
        self.assertEqual(subprocess.check_output([str(script), "4.89.0-preview.10"], text=True).strip(), "4.89.0~preview.10")
        self.assertNotEqual(subprocess.run([str(script), "4.89.0-preview.0"], capture_output=True).returncode, 0)
        if subprocess.run(["sh", "-c", "command -v dpkg >/dev/null"], capture_output=True).returncode == 0:
            subprocess.run(["dpkg", "--compare-versions", "4.89.0~preview.10", "lt", "4.89.0"], check=True)


@unittest.skipUnless(platform.system() == "Darwin", "a disk image is checked on macOS")
class PublicVerifierExceptionTests(unittest.TestCase):
    """verify-desktop-release.sh authenticates a published release; one named preview may lack the owner's manifest signature."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="werkbord-verify-test-")
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        # No retries: a missing manifest is the case under test, and the script retries the missing files for a minute.
        self.curl = self.work / "curl-without-retries"
        self.curl.write_text("#!/usr/bin/env python3\nimport os, sys\nargs = sys.argv[1:]\nout = []\ni = 0\n"
                             "while i < len(args):\n    if args[i] in ('--retry', '--retry-delay'): i += 2; continue\n"
                             "    if args[i] != '--retry-all-errors': out.append(args[i])\n    i += 1\nos.execvp('curl', ['curl'] + out)\n")
        self.curl.chmod(0o755)
        self.root = self.work / "site"
        class Quiet(http.server.SimpleHTTPRequestHandler):
            def log_message(self, *args):
                pass
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Quiet, directory=str(self.root)))
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def release(self, tag):
        version = tag[len("werkbord-v"):]
        directory = self.root / tag
        directory.mkdir(parents=True)
        dmg = directory / f"Werkbord_{version}_darwin_universal.dmg"
        dmg.write_bytes(b"not a real disk image")
        digest = subprocess.check_output(["shasum", "-a", "256", str(dmg)], text=True).split()[0]
        (directory / (dmg.name + ".sha256")).write_text(f"{digest}  {dmg.name}\n")

    def verify(self, tag, allow_unsigned_preview):
        env = dict(os.environ, WERKBORD_RELEASE_BASE=f"http://127.0.0.1:{self.server.server_address[1]}", CURL=str(self.curl))
        env.pop("WERKBORD_ALLOW_UNSIGNED_PREVIEW", None)
        if allow_unsigned_preview is not None:
            env["WERKBORD_ALLOW_UNSIGNED_PREVIEW"] = allow_unsigned_preview
        run = subprocess.run([str(ROOT / "scripts/verify-desktop-release.sh"), tag], capture_output=True, text=True, env=env)
        return run.returncode, run.stdout + run.stderr

    def test_a_preview_may_lack_the_manifest_signature_and_a_stable_release_never_may(self):
        preview = "werkbord-v4.89.0-preview.1"
        self.release(preview)
        code, out = self.verify(preview, None)  # the script alone is strict
        self.assertNotEqual(code, 0)
        self.assertIn("signed release manifest is missing", out)
        code, out = self.verify(preview, "0")  # only the workflow's explicit 1 counts
        self.assertIn("signed release manifest is missing", out)
        code, out = self.verify(preview, "1")  # a preview: it continues to the Apple checks, which a fake image fails
        self.assertIn("WARNING", out)
        self.assertNotIn("signed release manifest is missing", out)
        self.assertIn("the disk image's signature does not verify", out)
        stable = "werkbord-v4.89.0"
        self.release(stable)
        code, out = self.verify(stable, "1")  # never for a stable release
        self.assertIn("signed release manifest is missing", out)
        self.assertNotIn("WARNING", out)


if __name__ == "__main__":
    unittest.main()
