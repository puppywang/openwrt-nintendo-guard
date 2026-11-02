"""Run as root INSIDE `unshare --net`; no production router is involved."""
import http.server
import json
import os
from pathlib import Path
import signal
import socket
import ssl
import struct
import subprocess
import sys
import tempfile
import threading
import time

SELF = str(Path(__file__).resolve())
UPDATE = "sun.p01.lp1.d4c.srv.nintendo.net"
GAME = "atum.p01.lp1.d4c.srv.nintendo.net"
HOME = "home.example.org"
MAC = "02:aa:bb:cc:dd:01"


def command(*args, **kwargs):
    return subprocess.run(list(args), capture_output=True, text=True, timeout=10, check=True, **kwargs)


def client(kind, host, destination, port="53"):
    try:
        if kind in ("dns", "dns-tcp"):
            q = struct.pack("!HHHHHH", 123, 0x100, 1, 0, 0, 0)
            q += b"".join(bytes([len(part)]) + part.encode() for part in host.split(".")) + b"\0\0\1\0\1"
            if kind == "dns-tcp":
                with socket.create_connection((destination, int(port)), 2) as sock:

                    def receive(size):
                        result = b""
                        while len(result) < size:
                            part = sock.recv(size-len(result))
                            if not part: raise OSError("DNS TCP connection closed")
                            result += part
                        return result

                    sock.sendall(struct.pack("!H", len(q)) + q)
                    reply = receive(struct.unpack("!H", receive(2))[0])
            else:
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                    sock.settimeout(2); sock.sendto(q, (destination, int(port)))
                    reply = sock.recv(4096)
            def skip_name(offset):
                while reply[offset]:
                    length = reply[offset]
                    if length & 0xc0 == 0xc0: return offset+2
                    offset += length+1
                return offset+1
            offset=12
            for _ in range(struct.unpack("!H",reply[4:6])[0]): offset=skip_name(offset)+4
            addresses=[]
            for _ in range(struct.unpack("!H",reply[6:8])[0]):
                offset=skip_name(offset)
                rrtype,_,_,size=struct.unpack("!HHIH",reply[offset:offset+10]);offset+=10
                if rrtype==1 and size==4: addresses.append(socket.inet_ntoa(reply[offset:offset+size]))
                offset+=size
            return {"rcode": reply[3] & 15,"addresses":addresses}
        if kind == "udp":
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                sock.settimeout(2); sock.sendto(b"ping", (destination, 9999))
                return {"ok": sock.recv(64) == b"ping"}
        with socket.create_connection((destination, 443 if kind == "tls" else 80), 2) as raw:
            sock = ssl._create_unverified_context().wrap_socket(raw, server_hostname=host) if kind == "tls" else raw
            with sock:
                sock.sendall(("GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n").encode())
                reply=b""
                while len(reply)<16384:
                    chunk=sock.recv(2048)
                    if not chunk: break
                    reply+=chunk
                return {"ok": b"200" in reply,"local_test":b"X-Organization: Nintendo" in reply,"body":reply.split(b"\r\n\r\n",1)[-1].decode("utf-8","replace")}
    except (OSError, ssl.SSLError) as error:
        return {"ok": False, "error": type(error).__name__}


def fixture(cert, key):
    # Child fixture cannot outlive a lost parent indefinitely.
    threading.Timer(75, lambda: os._exit(0)).start()
    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200); self.end_headers(); self.wfile.write(b"ok")
        def log_message(self, *_):
            pass
    for port in (80, 443):
        server = http.server.HTTPServer(("0.0.0.0", port), Handler)
        if port == 443:
            context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER); context.load_cert_chain(cert, key)
            server.socket = context.wrap_socket(server.socket, server_side=True)
        threading.Thread(target=server.serve_forever, daemon=True).start()
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as udp:
        udp.bind(("0.0.0.0", 9999))
        print("ready", flush=True)
        while True:
            data, address = udp.recvfrom(4096); udp.sendto(data, address)


