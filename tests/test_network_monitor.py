from __future__ import annotations

import time

import pytest

from pdf_ocr_poc.network_monitor import monitor_process_tree_network


def test_monitor_process_tree_network_no_violations(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        "pdf_ocr_poc.network_monitor._capture_remote_connections", lambda _pid: []
    )

    with monitor_process_tree_network(poll_interval_seconds=0.01) as report:
        time.sleep(0.03)

    assert report["samples"] >= 1
    assert report["violations"] == []


def test_monitor_process_tree_network_collects_violations(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls = {"count": 0}

    def fake_capture(_pid: int):
        calls["count"] += 1
        if calls["count"] == 1:
            return [
                {
                    "pid": 10,
                    "process_name": "x",
                    "remote_ip": "8.8.8.8",
                    "remote_port": 53,
                }
            ]
        return []

    monkeypatch.setattr(
        "pdf_ocr_poc.network_monitor._capture_remote_connections", fake_capture
    )

    with monitor_process_tree_network(poll_interval_seconds=0.01) as report:
        time.sleep(0.03)

    assert len(report["violations"]) == 1
    assert report["violations"][0]["remote_ip"] == "8.8.8.8"


def test_monitor_process_tree_network_deduplicates(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    payload = [
        {"pid": 10, "process_name": "x", "remote_ip": "8.8.8.8", "remote_port": 443}
    ]
    monkeypatch.setattr(
        "pdf_ocr_poc.network_monitor._capture_remote_connections", lambda _pid: payload
    )

    with monitor_process_tree_network(poll_interval_seconds=0.01) as report:
        time.sleep(0.04)

    assert len(report["violations"]) == 1
