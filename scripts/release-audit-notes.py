#!/usr/bin/env python3
"""Disclose preview audit exceptions from the exact candidate's retained evidence."""
import json
from pathlib import Path
import sys
import tarfile


def main(archive_path, notes_path):
    reviews = {}
    with tarfile.open(archive_path, "r:gz") as archive:
        for scope in ("source", "native"):
            audit = json.load(archive.extractfile(f"./{scope}/audit.json"))
            for check in audit["checks"]:
                if "reviewedPreviewAdvisory" in check:
                    review = check["reviewedPreviewAdvisory"]
                    reviews[review["advisory"]] = review
    if reviews:
        with Path(notes_path).open("a") as notes:
            notes.write("\n### Reviewed build dependency advisory\n\n")
            for review in reviews.values():
                notes.write("A low-severity advisory affects a build-only dependency: " + review["advisory"] + ". The preview retains the reviewed finding and scanner evidence in `release-audit.tar.gz`; no upstream fix was available at review time.\n")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: release-audit-notes.py AUDIT_ARCHIVE NOTES_FILE")
    main(sys.argv[1], sys.argv[2])
