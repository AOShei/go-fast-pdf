#!/usr/bin/env python3
"""Prints an HTML file to PDF with Firefox, as "Save as PDF" would.

    browser-print.py <in.html> <out.pdf> <work directory>

Firefox has no command-line option for this, so it is started without a
window and told what to do over its remote-control protocol (Marionette).
It runs with a new, empty profile in the work directory, and is told to
quit when the page is printed.

testdata/gen.sh runs this script under "contained": a time limit, a
file-size limit, and the whole process group killed at the end. Firefox
works through several processes of its own.
"""
import base64
import json
import os
import socket
import subprocess
import sys
import time

PREFS = {
    "marionette.port": 0,  # set below
    "browser.shell.checkDefaultBrowser": False,
    "browser.startup.homepage_override.mstone": "ignore",
    "browser.aboutwelcome.enabled": False,
    "app.update.enabled": False,
    "app.normandy.enabled": False,
    "datareporting.policy.dataSubmissionEnabled": False,
    "datareporting.healthreport.uploadEnabled": False,
    "toolkit.telemetry.enabled": False,
    "toolkit.telemetry.unified": False,
    "network.captive-portal-service.enabled": False,
    "network.connectivity-service.enabled": False,
    "browser.safebrowsing.malware.enabled": False,
    "browser.safebrowsing.phishing.enabled": False,
    "extensions.update.enabled": False,
    "services.settings.server": "http://127.0.0.1:9/",
}


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Marionette:
    def __init__(self, port, deadline):
        while True:
            try:
                self.sock = socket.create_connection(("127.0.0.1", port), timeout=5)
                break
            except OSError:
                if time.time() > deadline:
                    raise SystemExit("Firefox did not open its control port")
                time.sleep(0.2)
        self.sock.settimeout(60)
        self.buf = b""
        self.next_id = 0
        self.read()  # the greeting

    def read(self):
        while b":" not in self.buf:
            self.buf += self.receive()
        length, _, rest = self.buf.partition(b":")
        length = int(length)
        while len(rest) < length:
            rest += self.receive()
        self.buf = rest[length:]
        return json.loads(rest[:length])

    def receive(self):
        data = self.sock.recv(1 << 16)
        if not data:
            raise SystemExit("Firefox closed the connection")
        return data

    def command(self, name, params=None):
        self.next_id += 1
        payload = json.dumps([0, self.next_id, name, params or {}]).encode()
        self.sock.sendall(str(len(payload)).encode() + b":" + payload)
        _, _, error, result = self.read()
        if error:
            raise SystemExit(f"{name}: {error}")
        return result


def main():
    src, out, work = sys.argv[1:4]
    profile = os.path.join(work, "firefox-profile")
    os.makedirs(profile, exist_ok=True)
    port = free_port()
    PREFS["marionette.port"] = port
    with open(os.path.join(profile, "user.js"), "w") as f:
        for key, value in PREFS.items():
            f.write(f"user_pref({json.dumps(key)}, {json.dumps(value)});\n")

    firefox = subprocess.Popen(
        ["firefox", "--headless", "--no-remote", "--marionette", "--profile", profile],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        m = Marionette(port, time.time() + 60)
        m.command("WebDriver:NewSession", {"capabilities": {"alwaysMatch": {"acceptInsecureCerts": False}}})
        m.command("WebDriver:Navigate", {"url": "file://" + os.path.abspath(src)})
        result = m.command("WebDriver:Print", {
            "page": {"width": 21.0, "height": 29.7},
            "margin": {"top": 2, "bottom": 2, "left": 2, "right": 2},
            "printBackground": False, "shrinkToFit": True, "scale": 1.0,
        })
        with open(out, "wb") as f:
            f.write(base64.b64decode(result["value"]))
        try:
            m.command("Marionette:Quit", {"flags": ["eForceQuit"]})
        except (SystemExit, OSError):
            pass
        firefox.wait(timeout=20)
    finally:
        if firefox.poll() is None:
            firefox.kill()


if __name__ == "__main__":
    main()
