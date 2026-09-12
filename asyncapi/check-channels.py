#!/usr/bin/env python3
"""Validate AsyncAPI declarations using the nats-console checker service."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any

try:
    import yaml
except ImportError:
    print("ERROR: Python package 'PyYAML' is required.", file=sys.stderr)
    raise SystemExit(2)

STATUS_SUBJECT = "checker.status.account"
SCRIPT_DIR = Path(__file__).resolve().parent


@dataclass(frozen=True, order=True)
class Channel:
    account: str
    stream: str
    subject: str
    publish_required: bool = False
    consumer: str = ""
    source_account: str = ""
    source_stream: str = ""
    source_filter: str = ""


@dataclass
class Result:
    group: str
    description: str
    target: str
    status: str
    action: str = "-"


def discover_channels(selected: str) -> list[Channel]:
    channels: set[Channel] = set()
    accounts: set[str] = set()
    for path in sorted(SCRIPT_DIR.glob("*.yaml")):
        try:
            document = yaml.safe_load(path.read_text()) or {}
        except (OSError, yaml.YAMLError) as error:
            raise RuntimeError(f"{path.name}: cannot read AsyncAPI document: {error}") from error
        server_accounts = {
            server.get("x-nats-account")
            for server in (document.get("servers") or {}).values()
            if isinstance(server, dict) and server.get("x-nats-account")
        }
        accounts.update(server_accounts)
        if len(server_accounts) != 1:
            raise RuntimeError(f"{path.name}: expected exactly one x-nats-account")
        account = next(iter(server_accounts))
        sends: set[str] = set()
        consumers: dict[str, str] = {}
        for name, operation in (document.get("operations") or {}).items():
            if not isinstance(operation, dict):
                raise RuntimeError(f"{path.name}: operation {name!r} must be an object")
            action = operation.get("action")
            if action not in ("send", "receive"):
                continue
            ref_value = operation.get("channel")
            ref = ref_value.get("$ref") if isinstance(ref_value, dict) else None
            prefix = "#/channels/"
            if not isinstance(ref, str) or not ref.startswith(prefix) or not ref[len(prefix):]:
                raise RuntimeError(f"{path.name}: operation {name!r} must reference a local channel")
            channel_name = ref[len(prefix):]
            if action == "send":
                sends.add(channel_name)
                continue
            extension = operation.get("x-jetstream-consumer")
            consumer = extension.get("name") if isinstance(extension, dict) else None
            if not isinstance(consumer, str) or not consumer:
                raise RuntimeError(f"{path.name}: receive operation {name!r} has no x-jetstream-consumer.name")
            if channel_name in consumers and consumers[channel_name] != consumer:
                raise RuntimeError(f"{path.name}: channel {channel_name!r} declares conflicting Consumer names")
            consumers[channel_name] = consumer
        for name, value in (document.get("channels") or {}).items():
            if not isinstance(value, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} must be an object")
            jetstream = value.get("x-jetstream")
            if jetstream is None:
                continue
            if not isinstance(jetstream, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} has invalid x-jetstream")
            subject, stream = value.get("address"), jetstream.get("stream")
            if not isinstance(subject, str) or not subject or not isinstance(stream, str) or not stream:
                raise RuntimeError(f"{path.name}: channel {name!r} requires address and x-jetstream.stream")
            source = value.get("x-source") or {}
            if not isinstance(source, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} has invalid x-source")
            source_account = source.get("account", "")
            source_stream = source.get("stream", "")
            source_filter = source.get("filterSubject", "")
            if not all(isinstance(item, str) for item in (source_account, source_stream, source_filter)):
                raise RuntimeError(f"{path.name}: channel {name!r} has non-string x-source fields")
            if bool(source_account) != bool(source_stream):
                raise RuntimeError(f"{path.name}: channel {name!r} must declare both x-source.account and x-source.stream")
            if source_account and not source_filter:
                raise RuntimeError(f"{path.name}: channel {name!r} must declare x-source.filterSubject")
            if not source_account and source_filter:
                raise RuntimeError(f"{path.name}: channel {name!r} declares filterSubject without an x-source")
            if not selected or selected in (account, source_account):
                channels.add(Channel(account, stream, subject, name in sends, consumers.get(name, ""), source_account, source_stream, source_filter))
    if selected and selected not in accounts:
        raise RuntimeError(f"unknown NATS_ACCOUNT={selected!r}; declared accounts: {', '.join(sorted(accounts)) or 'none'}")
    if not channels:
        raise RuntimeError(f"no JetStream channels{' for NATS_ACCOUNT=' + selected if selected else ''}")
    return sorted(channels)


def request_checks(server: str, creds: str, timeout: str, channels: list[Channel]) -> list[dict[str, Any]]:
    payload = {"channels": [{
        "account": item.account, "stream": item.stream, "subject": item.subject,
        "publishRequired": item.publish_required, "consumer": item.consumer,
        "sourceAccount": item.source_account, "sourceStream": item.source_stream,
        "sourceFilter": item.source_filter,
    } for item in channels]}
    command = ["nats", "--server", server, "--creds", creds, "request", "--raw", "--timeout", timeout, STATUS_SUBJECT, json.dumps(payload, separators=(",", ":"))]
    completed = subprocess.run(command, text=True, capture_output=True, check=False)
    if completed.returncode:
        detail = (completed.stderr or completed.stdout).strip().splitlines()
        raise RuntimeError(detail[0] if detail else "checker request failed")
    try:
        response = json.loads(completed.stdout.strip())
    except json.JSONDecodeError as error:
        raise RuntimeError(f"invalid checker JSON response: {error}") from error
    if not isinstance(response, dict):
        raise RuntimeError("checker response is not an object")
    if response.get("error"):
        if response["error"] == "account status unavailable":
            raise RuntimeError(
                "the connected nats-console API does not support channel checks; "
                "deploy or restart it with this branch, then run Update checker access"
            )
        raise RuntimeError(str(response["error"]))
    statuses = response.get("channels")
    if not isinstance(statuses, list) or len(statuses) != len(channels) or not all(isinstance(item, dict) for item in statuses):
        raise RuntimeError("checker response does not match the requested channels")
    return statuses


def render_results(channels: list[Channel], statuses: list[dict[str, Any]]) -> bool:
    results: list[Result] = []
    seen: set[tuple[str, str, str]] = set()

    def add(group: str, description: str, target: str, passed: bool | None, action: str = "-") -> None:
        key = (group, description, target)
        if key in seen:
            return
        seen.add(key)
        status = "건너뜀" if passed is None else ("통과" if passed else "실패")
        results.append(Result(group, description, target, status, action if not passed else "-"))

    for channel, value in zip(channels, statuses):
        account = value.get("account") or {}
        add(channel.account, "서비스 account 확인", channel.account, bool(account.get("exists")), f"NATS Console에서 {channel.account} account를 생성하세요.")
        if not account.get("exists"):
            add(channel.account, "JetStream 활성화", channel.account, None, "서비스 account가 없어 확인하지 않습니다.")
            continue
        add(channel.account, "JetStream 활성화", channel.account, bool(account.get("jetstreamEnabled")), f"{channel.account} account에서 JetStream을 활성화하세요.")
        if channel.publish_required:
            add(channel.account, "메시지 Publish 권한", f"{channel.account} / {channel.subject}", account.get("publishAllowed") is True, f"{channel.account} account Publish permissions에 {channel.subject}를 추가하고 Deny 규칙을 확인하세요.")
        add(channel.account, "Stream 존재", f"{channel.account} / {channel.stream}", bool(value.get("targetStream")), value.get("targetStreamError") or "Stream을 생성하세요.")
        if not value.get("targetStream"):
            continue
        if channel.source_account:
            source = value.get("sourceAccount") or {}
            sharing = source.get("sourceSharing") or {}
            add(channel.account, "원본 account 확인", channel.source_account, bool(source.get("exists")), f"NATS Console에서 {channel.source_account} account를 생성하세요.")
            add(channel.account, "원본 JetStream 활성화", channel.source_account, bool(source.get("jetstreamEnabled")), f"{channel.source_account} account에서 JetStream을 활성화하세요.")
            add(channel.account, "원본 Stream 존재", f"{channel.source_account} / {channel.source_stream}", value.get("sourceStream") is True, value.get("sourceStreamError") or "원본 Stream을 생성하세요.")
            add(channel.account, "Stream Source 제공 허용", channel.source_account, bool(sharing.get("sourceSharingEnabled")), "NATS Console에서 Stream Source 공유를 활성화하세요.")
            imports = all(sharing.get(name) is True for name in ("consumerAPIImportEnabled", "deliverySubjectImportEnabled", "flowControlImportEnabled"))
            add(channel.account, "Stream Source account 연결 설정", f"{channel.source_account} -> {channel.account}", bool(sharing.get("sourceExportsEnabled")) and imports, "NATS Console에서 source account 연결을 갱신하세요.")
            target = f"{channel.source_account} / {channel.source_stream} <- {channel.account} / {channel.stream}"
            add(channel.account, "Stream Source 연결", target, value.get("sourceConnected") is True, "대상 Stream에 원본 Stream을 연결하세요.")
            add(channel.account, "Stream Source filter", f"{target} [filter: {channel.source_filter}]", value.get("sourceFilter") is True, f"Stream Source filter를 {channel.source_filter}로 설정하세요.")
        else:
            add(channel.account, "이벤트를 저장할 Stream 준비", f"{channel.account} / {channel.stream} / {channel.subject}", value.get("subjectCovered") is True, f"Stream subject에 {channel.subject}를 추가하세요.")
        if channel.consumer:
            add(channel.account, "Consumer 준비", f"{channel.account} / {channel.stream} / {channel.consumer}", value.get("consumerReady") is True, value.get("consumerError") or "Consumer를 생성하세요.")

    colors = {"통과": "\033[32m", "실패": "\033[31m", "건너뜀": "\033[33m"} if sys.stdout.isatty() else {}
    reset, current = ("\033[0m" if colors else ""), ""
    for result in results:
        if result.group != current:
            if current:
                print()
            print(f"[{result.group}]")
            current = result.group
        print(f"  [{colors.get(result.status, '')}{result.status}{reset}] {result.description}")
        print(f"       대상: {result.target}")
        if result.action != "-":
            print(f"       조치: {result.action.replace(chr(9), ' ').replace(chr(10), ' ')}")
    return any(result.status == "실패" for result in results)


def usage() -> None:
    print("""Usage:
  NATS_URL=nats://<host>:<port> NATS_CREDS=/path/to/asyncapi-checker.creds ./asyncapi/check-channels.py

