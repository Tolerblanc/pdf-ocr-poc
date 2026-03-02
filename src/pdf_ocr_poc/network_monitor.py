from __future__ import annotations

import ipaddress
import os
import threading
import time
from contextlib import contextmanager
from typing import Any, Generator

import psutil


def _is_loopback_ip(ip: str) -> bool:
    try:
        return ipaddress.ip_address(ip).is_loopback
    except ValueError:
        return False


def _capture_remote_connections(root_pid: int) -> list[dict[str, Any]]:
    violations: list[dict[str, Any]] = []
    pids: set[int] = {root_pid}

    try:
        root = psutil.Process(root_pid)
        for child in root.children(recursive=True):
            pids.add(child.pid)
    except psutil.Error:
        pass

    for pid in pids:
        try:
            process = psutil.Process(pid)
            for conn in process.net_connections(kind="inet"):
                if not conn.raddr:
                    continue

                remote_ip = getattr(conn.raddr, "ip", None)
                remote_port = getattr(conn.raddr, "port", None)
                if remote_ip is None and isinstance(conn.raddr, tuple) and conn.raddr:
                    remote_ip = conn.raddr[0]
                    remote_port = conn.raddr[1] if len(conn.raddr) > 1 else None

                if not remote_ip or _is_loopback_ip(str(remote_ip)):
                    continue

                violations.append(
                    {
                        "pid": pid,
                        "process_name": process.name(),
                        "remote_ip": str(remote_ip),
                        "remote_port": int(remote_port) if remote_port else None,
                    }
                )
        except psutil.Error:
            continue

    return violations


@contextmanager
def monitor_process_tree_network(
    poll_interval_seconds: float = 0.2,
) -> Generator[dict[str, Any], None, None]:
    report: dict[str, Any] = {
        "samples": 0,
        "violations": [],
        "duration_seconds": 0.0,
    }

    root_pid = os.getpid()
    stop_event = threading.Event()
    seen: set[tuple[int, str, int | None]] = set()
    start = time.perf_counter()

    def _poll() -> None:
        while not stop_event.is_set():
            sample = _capture_remote_connections(root_pid)
            report["samples"] += 1
            for item in sample:
                key = (item["pid"], item["remote_ip"], item["remote_port"])
                if key in seen:
                    continue
                seen.add(key)
                report["violations"].append(item)
            stop_event.wait(poll_interval_seconds)

    thread = threading.Thread(
        target=_poll, name="local-only-network-monitor", daemon=True
    )
    thread.start()
    try:
        yield report
    finally:
        stop_event.set()
        thread.join(timeout=1.5)
        report["duration_seconds"] = time.perf_counter() - start
