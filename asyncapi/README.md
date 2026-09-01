# AsyncAPI

서비스별 AsyncAPI 문서는 루트에 두고, 공통 메시지와 payload 스키마는 각각
`messages/`, `schemas/`에서 재사용한다.

checker의 account 경계, Export/Import/Activation token, 권한 구성, 요청 흐름과
모든 통과·실패·건너뜀 판단 기준은 [Checker architecture](./CHECKER.md)에 정리되어 있다.

## Event identity

모든 이벤트 payload는 `eventId`를 포함한다. 이전 이벤트 없이 시작하는 이벤트는
새 UUID를 생성한다. 이전 이벤트로부터 파생되는 이벤트는 이전 이벤트의 `eventId`를
`sourceEventId`로 보존하고 다음 규칙으로 `eventId`를 결정한다.

```text
eventId = UUIDv5(namespace = sourceEventId, name = current subject)
```

같은 이전 이벤트와 같은 subject 조합은 항상 같은 `eventId`를 만들므로 재처리 시에도
동일한 이벤트 식별자를 사용한다.

## JetStream channel check

`check-channels.py`는 모든 NATS account에서 서비스 문서의
`x-jetstream.stream`에 선언된 channel address가 실제 Stream에 포함되는지 검사한다.
Stream을 생성하거나 변경하지 않으며, `asyncapi-checker` account의 user credentials가
필요하다.

```bash
NATS_URL=nats://nats.iterating.io:34222 \
NATS_CREDS=/path/to/asyncapi-checker.creds \
./asyncapi/check-channels.py
```

기본값은 전체 account 검사다. 특정 account만 검사하려면 `NATS_ACCOUNT=blog`처럼
지정한다. NATS Console에서 검사할 각 서비스 account의 Stream Info 및 Consumer Info import를
`asyncapi-checker`에 활성화해야 한다.
`NATS_ACCOUNT`를 지정하면 해당 account가 소유한 채널뿐 아니라 다른 문서가
`x-source.account`로 해당 account를 참조하는 downstream source 연결도 검사한다.

## Stream naming

Stream 이름은 메시지의 방향과 의미에 따라 구분한다.

- `<SERVICE>_EVENTS`: 서비스가 직접 발생시킨 사실을 저장한다. `x-source`를 사용하지 않는다.
- `<SERVICE>_INBOX`: 서비스가 외부에서 받아 처리할 이벤트나 명령을 저장한다. 원본을 `x-source`로 선언한다.
- `<SERVICE>_COMMANDS`: 서비스가 다른 서비스에 요청하는 명령을 저장한다. 대상 서비스는 이를 자신의 `INBOX`로 가져간다.

`receive` operation의 channel은 `INBOX`, 이벤트 `send` operation은 `EVENTS`,
명령 `send` operation은 `COMMANDS`를 사용한다. 하나의 Stream에 수신 메시지와
서비스가 발생시킨 메시지를 함께 저장하지 않는다.

## Account flow

```text
blog / BLOG_EVENTS (blog.draft.*)
  -> eventpipe / EVENTPIPE_INBOX

eventpipe / EVENTPIPE_COMMANDS (s3.objects.reconcile)
  -> s3 / S3_INBOX

s3 / S3_EVENTS (s3.objects.reconciled)
```

각 서비스는 자기 account의 Stream과 consumer만 사용한다. `x-source`는 대상의
`INBOX` Stream이 복제하는 원본 account와 `EVENTS` 또는 `COMMANDS` Stream을 나타낸다.
따라서 `S3_INBOX`에는 수신한 `s3.objects.reconcile`만 저장되고, S3가 직접 발생시킨
`s3.objects.reconciled`는 `S3_EVENTS`에 별도로 저장된다.