Checks every declared account by default. Set NATS_ACCOUNT to filter checks.
All NATS inspection and matching is performed by nats-console.""")


def main() -> int:
    if any(argument in ("-h", "--help") for argument in sys.argv[1:]):
        usage()
        return 0
    server = os.getenv("NATS_URL", "")
    creds = os.getenv("NATS_CREDS") or os.getenv("NATS_CREDENTIALS_PATH", "")
    if not server or not creds:
        print("ERROR: NATS_URL and NATS_CREDS are required.", file=sys.stderr)
        return 2
    if not Path(creds).is_file() or not os.access(creds, os.R_OK):
        print(f"ERROR: NATS credentials file is not readable: {creds}", file=sys.stderr)
        return 2
    if shutil.which("nats") is None:
        print("ERROR: the NATS CLI ('nats') is required.", file=sys.stderr)
        return 2
    try:
        channels = discover_channels(os.getenv("NATS_ACCOUNT", ""))
        statuses = request_checks(server, creds, os.getenv("NATS_TIMEOUT", "30s"), channels)
    except RuntimeError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2
    if render_results(channels, statuses):
        print("하나 이상의 AsyncAPI Stream 검사가 실패했습니다.", file=sys.stderr)
        return 1
    print("All AsyncAPI channels are ready.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
