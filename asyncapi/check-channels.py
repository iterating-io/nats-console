#!/usr/bin/env python3
"""Validate AsyncAPI channel declarations against read-only NATS checker APIs."""

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
    source_account: str = ""
    source_stream: str = ""
    source_filter: str = ""
    publish_required: bool = False
    consumer: str = ""


@dataclass
class Result:
    group: str
    description: str
    target: str
    status: str
    action: str = "-"


class Checker:
    def __init__(self, server: str, creds: str, timeout: str, channels: list[Channel]) -> None:
        self.server = server
        self.creds = creds
        self.timeout = timeout
        self.channels = channels
        self.results: list[Result] = []
        self.failed = False
        self.account_cache: dict[str, bool] = {}
        self.stream_cache: dict[tuple[str, str], dict[str, Any] | None] = {}
        self.source_cache: dict[tuple[str, str], bool] = {}
        self.subject_checked: set[tuple[str, str, str]] = set()
        self.publish_checked: set[tuple[str, str]] = set()
        self.source_connection_checked: set[tuple[str, str, str, str]] = set()
        self.source_filter_checked: set[tuple[str, str, str, str, str]] = set()
        self.skipped_source_setup: set[tuple[str, str]] = set()
        self.consumer_accounts = {channel.account for channel in channels if channel.consumer}
        self.consumer_access: dict[str, bool] = {}
        self.consumer_checked: set[tuple[str, str, str]] = set()

    def add(self, group: str, description: str, target: str, status: str, action: str = "-") -> None:
        self.results.append(Result(group, description, target, status, action.replace("\t", " ").replace("\n", " ")))
        if status == "실패":
            self.failed = True

    def skip(self, group: str, description: str, target: str, reason: str) -> None:
        self.add(group, description, target, "건너뜀", reason)

    def request_json(self, subject: str, payload: str = "") -> dict[str, Any]:
        command = [
            "nats", "--server", self.server, "--creds", self.creds,
            "request", "--raw", "--timeout", self.timeout, subject, payload,
        ]
        completed = subprocess.run(command, text=True, capture_output=True, check=False)
        raw = completed.stdout.strip()
        if completed.returncode:
            detail = (completed.stderr or completed.stdout).strip().splitlines()
            raise RuntimeError(detail[0] if detail else "request failed")
        if not raw:
            raise RuntimeError("checker 응답이 없습니다")
        try:
            value = json.loads(raw)
        except json.JSONDecodeError as error:
            raise RuntimeError(f"invalid JSON response: {error}") from error
        if not isinstance(value, dict):
            raise RuntimeError("response is not a JSON object")
        error = value.get("error")
        if error:
            if isinstance(error, dict):
                raise RuntimeError(str(error.get("description") or error.get("message") or error))
            raise RuntimeError(str(error))
        return value

    @staticmethod
    def require_bools(value: dict[str, Any], fields: tuple[str, ...], context: str) -> None:
        if any(not isinstance(value.get(field), bool) for field in fields):
            raise RuntimeError(f"{context} response is missing required boolean fields")

    def ensure_account(self, account: str) -> bool:
        if account in self.account_cache:
            return self.account_cache[account]
        try:
            status = self.request_json(STATUS_SUBJECT, json.dumps({"account": account}, separators=(",", ":")))
            fields = ["exists", "jetstreamEnabled", "streamInfoImportEnabled"]
            if account in self.consumer_accounts:
                fields.append("consumerInfoImportEnabled")
            self.require_bools(status, tuple(fields), "status")
        except RuntimeError as error:
            self.add(account, "checker 상태 API 연결", account, "실패",
                     f"NATS Console의 AsyncAPI에서 Update checker access를 실행하고 새 checker credentials로 교체하세요. 상세: {error}")
            self.skip(account, "서비스 account 확인", account, "account 상태 조회 실패로 확인하지 않습니다.")
            self.skip(account, "JetStream 활성화", account, "account 상태 조회 실패로 확인하지 않습니다.")
            self.skip(account, "checker Stream Info 연결", account, "account 상태 조회 실패로 확인하지 않습니다.")
            if account in self.consumer_accounts:
                self.skip(account, "checker Consumer Info 연결", account, "account 상태 조회 실패로 확인하지 않습니다.")
                self.consumer_access[account] = False
            self.account_cache[account] = False
            return False

        if not status["exists"]:
            self.add(account, "서비스 account 확인", account, "실패", f"NATS Console에서 {account} account를 생성하세요.")
            self.skip(account, "JetStream 활성화", account, "서비스 account가 없어 확인하지 않습니다.")
            self.skip(account, "checker Stream Info 연결", account, "서비스 account가 없어 확인하지 않습니다.")
            if account in self.consumer_accounts:
                self.skip(account, "checker Consumer Info 연결", account, "서비스 account가 없어 확인하지 않습니다.")
                self.consumer_access[account] = False
            self.account_cache[account] = False
            return False
        self.add(account, "서비스 account 확인", account, "통과")

        if not status["jetstreamEnabled"]:
            self.add(account, "JetStream 활성화", account, "실패", f"{account} account에서 JetStream을 활성화하세요.")
            self.skip(account, "checker Stream Info 연결", account, "JetStream 비활성화로 확인하지 않습니다.")
            if account in self.consumer_accounts:
                self.skip(account, "checker Consumer Info 연결", account, "JetStream 비활성화로 확인하지 않습니다.")
                self.consumer_access[account] = False
            self.account_cache[account] = False
            return False
        self.add(account, "JetStream 활성화", account, "통과")

        if not status["streamInfoImportEnabled"]:
            self.add(account, "checker Stream Info 연결", account, "실패",
                     f"NATS Console의 AsyncAPI에서 {account} account Import API를 활성화하고 새 checker credentials로 교체하세요.")
            self.account_cache[account] = False
            return False
        self.add(account, "checker Stream Info 연결", account, "통과")
        if account in self.consumer_accounts:
            allowed = status["consumerInfoImportEnabled"]
            self.consumer_access[account] = allowed
            self.add(account, "checker Consumer Info 연결", account,
                     "통과" if allowed else "실패",
                     "-" if allowed else f"NATS Console의 AsyncAPI에서 {account} account Import API를 갱신하고 새 checker credentials로 교체하세요.")
        self.account_cache[account] = True
        return True

    def check_publish_permission(self, channel: Channel) -> None:
        key = (channel.account, channel.subject)
        if key in self.publish_checked:
            return
        self.publish_checked.add(key)
        target = f"{channel.account} / {channel.subject}"
        payload = {"account": channel.account, "publishSubject": channel.subject}
        try:
            status = self.request_json(STATUS_SUBJECT, json.dumps(payload, separators=(",", ":")))
            self.require_bools(status, ("exists", "publishAllowed"), "publish status")
        except RuntimeError as error:
            self.add(channel.account, "메시지 Publish 권한", target, "실패",
                     f"account Publish 권한을 조회하지 못했습니다: {error}")
            return
        passed = status["publishAllowed"]
        self.add(
            channel.account, "메시지 Publish 권한", target,
            "통과" if passed else "실패",
            "-" if passed else (
                f"NATS Console의 Accounts에서 {channel.account} account Publish permissions에 "
                f"{channel.subject}를 추가하고 Deny 규칙도 확인하세요."
            ),
        )

    def skip_publish_permission(self, channel: Channel, reason: str) -> None:
        key = (channel.account, channel.subject)
        if key not in self.publish_checked:
            self.publish_checked.add(key)
            self.skip(channel.account, "메시지 Publish 권한",
                      f"{channel.account} / {channel.subject}", reason)

    def ensure_stream(self, group: str, account: str, stream: str) -> dict[str, Any] | None:
        key = (account, stream)
        if key in self.stream_cache:
            return self.stream_cache[key]
        try:
            value = self.request_json(f"checker.{account}.$JS.API.STREAM.INFO.{stream}")
            config = value.get("config")
            if not isinstance(config, dict):
                raise RuntimeError("Stream Info 응답에 config가 없습니다")
        except RuntimeError as error:
            self.add(group, "Stream 존재", f"{account} / {stream}", "실패", f"Stream Info를 조회하지 못했습니다: {error}")
            self.stream_cache[key] = None
            return None
        self.add(group, "Stream 존재", f"{account} / {stream}", "통과")
        self.stream_cache[key] = value
        return value

    def skip_consumer(self, channel: Channel, reason: str) -> None:
        if not channel.consumer:
            return
        key = (channel.account, channel.stream, channel.consumer)
        if key not in self.consumer_checked:
            self.consumer_checked.add(key)
            self.skip(channel.account, "Consumer 준비", f"{channel.account} / {channel.stream} / {channel.consumer}", reason)

    def check_consumer(self, channel: Channel) -> None:
        if not channel.consumer:
            return
        key = (channel.account, channel.stream, channel.consumer)
        if key in self.consumer_checked:
            return
        self.consumer_checked.add(key)
        target = f"{channel.account} / {channel.stream} / {channel.consumer}"
        if not self.consumer_access.get(channel.account, False):
            self.skip(channel.account, "Consumer 준비", target, "checker Consumer Info 연결이 없어 확인하지 않습니다.")
            return
        try:
            value = self.request_json(f"checker.{channel.account}.$JS.API.CONSUMER.INFO.{channel.stream}.{channel.consumer}")
            config = value.get("config")
            if not isinstance(config, dict):
                raise RuntimeError("Consumer Info 응답에 config가 없습니다")
            if config.get("durable_name") != channel.consumer:
                raise RuntimeError("선언한 durable 이름과 실제 Consumer 이름이 다릅니다")
        except RuntimeError as error:
            self.add(channel.account, "Consumer 준비", target, "실패", f"NATS Console의 {channel.account} account / {channel.stream}에서 {channel.consumer} Consumer를 생성하세요. 상세: {error}")
            return
        self.add(channel.account, "Consumer 준비", target, "통과")

    def source_setup_target(self, channel: Channel, description: str) -> str:
        if description in {
            "checker source 상태 API 연결",
            "원본 account 확인",
            "원본 JetStream 활성화",
            "Stream Source 제공 허용",
            "Stream Source 원본 export",
        }:
            return channel.source_account
        if description in {"대상 account 확인", "Stream Source 대상 import"}:
            return channel.account
        return self.source_connection_target(channel)

    def skip_source_setup(self, channel: Channel, reason: str) -> None:
        relation = (channel.source_account, channel.account)
        if relation in self.skipped_source_setup:
            return
        self.skipped_source_setup.add(relation)
        for description in (
            "checker source 상태 API 연결", "원본 account 확인", "원본 JetStream 활성화",
            "대상 account 확인", "Stream Source 제공 허용", "Stream Source 원본 export",
            "Stream Source 대상 import", "Stream Source account 연결 설정",
        ):
            self.skip(
                channel.account, description,
                self.source_setup_target(channel, description), reason,
            )

    def ensure_source_setup(self, channel: Channel) -> bool:
        key = (channel.source_account, channel.account)
        if key in self.source_cache:
            return self.source_cache[key]
        payload = {"account": channel.source_account, "sourceTarget": channel.account}
        try:
            status = self.request_json(STATUS_SUBJECT, json.dumps(payload, separators=(",", ":")))
            self.require_bools(status, ("exists", "jetstreamEnabled"), "source status")
            sharing = status.get("sourceSharing")
            fields = (
                "targetExists", "sourceSharingEnabled", "sourceExportsEnabled",
                "consumerAPIImportEnabled", "deliverySubjectImportEnabled",
                "flowControlImportEnabled",
            )
            if not isinstance(sharing, dict):
                raise RuntimeError("source status response is missing source-sharing fields")
            self.require_bools(sharing, fields, "source status")
        except RuntimeError as error:
            description = "checker source 상태 API 연결"
            self.add(
                channel.account, description,
                self.source_setup_target(channel, description), "실패",
                f"NATS Console의 AsyncAPI에서 Update checker access를 실행하고 "
                f"새 checker credentials로 교체하세요. 상세: {error}",
            )
            for description in (
                "원본 account 확인", "원본 JetStream 활성화", "대상 account 확인",
                "Stream Source 제공 허용", "Stream Source 원본 export",
                "Stream Source 대상 import", "Stream Source account 연결 설정",
            ):
                self.skip(
                    channel.account, description,
                    self.source_setup_target(channel, description),
                    "공유 상태 조회 실패로 확인하지 않습니다.",
                )
            self.source_cache[key] = False
            return False

        checks = [
            ("원본 account 확인", status["exists"], f"NATS Console에서 {channel.source_account} account를 생성하세요."),
            ("원본 JetStream 활성화", status["jetstreamEnabled"], f"{channel.source_account} account에서 JetStream을 활성화하세요."),
            ("대상 account 확인", sharing["targetExists"], f"NATS Console에서 {channel.account} account를 생성하세요."),
            ("Stream Source 제공 허용", sharing["sourceSharingEnabled"], f"NATS Console에서 {channel.source_account} account의 Stream Source 공유를 활성화하세요."),
            ("Stream Source 원본 export", sharing["sourceExportsEnabled"], f"{channel.source_account} account의 source API, delivery, flow-control export를 갱신하세요. NATS Console의 Accounts에서 {channel.source_account}가 Allow as source 상태인지 확인한 뒤, {channel.account}의 Source sharing에서 {channel.source_account}를 선택하고 Add source account를 실행하세요."),
            ("Stream Source 대상 import", all(sharing[name] for name in fields[3:]), f"{channel.account} account에서 {channel.source_account}를 Add source account로 다시 추가하세요."),
        ]
        ready = True
        for description, passed, action in checks:
            self.add(
                channel.account, description,
                self.source_setup_target(channel, description),
                "통과" if passed else "실패", "-" if passed else action,
            )
            ready = ready and passed
        description = "Stream Source account 연결 설정"
        self.add(
            channel.account, description,
            self.source_setup_target(channel, description),
            "통과" if ready else "실패",
            "-" if ready else "실패한 source 공유 설정을 먼저 해결하세요.",
        )
        self.source_cache[key] = ready
        return ready

    @staticmethod
    def subject_matches(pattern: str, subject: str) -> bool:
        pattern_tokens = pattern.split(".")
        subject_tokens = subject.split(".")
        index = 0
        for position, token in enumerate(pattern_tokens):
            if token == ">":
                return position == len(pattern_tokens) - 1 and index < len(subject_tokens)
            if index >= len(subject_tokens) or (token != "*" and token != subject_tokens[index]):
                return False
            index += 1
        return index == len(subject_tokens)

    def check_channel(self, channel: Channel) -> None:
        if not self.ensure_account(channel.account):
            if channel.publish_required:
                self.skip_publish_permission(channel, "대상 account가 준비되지 않아 확인하지 않습니다.")
            self.skip_dependents(channel, "대상 account가 준비되지 않아 확인하지 않습니다.")
            return
        if channel.publish_required:
            self.check_publish_permission(channel)
        target_info = self.ensure_stream(channel.account, channel.account, channel.stream)
        if target_info is None:
            if channel.source_stream:
                self.skip_source_setup(channel, "대상 Stream이 없어 source 설정을 확인하지 않습니다.")
                self.skip_source_link(channel, "대상 Stream이 없어 확인하지 않습니다.")
            else:
                self.skip_subject(channel, "대상 Stream이 없어 확인하지 않습니다.")
            self.skip_consumer(channel, "대상 Stream이 없어 확인하지 않습니다.")
            return

        if not channel.source_stream:
            self.check_subject(channel, target_info)
            self.check_consumer(channel)
            return

        if not self.ensure_account(channel.source_account):
            self.skip_source_setup(channel, "원본 account가 준비되지 않아 source 설정을 확인하지 않습니다.")
            self.skip_source_link(channel, "원본 account가 준비되지 않아 확인하지 않습니다.")
            self.skip_consumer(channel, "원본 account가 준비되지 않아 확인하지 않습니다.")
            return
        if self.ensure_stream(channel.account, channel.source_account, channel.source_stream) is None:
            self.skip_source_setup(channel, "원본 Stream이 없어 source 설정을 확인하지 않습니다.")
            self.skip_source_link(channel, "원본 Stream이 없어 확인하지 않습니다.")
            self.skip_consumer(channel, "원본 Stream이 없어 확인하지 않습니다.")
            return
        if not self.ensure_source_setup(channel):
            self.skip_source_link(channel, "Stream Source account 연결 설정 실패로 확인하지 않습니다.")
            self.skip_consumer(channel, "Stream Source account 연결 설정 실패로 확인하지 않습니다.")
            return
        self.check_source_link(channel, target_info)
        self.check_consumer(channel)

    def skip_dependents(self, channel: Channel, reason: str) -> None:
        key = (channel.account, channel.stream)
        if key not in self.stream_cache:
            self.skip(channel.account, "Stream 존재", f"{channel.account} / {channel.stream}", reason)
            self.stream_cache[key] = None
        if channel.source_stream:
            self.skip_source_setup(channel, reason)
            self.skip_source_link(channel, reason)
        else:
            self.skip_subject(channel, reason)
        self.skip_consumer(channel, reason)

    def skip_subject(self, channel: Channel, reason: str) -> None:
        key = (channel.account, channel.stream, channel.subject)
        if key not in self.subject_checked:
            self.subject_checked.add(key)
            self.skip(channel.account, "이벤트를 저장할 Stream 준비",
                      f"{channel.account} / {channel.stream} / {channel.subject}", reason)

    def check_subject(self, channel: Channel, info: dict[str, Any]) -> None:
        key = (channel.account, channel.stream, channel.subject)
        if key in self.subject_checked:
            return
        self.subject_checked.add(key)
        patterns = info.get("config", {}).get("subjects") or []
        passed = any(isinstance(pattern, str) and self.subject_matches(pattern, channel.subject) for pattern in patterns)
        self.add(channel.account, "이벤트를 저장할 Stream 준비",
                 f"{channel.account} / {channel.stream} / {channel.subject}",
                 "통과" if passed else "실패",
                 "-" if passed else f"{channel.account} account의 {channel.stream} subject에 {channel.subject}를 추가하세요.")

    def source_connection_target(self, channel: Channel) -> str:
        return (
            f"{channel.source_account} / {channel.source_stream} <- "
            f"{channel.account} / {channel.stream}"
        )

    def source_filter_target(self, channel: Channel) -> str:
        return f"{self.source_connection_target(channel)} [filter: {channel.source_filter}]"

    def skip_source_link(self, channel: Channel, reason: str) -> None:
        connection_key = (channel.account, channel.stream, channel.source_account, channel.source_stream)
        filter_key = (*connection_key, channel.source_filter)
        if connection_key not in self.source_connection_checked:
            self.source_connection_checked.add(connection_key)
            self.skip(channel.account, "Stream Source 연결", self.source_connection_target(channel), reason)
        if filter_key not in self.source_filter_checked:
            self.source_filter_checked.add(filter_key)
            self.skip(channel.account, "Stream Source filter", self.source_filter_target(channel), reason)

    def check_source_link(self, channel: Channel, info: dict[str, Any]) -> None:
        connection_key = (channel.account, channel.stream, channel.source_account, channel.source_stream)
        filter_key = (*connection_key, channel.source_filter)
        if filter_key in self.source_filter_checked:
            return
        self.source_filter_checked.add(filter_key)
        config = info.get("config", {})
        candidates = [
            item for item in config.get("sources") or []
            if isinstance(item, dict) and item.get("name") == channel.source_stream
        ]
        mirror = config.get("mirror")
        if isinstance(mirror, dict) and mirror.get("name") == channel.source_stream:
            candidates.append(mirror)

        if not candidates:
            if connection_key not in self.source_connection_checked:
                self.source_connection_checked.add(connection_key)
                self.add(
                    channel.account, "Stream Source 연결",
                    self.source_connection_target(channel), "실패",
                    f"{channel.account} account의 {channel.stream}에 "
                    f"{channel.source_account} / {channel.source_stream}를 Stream Source로 연결하세요.",
                )
            self.skip(
                channel.account, "Stream Source filter", self.source_filter_target(channel),
                "Stream Source가 연결되지 않아 filter를 확인하지 않습니다.",
            )
            return

        if connection_key not in self.source_connection_checked:
            self.source_connection_checked.add(connection_key)
            self.add(
                channel.account, "Stream Source 연결",
                self.source_connection_target(channel), "통과",
            )
        passed = any(item.get("filter_subject") == channel.source_filter for item in candidates)
        self.add(
            channel.account, "Stream Source filter", self.source_filter_target(channel),
            "통과" if passed else "실패",
            "-" if passed else (
                f"{channel.account} account의 {channel.stream} Stream Source filter를 "
                f"{channel.source_filter}로 설정하세요."
            ),
        )

    def run(self) -> int:
        for channel in self.channels:
            self.check_channel(channel)
        self.print_results()
        if self.failed:
            print("하나 이상의 AsyncAPI Stream 검사가 실패했습니다.", file=sys.stderr)
            return 1
        selected = os.getenv("NATS_ACCOUNT", "")
        print(f"All AsyncAPI channels for {selected} are ready." if selected else "All AsyncAPI channels are ready.")
        return 0

    def print_results(self) -> None:
        colors = {"통과": "\033[32m", "실패": "\033[31m", "건너뜀": "\033[33m"} if sys.stdout.isatty() else {}
        reset = "\033[0m" if colors else ""
        current = ""
        for result in self.results:
            if result.group != current:
                if current:
                    print()
                print(f"[{result.group}]")
                current = result.group
            color = colors.get(result.status, "")
            print(f"  [{color}{result.status}{reset}] {result.description}")
            print(f"       대상: {result.target}")
            if result.action != "-":
                print(f"       조치: {result.action}")


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
        send_channels: set[str] = set()
        for operation_name, operation in (document.get("operations") or {}).items():
            if not isinstance(operation, dict):
                raise RuntimeError(f"{path.name}: operation {operation_name!r} must be an object")
            if operation.get("action") != "send":
                continue
            channel_ref = operation.get("channel")
            if not isinstance(channel_ref, dict) or not isinstance(channel_ref.get("$ref"), str):
                raise RuntimeError(f"{path.name}: send operation {operation_name!r} must reference a channel")
            ref = channel_ref["$ref"]
            prefix = "#/channels/"
            if not ref.startswith(prefix) or not ref[len(prefix):]:
                raise RuntimeError(f"{path.name}: send operation {operation_name!r} must use a local channel reference")
            send_channels.add(ref[len(prefix):])
        consumers: dict[str, str] = {}
        for operation in (document.get("operations") or {}).values():
            if not isinstance(operation, dict) or operation.get("action") != "receive":
                continue
            channel_ref = operation.get("channel")
            extension = operation.get("x-jetstream-consumer")
            if not isinstance(channel_ref, dict) or not isinstance(channel_ref.get("$ref"), str):
                raise RuntimeError("receive operation must reference a channel")
            if not isinstance(extension, dict) or not isinstance(extension.get("name"), str) or not extension["name"]:
                raise RuntimeError("receive operation has no x-jetstream-consumer.name")
            prefix = "#" + chr(47) + "channels" + chr(47)
            ref = channel_ref["$ref"]
            if not ref.startswith(prefix) or not ref[len(prefix):]:
                raise RuntimeError("receive operation must use a local channel reference")
            channel_name = ref[len(prefix):]
            if channel_name in consumers and consumers[channel_name] != extension["name"]:
                raise RuntimeError("channel declares conflicting Consumer names")
            consumers[channel_name] = extension["name"]
        for name, value in (document.get("channels") or {}).items():
            if not isinstance(value, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} must be an object")
            jetstream = value.get("x-jetstream")
            if jetstream is None:
                continue
            if not isinstance(jetstream, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} has invalid x-jetstream")
            subject, stream = value.get("address"), jetstream.get("stream")
            if not isinstance(subject, str) or not subject:
                raise RuntimeError(f"{path.name}: channel {name!r} has no address")
            if not isinstance(stream, str) or not stream:
                raise RuntimeError(f"{path.name}: channel {name!r} has no x-jetstream.stream")
            source = value.get("x-source") or {}
            if not isinstance(source, dict):
                raise RuntimeError(f"{path.name}: channel {name!r} has invalid x-source")
            source_account = source.get("account", "")
            source_stream = source.get("stream", "")
            source_filter = source.get("filterSubject", "")
            if bool(source_account) != bool(source_stream):
                raise RuntimeError(f"{path.name}: channel {name!r} must declare both x-source.account and x-source.stream")
            if source_account and (not isinstance(source_filter, str) or not source_filter):
                raise RuntimeError(f"{path.name}: channel {name!r} must declare x-source.filterSubject")
            if not source_account and source_filter:
                raise RuntimeError(f"{path.name}: channel {name!r} declares filterSubject without an x-source")
            if not selected or selected in (account, source_account):
                channels.add(Channel(account, stream, subject, source_account, source_stream, source_filter, name in send_channels, consumers.get(name, "")))
    if selected and selected not in accounts:
        names = ", ".join(sorted(accounts)) or "none"
        raise RuntimeError(f"unknown NATS_ACCOUNT={selected!r}; declared accounts: {names}")
    if not channels:
        suffix = f" for NATS_ACCOUNT={selected}" if selected else ""
        raise RuntimeError(f"no JetStream channels found{suffix}")
    return sorted(channels)