def main(binary):
    if os.geteuid() != 0:
        raise SystemExit("Run inside sudo unshare --net")
    # Refuse the host namespace even if invoked incorrectly as root.
    if os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"):
        raise SystemExit("Refusing to run in the host network namespace")
    processes, namespaces, dns_sockets = [], [], []
    def stop(proc):
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
            try: proc.wait(timeout=4)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL); proc.wait(timeout=2)
    def spawn(args, **kwargs):
        proc = subprocess.Popen(args, start_new_session=True, **kwargs)
        processes.append(proc); return proc
    def terminate(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, terminate)
    with tempfile.TemporaryDirectory(prefix="nintendo-guard-test-") as work:
        try:
            env = dict(os.environ, NINTENDO_GUARD_CONFIG=work+"/config.json", NINTENDO_GUARD_SOCKET=work+"/control.sock")
            command("ip", "link", "set", "lo", "up")
            command("sysctl", "-qw", "net.ipv4.ip_forward=1")
            def network(label, subnet, mac):
                ns = "ng-" + str(os.getpid()) + "-" + label
                command("ip", "netns", "add", ns); namespaces.append(ns)
                host, peer = "ng-"+label, "ng-peer-"+label
                command("ip", "link", "add", host, "type", "veth", "peer", "name", peer)
                command("ip", "link", "set", peer, "netns", ns)
                command("ip", "addr", "add", subnet+".1/24", "dev", host)
                command("ip", "link", "set", host, "up")
                prefix = ["ip", "netns", "exec", ns, "ip"]
                command(*prefix, "link", "set", "lo", "up")
                command(*prefix, "link", "set", peer, "address", mac)
                command(*prefix, "addr", "add", subnet+".2/24", "dev", peer)
                command(*prefix, "link", "set", peer, "up")
                command(*prefix, "route", "add", "default", "via", subnet+".1")
                return ns
            target = network("t", "192.0.2", MAC)
            other = network("c", "203.0.113", "02:aa:bb:cc:dd:02")
            free = network("f", "198.18.0", "02:aa:bb:cc:dd:04")
            restricted = network("n", "192.0.3", "02:aa:bb:cc:dd:05")
            server = network("s", "198.51.100", "02:aa:bb:cc:dd:03")
            cert, key = work+"/cert.pem", work+"/key.pem"
            command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", key, "-out", cert, "-days", "1", "-subj", "/CN=test.invalid")
            helper = spawn(["ip", "netns", "exec", server, sys.executable, SELF, "--server", cert, key], stdout=subprocess.PIPE, text=True)
            assert helper.stdout.readline().strip() == "ready"
            def dns_reply(packet):
                reply = packet[:2] + b"\x81\x80\0\1\0\1\0\0\0\0" + packet[12:]
                return reply + b"\xc0\x0c\0\1\0\1\0\0\0\x3c\0\4" + socket.inet_aton("198.51.100.2")
            def dns_server(sock):
                while True:
                    try:
                        packet, address = sock.recvfrom(4096)
                        sock.sendto(dns_reply(packet), address)
                    except OSError: return
            def dns_tcp_server(sock):
                while True:
                    try: conn, _ = sock.accept()
                    except OSError: return
                    with conn:
                        conn.settimeout(2)
                        try:
                            def receive(size):
                                result = b""
                                while len(result) < size:
                                    part = conn.recv(size-len(result))
                                    if not part: raise OSError("DNS TCP connection closed")
                                    result += part
                                return result
                            reply = dns_reply(receive(struct.unpack("!H",receive(2))[0]))
                            conn.sendall(struct.pack("!H",len(reply))+reply)
                        except OSError: pass
            for port in (53, 15353):
                sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); sock.bind(("0.0.0.0", port)); dns_sockets.append(sock)
                threading.Thread(target=dns_server, args=(sock,), daemon=True).start()
            tcp_dns = socket.socket(socket.AF_INET,socket.SOCK_STREAM)
            tcp_dns.bind(("0.0.0.0",53)); tcp_dns.listen(8); dns_sockets.append(tcp_dns)
            threading.Thread(target=dns_tcp_server,args=(tcp_dns,),daemon=True).start()
            config = {"mode":"off", "mac":"", "upstream":"127.0.0.1:15353"}
            Path(env["NINTENDO_GUARD_CONFIG"]).write_text(json.dumps(config))
            binary = str(Path(binary).resolve())
            def start():
                proc = spawn(["timeout", "75", binary, "serve"], env=env, stderr=subprocess.PIPE, text=True)
                for _ in range(40):
                    result = subprocess.run([binary,"status"], env=env, capture_output=True, text=True, timeout=3)
                    if result.returncode == 0: return proc
                    if proc.poll() is not None: raise RuntimeError(proc.stderr.read())
                    time.sleep(.1)
                raise RuntimeError("service readiness timeout")
            guard = start()
            command(binary,"set",input=json.dumps(config),env=env)
            assert json.loads(Path(env["NINTENDO_GUARD_CONFIG"]).read_text())["devices"] == []
            def set_mode(mode):
                config.update(mode=mode, mac=MAC)
                status = json.loads(command(binary,"set",input=json.dumps(config),env=env).stdout)
                assert status["firewall_ok"], status
            def query(ns, kind, name=GAME, dest="198.51.100.2", port="53"):
                return json.loads(command("ip","netns","exec",ns,sys.executable,SELF,"--client",kind,name,dest,port).stdout)
            set_mode("updates")
            assert query(target,"dns",UPDATE,"192.0.2.1")["rcode"] == 3
            assert query(target,"dns",GAME,"192.0.2.1")["rcode"] == 0
            assert query(target,"tls")["ok"]
            assert not query(target,"tls",UPDATE)["ok"]  # cached IP, no DNS
            assert not query(target,"http",UPDATE)["ok"]
            assert query(other,"tls",UPDATE)["ok"]
            assert query(target,"udp")["ok"]
            print("PASS updates: DNS, cached-IP TLS/HTTP, other client, UDP", flush=True)
            set_mode("all")
            assert query(target,"dns",GAME,"192.0.2.1")["rcode"] == 3
            assert not query(target,"tls")["ok"]
            assert not query(target,"udp")["ok"]
            assert query(other,"tls")["ok"]
            assert query(other,"udp")["ok"]
            print("PASS full isolation: selected client only", flush=True)
            set_mode("off")
            assert query(target,"dns",UPDATE,"192.0.2.1")["rcode"] == 0
            assert query(target,"tls",UPDATE)["ok"]
            print("PASS off: normal route restored", flush=True)
            # Concurrent, independent policies. The off entry and an unlisted
            # client both retain their normal network path.
            config = {"upstream":"127.0.0.1:15353", "devices":[
                {"name":"Switch 2", "mac":MAC, "mode":"updates"},
                {"name":"Switch", "mac":"02:aa:bb:cc:dd:02", "mode":"all"},
                {"name":"Disabled", "mac":"02:aa:bb:cc:dd:04", "mode":"off"},
                {"name":"Nintendo only", "mac":"02:aa:bb:cc:dd:05", "mode":"nintendo"},
            ]}
            def save():
                status = json.loads(command(binary,"set",input=json.dumps(config),env=env).stdout)
                assert status["firewall_ok"], status
                return status
            save()
            for kind in ("dns", "dns-tcp"):
                assert query(target,kind,UPDATE,"192.0.2.1")["rcode"] == 3
                assert query(target,kind,GAME,"192.0.2.1")["rcode"] == 0
                assert query(other,kind,GAME,"203.0.113.1")["rcode"] == 3
                assert query(free,kind,UPDATE,"198.18.0.1")["rcode"] == 0
                assert "rcode" not in query(other,kind,GAME,"203.0.113.1","5354")
                assert "rcode" not in query(target,kind,GAME,"192.0.2.1","5355")
                assert "rcode" not in query(free,kind,GAME,"198.18.0.1","5354")
                blocked=query(restricted,kind,GAME,"192.0.3.1")
                assert blocked["rcode"]==0 and blocked["addresses"]==["0.0.0.0"],blocked
                assert query(restricted,kind,HOME,"192.0.3.1")["addresses"]==["198.51.100.2"]
                assert "rcode" not in query(restricted,kind,HOME,"192.0.3.1","5354")
                assert "rcode" not in query(target,kind,HOME,"192.0.2.1","5356")
                test=query(restricted,kind,"ctest.cdn.nintendo.net","192.0.3.1")
                assert test["addresses"]==["192.0.3.1"],test
            assert query(target,"tls")["ok"]
            assert not query(target,"tls",UPDATE)["ok"]
            assert not query(other,"tls")["ok"]
            assert not query(other,"udp")["ok"]
            assert query(free,"tls",UPDATE)["ok"]
            assert query(free,"udp")["ok"]
            for kind in ("tls","http"):
                assert query(restricted,kind,HOME)["ok"]
                assert not query(restricted,kind,GAME)["ok"] # cached IP, no DNS
                assert not query(restricted,kind,"nega-lp1-upload.s3.us-east-1.amazonaws.com")["ok"]
            assert query(restricted,"udp")["ok"]
            for host in ("conntest.nintendowifi.net","ctest.cdn.nintendo.net"):
                test=query(restricted,"http",host,"192.0.3.1")
                assert test["ok"] and test["local_test"],test
            print("PASS four modes: selective Nintendo blocking, unrelated DNS/TLS/HTTP/UDP, local connection test, listener isolation", flush=True)
            # Invalid saves must leave every device's existing policy in place.
            invalid = dict(config,devices=config["devices"]+[config["devices"][0]])
            result = subprocess.run([binary,"set"],input=json.dumps(invalid),env=env,capture_output=True,text=True,timeout=10)
            assert result.returncode != 0
            assert query(other,"dns",GAME,"203.0.113.1")["rcode"] == 3
            config["devices"].pop(1); save()
            assert query(other,"tls",UPDATE)["ok"]
            assert query(other,"udp")["ok"]
            assert query(target,"dns",UPDATE,"192.0.2.1")["rcode"] == 3
            # Same MAC, different IP: policy follows Ethernet identity.
            command("ip","netns","exec",target,"ip","addr","del","192.0.2.2/24","dev","ng-peer-t")
            command("ip","netns","exec",target,"ip","addr","add","192.0.2.9/24","dev","ng-peer-t")
            command("ip","netns","exec",target,"ip","route","replace","default","via","192.0.2.1")
            assert query(target,"dns",UPDATE,"192.0.2.1")["rcode"] == 3
            assert query(target,"tls")["ok"]
            print("PASS invalid save, device removal and changed client IP", flush=True)
            stop(guard); guard = start()
            assert query(target,"dns",UPDATE,"192.0.2.1")["rcode"] == 3
            assert query(restricted,"dns",GAME,"192.0.3.1")["addresses"]==["0.0.0.0"]
            assert query(restricted,"tls",HOME)["ok"]
            command("nft","delete","table","inet","nintendo_control")
            restored = False
            for _ in range(65):
                status = json.loads(command(binary,"status",env=env).stdout)
                if status["firewall_ok"]: restored=True; break
                time.sleep(.2)
            assert restored
            print("PASS restart persistence and table recovery", flush=True)
            config["devices"] = []; save()
            assert query(target,"tls",UPDATE)["ok"]
            assert query(other,"tls",UPDATE)["ok"]
            assert query(restricted,"tls",GAME)["ok"]
            print("PASS empty list restores normal networking", flush=True)
        finally:
            for proc in reversed(processes): stop(proc)
            for sock in dns_sockets: sock.close()
            for ns in reversed(namespaces):
                subprocess.run(["ip","netns","delete",ns], capture_output=True, timeout=5)


if __name__ == "__main__":
    if sys.argv[1] == "--client": print(json.dumps(client(*sys.argv[2:])))
    elif sys.argv[1] == "--server": fixture(*sys.argv[2:])
    else: main(sys.argv[1])
