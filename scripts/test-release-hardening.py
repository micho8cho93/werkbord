#!/usr/bin/env python3
"""Security regressions for exact-byte candidate review and preview package ordering."""
import base64
import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("candidate", ROOT / "scripts/release-candidate.py")
candidate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(candidate)


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

    def test_preview_version_preserves_debian_order(self):
        script = ROOT / "scripts/debian-version.sh"
        self.assertEqual(subprocess.check_output([str(script), "4.89.0-preview.10"], text=True).strip(), "4.89.0~preview.10")
        self.assertNotEqual(subprocess.run([str(script), "4.89.0-preview.0"], capture_output=True).returncode, 0)
        if subprocess.run(["sh", "-c", "command -v dpkg >/dev/null"], capture_output=True).returncode == 0:
            subprocess.run(["dpkg", "--compare-versions", "4.89.0~preview.10", "lt", "4.89.0"], check=True)


if __name__ == "__main__":
    unittest.main()
