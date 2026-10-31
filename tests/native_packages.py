"""Exercise real APK install/upgrade/remove in an isolated root and inspect IPKs."""
import argparse
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/"scripts"))
from build_native import apk_file, payload, hooks, BACKEND_DEPS


def run(*args, **kwargs):
    return subprocess.run([str(a) for a in args],capture_output=True,text=True,check=True,timeout=25,**kwargs)


def main():
    parser=argparse.ArgumentParser();parser.add_argument("--version",required=True);parser.add_argument("--apk",type=Path,required=True)
    args=parser.parse_args();apk=args.apk.resolve();version=args.version.removeprefix("v");directory=ROOT/"dist"/args.version
    for package in directory.glob("*.ipk"):
        with tarfile.open(package) as outer:
            assert outer.extractfile("./debian-binary").read()==b"2.0\n"
            with tarfile.open(fileobj=io.BytesIO(outer.extractfile("./control.tar.gz").read())) as control:
                assert b"Architecture:" in control.extractfile("./control").read()
                for member in control.getmembers():
                    if member.isfile() and member.name!="./control":
                        text=control.extractfile(member).read()
                        run("sh","-n",input=text.decode())
            with tarfile.open(fileobj=io.BytesIO(outer.extractfile("./data.tar.gz").read())) as data:
                assert not any(".." in Path(m.name).parts for m in data.getmembers())
                assert not any(m.name=="./etc/nintendo-control.json" for m in data.getmembers())
    print("PASS native IPK control, lifecycle and payload format",flush=True)
    with tempfile.TemporaryDirectory(prefix="nintendo-pkg-root-") as work:
        root=Path(work)/"root";root.mkdir()
        providers=Path(work)/"test-deps.apk"
        apk_file(apk,providers,"openwrt-test-deps","1-r1","noarch",{},[],
                 provides=[p+"=1" for p in BACKEND_DEPS+["luci-base","rpcd"]])
        backend=directory/f"nintendo-guard-{version}-r1.x86_64.apk"
        ui=directory/f"luci-app-nintendo-guard-{version}-r1.noarch.apk"
        common=[str(apk),"--root",str(root),"--arch","x86_64","--allow-untrusted","--no-network","--no-scripts"]
        run(*common,"add","--initdb",backend,ui,providers)
        assert (root/"usr/sbin/nintendo-guard").stat().st_mode&0o777==0o755
        assert (root/"usr/sbin/nintendo-guard").stat().st_uid==0
        assert (root/"www/luci-static/resources/view/network/nintendo.js").exists()
        assert not (root/"etc/nintendo-control.json").exists()
        # A small isolated chroot supplies OpenWrt command mocks; the actual Go
        # validate/cleanup code and package lifecycle scripts execute inside it.
        (root/"bin").mkdir();(root/"dev").mkdir();(root/"tmp").mkdir()
        (root/"dev/null").touch()
        shutil.copy2(shutil.which("busybox"),root/"bin/busybox")
        for name in ["sh","cp","rm","cat"]:(root/"bin"/name).symlink_to("busybox")
        def script(path,text):
            target=root/path;target.parent.mkdir(parents=True,exist_ok=True);target.write_text("#!/bin/sh\n"+text);target.chmod(0o755)
        script("usr/sbin/uci",'echo "$*" >> /tmp/uci.calls\n')
        script("usr/sbin/nft",'cat > /tmp/nft.calls\n')
        script("usr/sbin/conntrack",'exit 0\n')
        script("usr/sbin/ip",'exit 0\n')
        script("etc/init.d/nintendo-control",'echo "$*" >> /tmp/service.calls\n[ "$1" != running ]\n')
        script("etc/init.d/rpcd",'echo "$*" >> /tmp/rpcd.calls\n')
        def lifecycle(action):run("chroot",root,"/usr/libexec/nintendo-control-package",action,env=dict(os.environ,PATH="/usr/sbin:/usr/bin:/sbin:/bin"))
        lifecycle("configure")
        config=root/"etc/nintendo-control.json"
        assert json.loads(config.read_text())["mode"]=="off"
        config.write_text('{"mode":"updates","mac":"02:aa:bb:cc:dd:01","upstream":"127.0.0.1:53"}\n')
        saved=config.read_bytes();lifecycle("configure");assert config.read_bytes()==saved
        cache=root/"tmp/luci-indexcache.example.json";cache.write_text("{}")
        lifecycle("ui");assert not cache.exists()
        print("PASS real lifecycle: default-off, saved configuration, service and LuCI refresh",flush=True)
        replacement=Path(work)/"backend-r2.apk"
        apk_file(apk,replacement,"nintendo-guard",version+"-r2","x86_64",payload(ROOT/"build/amd64/nintendo-guard"),BACKEND_DEPS)
        run(*common,"add","--upgrade",replacement)
        assert config.read_bytes()==saved
        # The upgrade restores the real init file; use a chroot-only service stub
        # for pre-deinstall's stop/disable actions, which require procd on OpenWrt.
        script("etc/init.d/nintendo-control",'echo "$*" >> /tmp/service.calls\nexit 0\n')
        lifecycle("remove")
        assert "delete table inet nintendo_control" in (root/"tmp/nft.calls").read_text()
        assert config.read_bytes()==saved
        run(*common,"del","luci-app-nintendo-guard","nintendo-guard")
        assert not (root/"usr/sbin/nintendo-guard").exists()
        assert not (root/"www/luci-static/resources/view/network/nintendo.js").exists()
        assert config.read_bytes()==saved
        print("PASS real APK database install, upgrade and remove; user settings retained",flush=True)


if __name__=="__main__":main()
