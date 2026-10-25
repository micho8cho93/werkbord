#!/usr/bin/env python3
"""Convert the independently configured public build stamp to an OpenSSL PEM."""
import base64
import os
from pathlib import Path
import re
import sys

key = os.environ.get("RELEASE_PUBLIC_KEY", "")
if not re.fullmatch(r"[A-Za-z0-9_-]{43}", key):
    sys.exit("independently configured RELEASE_PUBLIC_KEY is required")
raw = base64.urlsafe_b64decode(key+"=")
if len(raw) != 32 or base64.urlsafe_b64encode(raw).rstrip(b"=").decode() != key:
    sys.exit("invalid Ed25519 public key")
der = bytes.fromhex("302a300506032b6570032100") + raw
Path(sys.argv[1]).write_text("-----BEGIN PUBLIC KEY-----\n"+base64.b64encode(der).decode()+"\n-----END PUBLIC KEY-----\n")
