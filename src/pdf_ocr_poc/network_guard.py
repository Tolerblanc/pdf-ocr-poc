from __future__ import annotations

import ipaddress
import socket
import threading
from contextlib import contextmanager
from typing import Generator
from typing import Callable

_GUARD_LOCK = threading.Lock()
_GUARD_DEPTH = 0
_ORIGINAL_CONNECT: Callable[..., object] | None = None
_ORIGINAL_CREATE_CONNECTION: Callable[..., object] | None = None


def _is_loopback_host(host: str) -> bool:
    if host in {"localhost", "127.0.0.1", "::1"}:
        return True

    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        pass

    try:
        infos = socket.getaddrinfo(host, None)
    except socket.gaierror:
        return False

    for info in infos:
        resolved = info[4][0]
        try:
            if not ipaddress.ip_address(resolved).is_loopback:
                return False
        except ValueError:
            return False
    return True


@contextmanager
def local_only_network_guard(enabled: bool = True) -> Generator[None, None, None]:
    global _GUARD_DEPTH
    global _ORIGINAL_CONNECT
    global _ORIGINAL_CREATE_CONNECTION

    if not enabled:
        yield
        return

    def guarded_connect(sock: socket.socket, address):  # type: ignore[override]
        host = address[0] if isinstance(address, tuple) else str(address)
        if not _is_loopback_host(str(host)):
            raise RuntimeError(f"Outbound network blocked in local-only mode: {host}")
        if _ORIGINAL_CONNECT is None:  # pragma: no cover
            raise RuntimeError("Network guard original connect function is unavailable")
        return _ORIGINAL_CONNECT(sock, address)

    def guarded_create_connection(address, timeout=None, source_address=None):
        host = address[0] if isinstance(address, tuple) else str(address)
        if not _is_loopback_host(str(host)):
            raise RuntimeError(f"Outbound network blocked in local-only mode: {host}")
        if _ORIGINAL_CREATE_CONNECTION is None:  # pragma: no cover
            raise RuntimeError(
                "Network guard original create_connection function is unavailable"
            )
        return _ORIGINAL_CREATE_CONNECTION(
            address, timeout=timeout, source_address=source_address
        )

    with _GUARD_LOCK:
        if _GUARD_DEPTH == 0:
            _ORIGINAL_CONNECT = socket.socket.connect
            _ORIGINAL_CREATE_CONNECTION = socket.create_connection
            socket.socket.connect = guarded_connect  # type: ignore[assignment]
            socket.create_connection = guarded_create_connection  # type: ignore[assignment]
        _GUARD_DEPTH += 1

    try:
        yield
    finally:
        with _GUARD_LOCK:
            _GUARD_DEPTH = max(0, _GUARD_DEPTH - 1)
            if _GUARD_DEPTH == 0:
                if _ORIGINAL_CONNECT is not None:
                    socket.socket.connect = _ORIGINAL_CONNECT  # type: ignore[assignment]
                if _ORIGINAL_CREATE_CONNECTION is not None:
                    socket.create_connection = _ORIGINAL_CREATE_CONNECTION  # type: ignore[assignment]
                _ORIGINAL_CONNECT = None
                _ORIGINAL_CREATE_CONNECTION = None


def selfcheck_network_guard() -> tuple[bool, str]:
    with local_only_network_guard(enabled=True):
        try:
            socket.create_connection(("1.1.1.1", 80), timeout=1)
        except RuntimeError:
            return True, "network guard blocks outbound connections"
        except OSError:
            # Any non-guard failure still means connect attempt happened.
            return False, "outbound connect was attempted"
        return False, "outbound connect succeeded unexpectedly"
