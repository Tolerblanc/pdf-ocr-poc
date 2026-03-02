from __future__ import annotations

import socket
import threading

import pytest

from pdf_ocr_poc.network_guard import (
    _is_loopback_host,
    local_only_network_guard,
    selfcheck_network_guard,
)


def test_is_loopback_host_known_values() -> None:
    assert _is_loopback_host("localhost")
    assert _is_loopback_host("127.0.0.1")
    assert not _is_loopback_host("1.1.1.1")


def test_local_only_network_guard_blocks_outbound() -> None:
    with local_only_network_guard(enabled=True):
        with pytest.raises(RuntimeError):
            socket.create_connection(("1.1.1.1", 80), timeout=0.2)


def test_local_only_network_guard_disabled_allows_attempt() -> None:
    # The call may fail due to environment/network, but it should not fail by our RuntimeError.
    with local_only_network_guard(enabled=False):
        try:
            socket.create_connection(("1.1.1.1", 80), timeout=0.01)
        except RuntimeError as exc:  # pragma: no cover
            raise AssertionError("Guard must be disabled") from exc
        except OSError:
            pass


def test_selfcheck_network_guard() -> None:
    ok, message = selfcheck_network_guard()
    assert ok
    assert "blocks" in message


def test_local_only_network_guard_nested_contexts() -> None:
    with local_only_network_guard(enabled=True):
        with local_only_network_guard(enabled=True):
            with pytest.raises(RuntimeError):
                socket.create_connection(("1.1.1.1", 80), timeout=0.2)
        with pytest.raises(RuntimeError):
            socket.create_connection(("1.1.1.1", 80), timeout=0.2)


def test_local_only_network_guard_threaded_reentry() -> None:
    errors: list[str] = []
    lock = threading.Lock()

    def worker() -> None:
        try:
            with local_only_network_guard(enabled=True):
                try:
                    socket.create_connection(("1.1.1.1", 80), timeout=0.2)
                except RuntimeError:
                    return
                raise AssertionError("Expected outbound connection to be blocked")
        except Exception as exc:  # noqa: BLE001
            with lock:
                errors.append(str(exc))

    threads = [threading.Thread(target=worker) for _ in range(4)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert not errors
