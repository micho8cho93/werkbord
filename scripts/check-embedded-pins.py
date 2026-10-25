#!/usr/bin/env python3
"""Compare helper bytes to the validated service's actual runtime trust anchors.
The caller must verify the service/app code signature before this metadata probe.
Go -trimpath deliberately omits linker flags from buildinfo, so never infer pins
from build records or a substring found in executable bytes.
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

service, network, database = map(Path, sys.argv[1:])
info = json.loads(subprocess.check_output([str(service.resolve()), "version", "--json"], text=True, timeout=10))
expected = {"nebulaSHA256": hashlib.sha256(network.read_bytes()).hexdigest(),
            "rqliteSHA256": hashlib.sha256(database.read_bytes()).hexdigest()}
if any(info.get(name) != value for name, value in expected.items()):
    sys.exit("helper bytes do not match the validated service's compiled runtime trust anchors")