def usage() -> None:
    print("""Usage:
  NATS_URL=nats://<host>:<port> NATS_CREDS=/path/to/asyncapi-checker.creds ./asyncapi/check-channels.py

Checks every declared account by default. Set NATS_ACCOUNT to filter checks.
This program is read-only and never changes NATS configuration.""")


def main() -> int:
    if any(argument in ("-h", "--help") for argument in sys.argv[1:]):
        usage()
        return 0
    server = os.getenv("NATS_URL", "")
    creds = os.getenv("NATS_CREDS") or os.getenv("NATS_CREDENTIALS_PATH", "")
    if not server:
        print("ERROR: NATS_URL is required.", file=sys.stderr)
        return 2
    if not creds:
        print("ERROR: NATS_CREDS is required.", file=sys.stderr)
        return 2
    if not Path(creds).is_file() or not os.access(creds, os.R_OK):
        print(f"ERROR: NATS credentials file is not readable: {creds}", file=sys.stderr)
        return 2
    if shutil.which("nats") is None:
        print("ERROR: the NATS CLI ('nats') is required.", file=sys.stderr)
        return 2
    try:
        channels = discover_channels(os.getenv("NATS_ACCOUNT", ""))
    except RuntimeError as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2
    return Checker(server, creds, os.getenv("NATS_TIMEOUT", "5s"), channels).run()


if __name__ == "__main__":
    raise SystemExit(main())
