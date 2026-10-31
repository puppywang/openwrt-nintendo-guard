"""Build standalone Linux release bundles with Python 3 and Go (no CGO)."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--version", default="dev")
parser.add_argument("--arch", choices=["arm64", "amd64", "armv7", "mipsle", "mips"], action="append")
args = parser.parse_args()
if not re.fullmatch(r"[A-Za-z0-9._-]+", args.version):
    parser.error("Invalid version")
out = ROOT / "dist" / args.version
out.mkdir(parents=True, exist_ok=True)
archives = []
for arch in args.arch or ["arm64", "amd64", "armv7", "mipsle", "mips"]:
    env = dict(os.environ, GOOS="linux", GOARCH="arm" if arch == "armv7" else arch, CGO_ENABLED="0", GOARM="7", GOMIPS="softfloat")
    build = ROOT / "build" / arch
    build.mkdir(parents=True, exist_ok=True)
    binary = build / "nintendo-guard"
    subprocess.run(["go", "build", "-buildvcs=false", "-trimpath", "-ldflags", "-s -w -X main.version=" + args.version, "-o", str(binary), "./cmd/nintendo-guard"], cwd=ROOT, env=env, check=True, timeout=180)
    name = f"nintendo-guard-{args.version}-linux-{arch}"
    archive = out / (name + ".tar.gz")
    files = [(binary, "nintendo-guard")]
    files += [(ROOT / p, p) for p in ["install.sh", "uninstall.sh", "README.md", "README.en.md", "LICENSE", "THIRD_PARTY_LICENSES.txt"]]
    files += [(p, "luci/" + p.name) for p in sorted((ROOT / "luci").iterdir()) if p.is_file()]
    with tarfile.open(archive, "w:gz") as tar:
        for path, relative in files:
            entry = tar.gettarinfo(str(path), name + "/" + relative)
            entry.uid = entry.gid = 0
            entry.uname = entry.gname = ""
            entry.mode = 0o755 if relative == "nintendo-guard" or relative.endswith(".sh") else 0o644
            with path.open("rb") as stream:
                tar.addfile(entry, stream)
    archives.append(archive)
    print(archive.name, flush=True)
(out / "SHA256SUMS").write_text("".join(hashlib.sha256(p.read_bytes()).hexdigest() + "  " + p.name + "\n" for p in archives), encoding="ascii")
