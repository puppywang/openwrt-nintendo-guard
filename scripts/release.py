"""Validate complete build artifacts and publish a checked GitHub preview release."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

from build_native import ARCHES

ROOT = Path(__file__).resolve().parents[1]


def version(tag):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        raise ValueError("Release version must be vX.Y.Z")
    return tag


def expected_assets(tag):
    number = version(tag)[1:]
    names = {f"nintendo-guard-{tag}-linux-{arch}.tar.gz" for arch in ("arm64", "amd64", "armv7", "mipsle", "mips")}
    for arch in ARCHES:
        names.add(f"nintendo-guard_{number}-1_{arch}.ipk")
        names.add(f"nintendo-guard-{number}-r1.{arch}.apk")
    names.add(f"luci-app-nintendo-guard_{number}-1_all.ipk")
    names.add(f"luci-app-nintendo-guard-{number}-r1.noarch.apk")
    return names


def parse_manifest(data, tag):
    checks = {}
    for line in data.decode("ascii").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9_.-]+)", line)
        if not match or match[2] in checks:
            raise ValueError("Invalid or duplicate SHA256SUMS entry")
        checks[match[2]] = match[1]
    if set(checks) != expected_assets(tag):
        raise ValueError("SHA256SUMS must contain every expected APK, IPK and tar bundle")
    return checks


def validate_artifacts(directory, tag):
    data = (directory / "SHA256SUMS").read_bytes()
    checks = parse_manifest(data, tag)
    checks["SHA256SUMS"] = hashlib.sha256(data).hexdigest()
    if {p.name for p in directory.iterdir()} != set(checks):
        raise ValueError("Artifact directory contains missing or unexpected files")
    for name, digest in checks.items():
        path = directory / name
        if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError("Artifact integrity check failed: " + name)
    return checks


def gh(*args, check=True):
    result = subprocess.run(["gh", *args], capture_output=True, timeout=300)
    if check and result.returncode:
        raise RuntimeError(result.stderr.decode("utf-8", "replace"))
    return result


def get_release(repo, tag):
    result = gh("release", "view", tag, "--repo", repo, "--json", "isDraft,url,assets", check=False)
    if result.returncode:
        if "not found" in result.stderr.decode("utf-8", "replace").lower():
            return None
        raise RuntimeError(result.stderr.decode("utf-8", "replace"))
    return json.loads(result.stdout)


def verify_remote(repo, tag, release, checks=None):
    assets = {item["name"]: item for item in release["assets"]}
    if set(assets) != expected_assets(tag) | {"SHA256SUMS"}:
        raise ValueError("Release has missing or unexpected assets")
    if checks is None:
        # A rerun may have different tar timestamps. Validate the already-public
        # release against its own manifest instead of replacing public assets.
        manifest = assets["SHA256SUMS"]
        endpoint = manifest["apiUrl"]
        if not endpoint.startswith("https://api.github.com/repos/" + repo + "/releases/assets/"):
            raise ValueError("Unexpected release asset URL")
        data = gh("api", endpoint, "--header", "Accept: application/octet-stream").stdout
        checks = parse_manifest(data, tag)
        checks["SHA256SUMS"] = hashlib.sha256(data).hexdigest()
    for name, digest in checks.items():
        asset = assets[name]
        if asset["state"] != "uploaded" or asset.get("digest") != "sha256:" + digest:
            raise ValueError("GitHub asset digest mismatch: " + name)


def publish(repo, tag, directory, commit):
    checks = validate_artifacts(directory, tag)
    release = get_release(repo, tag)
    if release and not release["isDraft"]:
        verify_remote(repo, tag, release)
        print("Already published and verified; public assets retained: " + release["url"])
        return
    assets = [str(directory / name) for name in sorted(checks)]
    if release:
        gh("release", "upload", tag, "--repo", repo, "--clobber", *assets)
    else:
        args = ["release", "create", tag, "--repo", repo, "--verify-tag", "--target", commit,
                "--draft", "--prerelease", "--latest=false", "--title", tag + " Preview"]
        notes = ROOT / "docs" / ("release-" + tag + ".md")
        args += ["--notes-file", str(notes)] if notes.is_file() else ["--generate-notes"]
        gh(*args, *assets)
    release = get_release(repo, tag)
    verify_remote(repo, tag, release, checks)
    gh("release", "edit", tag, "--repo", repo, "--draft=false", "--prerelease", "--latest=false")
    release = get_release(repo, tag)
    if release["isDraft"]:
        raise RuntimeError("Release is still a draft")
    verify_remote(repo, tag, release, checks)
    print("Published and verified " + str(len(checks)) + " assets: " + release["url"])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["version", "check", "verify", "publish"])
    parser.add_argument("--tag", default="")
    parser.add_argument("--directory", type=Path)
    parser.add_argument("--repo", default=os.getenv("GH_REPO", ""))
    parser.add_argument("--commit", default=os.getenv("GITHUB_SHA", ""))
    args = parser.parse_args()
    tag = version(args.tag or (ROOT / "VERSION").read_text().strip())
    if args.command == "version":
        if os.getenv("GITHUB_OUTPUT"):
            with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
                stream.write("version=" + tag + "\n")
        print(tag)
        return
    if args.command in ("verify", "publish") and not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repo):
        parser.error("A GitHub OWNER/REPO is required")
    if args.command == "verify":
        release = get_release(args.repo, tag)
        if not release or release["isDraft"]:
            raise ValueError("No published release found")
        verify_remote(args.repo, tag, release)
        print("Verified published assets: " + release["url"])
        return
    directory = args.directory or ROOT / "dist" / tag
    if args.command == "check":
        print("Verified " + str(len(validate_artifacts(directory, tag))) + " local artifacts")
    else:
        if not re.fullmatch(r"[0-9a-f]{40}", args.commit):
            parser.error("The tested commit SHA is required")
        publish(args.repo, tag, directory, args.commit)


if __name__ == "__main__":
    main()
