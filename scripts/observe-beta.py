#!/usr/bin/env python3
"""Opt-in local resource/health observations. No logs, arguments, credentials or customer content."""
import argparse
import datetime
import ipaddress
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def sample(pid, url):
    result = {"time": datetime.datetime.now(datetime.timezone.utc).isoformat(), "healthStatus": None, "rssKiB": None, "fileDescriptors": None}
    if pid:
        process = subprocess.run(["ps", "-p", str(pid), "-o", "rss="], capture_output=True, text=True)
        if process.returncode == 0 and process.stdout.strip().isdigit():
            result["rssKiB"] = int(process.stdout.strip())
        descriptors = Path(f"/proc/{pid}/fd")
        if descriptors.is_dir():
            try:
                result["fileDescriptors"] = len(list(descriptors.iterdir()))
            except OSError:
                pass
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    try:
        with urllib.request.build_opener(NoRedirect).open(url, timeout=5) as response:
            result["healthStatus"] = response.status
    except (urllib.error.URLError, TimeoutError):
        result["healthStatus"] = "unreachable"
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--pid", type=int)
    parser.add_argument("--health-url", default="http://127.0.0.1:7420/api/health")
    parser.add_argument("--duration-seconds", type=int, default=259200)
    parser.add_argument("--interval-seconds", type=int, default=60)
    args = parser.parse_args()
    parsed = urllib.parse.urlsplit(args.health_url)
    if parsed.scheme != "http" or parsed.path != "/api/health" or parsed.query or parsed.fragment or parsed.username or parsed.password or not ipaddress.ip_address(parsed.hostname).is_loopback:
        parser.error("health URL must be literal loopback HTTP /api/health without credentials/query")
    if args.duration_seconds < 0 or args.interval_seconds < 1 or (args.pid is not None and args.pid < 1):
        parser.error("invalid observation duration, interval or PID")
    args.out.parent.mkdir(parents=True, exist_ok=True)
    deadline = time.monotonic()+args.duration_seconds
    samples = []
    with args.out.open("x") as output:
        try:
            while True:
                point = sample(args.pid, args.health_url)
                samples.append(point)
                output.write(json.dumps(point, sort_keys=True)+"\n")
                output.flush()
                remaining = deadline-time.monotonic()
                if remaining <= 0:
                    break
                time.sleep(min(args.interval_seconds, remaining))
        except KeyboardInterrupt:
            pass
    rss = [point["rssKiB"] for point in samples if point["rssKiB"] is not None]
    summary = {"samples": len(samples), "completedDuration": time.monotonic() >= deadline,
               "healthFailures": sum(point["healthStatus"] != 200 for point in samples),
               "rssGrowthFraction": (rss[-1]-rss[0])/rss[0] if rss and rss[0] else None,
               "productionAcceptance": "pending", "limitations": "Passive observations do not exercise workloads or prove provider, quorum, recovery, heap/goroutine or physical-device acceptance. Review growth after warmup against a fixed workload."}
    args.out.with_suffix(args.out.suffix+".summary.json").write_text(json.dumps(summary, sort_keys=True, indent=2)+"\n")
    print(args.out)
    return int(not summary["completedDuration"] or summary["healthFailures"] != 0)


if __name__ == "__main__":
    sys.exit(main())
