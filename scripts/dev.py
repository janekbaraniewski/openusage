#!/usr/bin/env python3
import atexit
import os
import select
import signal
import subprocess
import sys
import time

BIN = "./tmp/openusage-dev"

def restore_terminal():
    # Disable xterm mouse tracking modes
    sys.stdout.write("\033[?1000l\033[?1002l\033[?1003l\033[?1006l")
    # Exit alternate screen buffer
    sys.stdout.write("\033[?1049l")
    # Show cursor
    sys.stdout.write("\033[?25h")
    sys.stdout.flush()
    # Reset terminal to cooked mode
    os.system("stty sane 2>/dev/null || true")

atexit.register(restore_terminal)

def signal_handler(sig, frame):
    restore_terminal()
    sys.exit(0)

signal.signal(signal.SIGINT, signal_handler)
signal.signal(signal.SIGTERM, signal_handler)

def build():
    print("\n🔨 Compiling OpenUsage...")
    res = subprocess.run(["go", "build", "-o", BIN, "./cmd/openusage"], capture_output=True, text=True)
    if res.returncode == 0:
        print("✅ Build succeeded!")
        return True
    else:
        print("❌ BUILD FAILED:")
        print("──────────────────────────────────────────────────")
        print(res.stderr.strip())
        print("──────────────────────────────────────────────────")
        return False

def start_watcher():
    return subprocess.Popen(
        [
            "inotifywait",
            "-m",
            "-r",
            "-q",
            "-e",
            "close_write,moved_to,modify,create,delete",
            "--include",
            r"\.go$",
            "cmd",
            "internal",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
    )

def wait_for_settled_edits(watcher):
    print("\n==================================================")
    print("  ⚡ EDITS HAPPENING...")
    print("==================================================")
    print("  Changes detected. Waiting for file edits to settle...")

    while True:
        r, _, _ = select.select([watcher.stdout], [], [], 1.2)
        if r:
            watcher.stdout.readline()
            print("  ↻ More edits detected, waiting for changes to settle...")
        else:
            print("  ✓ Edits settled.")
            break

def main():
    os.makedirs("./tmp", exist_ok=True)

    watcher = start_watcher()

    # Initial build if needed
    if not os.path.exists(BIN):
        while not build():
            print("Waiting for code edits to fix compiler errors...")
            watcher.stdout.readline()
            wait_for_settled_edits(watcher)

    while True:
        restore_terminal()
        app = subprocess.Popen([BIN] + sys.argv[1:])

        edit_occurred = False
        while True:
            # Check if app exited on its own (e.g. user pressed \"q\" or Ctrl+C)
            if app.poll() is not None:
                break

            # Check if watcher detected a file modification
            r, _, _ = select.select([watcher.stdout], [], [], 0.15)
            if r:
                watcher.stdout.readline()
                edit_occurred = True
                break

        if not edit_occurred:
            # User quit the app cleanly
            print("Exiting OpenUsage dev mode.")
            break

        # Stop app immediately and restore terminal so the banner is clear
        app.terminate()
        try:
            app.wait(timeout=0.6)
        except subprocess.TimeoutExpired:
            app.kill()
            app.wait()

        restore_terminal()
        wait_for_settled_edits(watcher)

        while not build():
            print("Waiting for code edits to fix compiler errors...")
            watcher.stdout.readline()
            wait_for_settled_edits(watcher)

        print("🚀 Launching OpenUsage...")
        time.sleep(0.3)

    watcher.terminate()
    try:
        watcher.wait(timeout=0.5)
    except Exception:
        watcher.kill()

if __name__ == "__main__":
    main()
