"""Build real opkg IPK and APKv3 packages from the cross-built static binaries."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
URL = "https://github.com/puppywang/openwrt-nintendo-guard"
ARCHES = {
    "aarch64_cortex-a53": "arm64", "aarch64_generic": "arm64",
    "x86_64": "amd64", "arm_cortex-a7_neon-vfpv4": "armv7",
    "mipsel_24kc": "mipsle", "mips_24kc": "mips",
}
BACKEND_DEPS = ["busybox", "firewall4", "nftables", "conntrack", "ip-full", "procd"]


def payload(binary=None, package="nintendo-guard"):
    def source(path, mode=0o644):
        return (ROOT.joinpath(path).read_bytes(), mode)
    if package == "nintendo-guard":
        return {
            "usr/sbin/nintendo-guard": (Path(binary).read_bytes(), 0o755),
            "etc/init.d/nintendo-control": source("luci/nintendo-control.init", 0o755),
            "usr/libexec/nintendo-control-firewall": source("luci/nintendo-control-firewall", 0o755),
            "usr/libexec/nintendo-control-package": source("packaging/lifecycle.sh", 0o755),
            "usr/share/nintendo-guard/default.json": (b'{"mode":"off","mac":"","upstream":"127.0.0.1:53"}\n', 0o644),
            "lib/upgrade/keep.d/nintendo-guard": (b"/etc/nintendo-control.json\n", 0o644),
            "etc/uci-defaults/95-nintendo-control": (b'#!/bin/sh\n/usr/libexec/nintendo-control-package configure\n', 0o755),
            "usr/share/licenses/nintendo-guard/LICENSE": source("LICENSE"),
            "usr/share/licenses/nintendo-guard/THIRD_PARTY_LICENSES.txt": source("THIRD_PARTY_LICENSES.txt"),
        }
    return {
        "usr/libexec/rpcd/nintendo-control": source("luci/nintendo-control.rpc", 0o755),
        "usr/share/luci/menu.d/luci-app-nintendo-control.json": source("luci/menu.json"),
        "usr/share/rpcd/acl.d/luci-app-nintendo-control.json": source("luci/acl.json"),
        "www/luci-static/resources/view/network/nintendo.js": source("luci/nintendo.js"),
    }


def hook(action):
    if action == "ui":
        return (b'#!/bin/sh\n[ -z "${IPKG_INSTROOT:-}" ] || exit 0\n'
                b'for path in /tmp/luci-indexcache.*.json; do [ ! -f "$path" ] || rm "$path"; done\n'
                b'[ ! -x /etc/init.d/rpcd ] || /etc/init.d/rpcd restart\n')
    return ('#!/bin/sh\n[ -z "${IPKG_INSTROOT:-}" ] || exit 0\n'
            'if [ -x /usr/libexec/nintendo-control-package ]; then\n'
            ' exec /usr/libexec/nintendo-control-package '+action+'\nfi\n').encode()


def hooks(package, fmt):
    if package == "nintendo-guard":
        post = hook("configure")
        # opkg also calls prerm for upgrade; retain blocking rules then.
        remove = b'#!/bin/sh\n[ "${1:-}" != upgrade ] || exit 0\n' + hook("remove").split(b"\n", 1)[1]
    else:
        post = hook("ui")
        remove = b"#!/bin/sh\nexit 0\n"
    if fmt == "ipk":
        result = {"postinst": post, "prerm": remove}
        if package != "nintendo-guard": result["postrm"] = hook("ui")
        return result
    result = {"post-install": post, "post-upgrade": post, "pre-deinstall": remove}
    if package != "nintendo-guard": result["post-deinstall"] = hook("ui")
    return result


def tar_bytes(files, compressed=True):
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.GNU_FORMAT) as archive:
        directories = set()
        for name in files:
            for parent in Path(name).parents:
                if str(parent) != ".": directories.add(parent.as_posix())
        for name in sorted(directories):
            entry = tarfile.TarInfo("./"+name); entry.type=tarfile.DIRTYPE; entry.mode=0o755
            archive.addfile(entry)
        for name,(data,mode) in sorted(files.items()):
            entry=tarfile.TarInfo("./"+name);entry.size=len(data);entry.mode=mode
            archive.addfile(entry,io.BytesIO(data))
    return gzip.compress(raw.getvalue(),mtime=0) if compressed else raw.getvalue()


def ipk_bytes(name, version, arch, files, depends):
    control = (f"Package: {name}\nVersion: {version}\nArchitecture: {arch}\n"
               f"Maintainer: puppywang <puppywang@gmail.com>\nLicense: MIT\nSection: net\n"
               f"Depends: {', '.join(depends)}\nInstalled-Size: {sum(len(v[0]) for v in files.values())}\n"
               f"Description: Per-device Nintendo update controls for firewall4\nURL: {URL}\n").encode()
    metadata={"control":(control,0o644)}
    metadata.update({k:(v,0o755) for k,v in hooks(name,"ipk").items()})
    # OpenWrt's ipkg-build uses a gzip tar containing these three members.
    return tar_bytes({"debian-binary":(b"2.0\n",0o644),
                      "control.tar.gz":(tar_bytes(metadata),0o644),
                      "data.tar.gz":(tar_bytes(files),0o644)})


def apk_file(tool, output, name, version, arch, files, depends, provides=()):
    with tempfile.TemporaryDirectory(prefix="nintendo-apk-") as work:
        root = Path(work)/"root"; root.mkdir()
        files = dict(files)
        list_path = "lib/apk/packages/"+name+".list"
        files[list_path] = (("\n".join("/"+p for p in sorted(list(files)+[list_path]))+"\n").encode(),0o644)
        if name == "nintendo-guard":
            files["lib/apk/packages/nintendo-guard.conffiles"]=(b"/etc/nintendo-control.json\n",0o644)
        for relative,(data,mode) in files.items():
            target=root/relative;target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(data);target.chmod(mode)
        args=[str(tool),"mkpkg","--info","name:"+name,"--info","version:"+version,
              "--info","arch:"+arch,"--info","license:MIT","--info","url:"+URL,
              "--info","description:Per-device Nintendo update controls for firewall4",
              "--info","maintainer:puppywang <puppywang@gmail.com>",
              "--info","depends:"+" ".join(depends),"--files",str(root),"--output",str(output)]
        if provides: args.extend(["--info", "provides:"+" ".join(provides)])
        for kind,data in hooks(name,"apk").items():
            path=Path(work)/kind;path.write_bytes(data);path.chmod(0o755)
            args.extend(["--script",kind+":"+str(path)])
        subprocess.run(args,check=True,timeout=45,env=dict(os.environ,SOURCE_DATE_EPOCH="0"))


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version",required=True)
    parser.add_argument("--arch",choices=ARCHES,action="append")
    parser.add_argument("--apk",type=Path,help="full apk-tools 3 executable with mkpkg")
    args=parser.parse_args()
    version=args.version.removeprefix("v")
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+",version): parser.error("Use a numeric x.y.z release version")
    out=ROOT/"dist"/args.version;out.mkdir(parents=True,exist_ok=True)
    targets=[("nintendo-guard",arch,payload(ROOT/"build"/ARCHES[arch]/"nintendo-guard"),BACKEND_DEPS) for arch in args.arch or ARCHES]
    targets.append(("luci-app-nintendo-guard","all",payload(package="luci-app-nintendo-guard"),["luci-base","rpcd","nintendo-guard"]))
    for name,arch,files,deps in targets:
        ipk=out/f"{name}_{version}-1_{arch}.ipk"
        ipk.write_bytes(ipk_bytes(name,version+"-1",arch,files,deps));print(ipk.name,flush=True)
        if args.apk:
            apk=out/f"{name}-{version}-r1.{('noarch' if arch=='all' else arch)}.apk"
            apk_file(args.apk.resolve(),apk,name,version+"-r1","noarch" if arch=="all" else arch,files,deps)
            print(apk.name,flush=True)
    assets=sorted(p for p in out.iterdir() if p.suffix in (".apk",".ipk") or p.name.endswith(".tar.gz"))
    (out/"SHA256SUMS").write_text("".join(hashlib.sha256(p.read_bytes()).hexdigest()+"  "+p.name+"\n" for p in assets),encoding="ascii")


if __name__=="__main__": main()
