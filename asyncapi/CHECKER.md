# AsyncAPI checker architecture

이 문서는 `asyncapi/check-channels.py`가 AsyncAPI 선언과 실제 NATS 구성을
어떻게 비교하는지, 그 검사를 가능하게 하는 `asyncapi-checker` account와 권한이
어떻게 구성되는지 설명한다.

`checker`는 NATS나 AsyncAPI 표준 기능의 이름이 아니다. 이 저장소가 여러 NATS
account를 최소 권한으로 검사하기 위해 만든 전용 account, user, API 연결, 검사
스크립트의 묶음이다.

## 목적과 경계

checker의 목적은 다음 질문에 읽기 전용으로 답하는 것이다.

- AsyncAPI에 선언한 서비스 account가 존재하는가?
- 해당 account에서 JetStream이 활성화됐는가?
- 선언한 Stream이 실제로 존재하는가?
- 일반 channel address가 Stream의 subject 패턴에 포함되는가?
- `x-source`로 선언한 원본 account와 대상 account 사이의 source-sharing 권한이 준비됐는가?
- 대상 Stream의 `sources` 또는 `mirror`에 선언한 원본 Stream이 연결됐는가?

checker는 다음 작업을 수행하지 않는다.

- 이벤트 publish 또는 subscribe
- Stream/consumer 생성, 수정, 삭제
- account 생성 또는 source-sharing 자동 수정
- system credentials 배포
- AsyncAPI 선언을 기준으로 NATS 구성을 자동 변경

## 구성요소

| 구성요소 | 위치 | 역할 |
| --- | --- | --- |
| AsyncAPI 문서 | `asyncapi/*.yaml` | 기대하는 account, channel, Stream, source 관계 선언 |
| 검사 스크립트 | `asyncapi/check-channels.py` | 문서에서 검사 계획을 만들고 NATS 응답과 비교 |
| `asyncapi-checker` account | NATS resolver | 여러 account의 제한된 API를 모으는 독립 subject 공간 |
| `asyncapi-checker` user | Console store + 발급된 `.creds` | 허용된 요청 subject만 publish하고 `_INBOX.>`로 응답 수신 |
| 서비스 account | blog, eventpipe, s3 등 | 실제 JetStream Stream 소유 |
| system account | NATS system account | Console의 제한된 checker status service export 소유 |
| Console API 서버 | Go API 프로세스 | checker 구성 및 custom status NATS service 응답 |
| NATS JetStream | NATS 서버 | `$JS.API.STREAM.INFO.<stream>` 표준 API 응답 |

## 두 가지 흐름

checker에는 서로 다른 두 흐름이 있다.

### 구성 흐름

Console의 HTTP API가 account JWT, export/import, activation token, user 권한을
구성한다.

```text
브라우저 AsyncAPI 화면
  -> Console HTTP API
  -> account/user 생성 또는 JWT claims 갱신
  -> NATS resolver에 새 account JWT push
  -> 새 checker credentials 발급
```

### 검사 흐름

`check-channels.py`는 Console HTTP API를 호출하지 않는다. 발급된 checker
credentials로 NATS request/reply를 직접 실행한다.

```text
check-channels.py
  -> checker status NATS service
  -> JetStream Stream Info NATS API
  -> AsyncAPI 문서와 응답 비교
```

## Console HTTP API와 checker 생성

### 상태 조회

```http
GET /api/v1/asyncapi
```

반환 내용:

- checker account 존재 여부
- checker account 정보
- 서비스 account별 Stream Info import 활성화 여부
- 각 import의 로컬 alias

### checker 생성 또는 공통 access 갱신

```http
POST /api/v1/asyncapi
```

checker가 없으면 다음을 생성한다.

1. `asyncapi-checker` account NKey
2. `asyncapi-checker` user NKey
3. checker user의 `_INBOX.>` subscribe 권한
4. account JWT를 NATS resolver에 push
5. checker status export/import 및 publish 권한

checker가 이미 있으면 account를 다시 만들지 않고 user와 status access를
멱등적으로 보장한다. UI의 `Create AsyncAPI Checker`와
`Update checker access`가 같은 endpoint를 사용한다.

### 서비스 account Stream Info import

```http
POST /api/v1/asyncapi/imports/{accountPublicKey}
Content-Type: application/json

{"enabled": true}
```

활성화하면 서비스 account와 checker account claims를 함께 갱신한다.

- 서비스 account: `$JS.API.STREAM.INFO.>` service export 추가
- checker account: 해당 export의 service import 추가
- checker user store: 로컬 alias publish allow 추가
- 양쪽 account JWT: operator key로 다시 서명하여 resolver에 push

### credentials 발급

```http
GET /api/v1/accounts/{operator}/{checkerAccountPublicKey}/users/asyncapi-checker/creds
```

`.creds`에는 user JWT와 user seed가 들어 있다. user JWT는 발급 시점의 publish와
subscribe 권한을 포함하므로 import나 checker access를 변경한 뒤에는 반드시 새
credentials를 받아야 한다.

## Stream Info 연결

blog account를 예로 들면 서비스 account에는 다음 export가 구성된다.

```text
account: blog
name: nats-console-asyncapi-stream-info-export
subject: $JS.API.STREAM.INFO.>
type: service
tokenRequired: true
```

checker account에는 다음 import가 구성된다.

```text
account: asyncapi-checker
name: nats-console-asyncapi-stream-info-import-<blog-public-key>
remote account: <blog-public-key>
remote subject: $JS.API.STREAM.INFO.>
local subject: checker.blog.$JS.API.STREAM.INFO.>
type: service
token: <blog가 서명한 activation JWT>
```

checker user에는 다음 권한이 필요하다.

```text
publish allow: checker.blog.$JS.API.STREAM.INFO.>
subscribe allow: _INBOX.>
```

여기서 alias의 `blog` 문자열은 사람이 읽기 위한 이름이다. 실제 routing 대상은
import claim의 remote account public key가 결정한다.

```text
checker.blog.$JS.API.STREAM.INFO.BLOG_EVENTS
  -> import가 지정한 blog account
  -> $JS.API.STREAM.INFO.BLOG_EVENTS
  -> blog account의 BLOG_EVENTS
```

서로 다른 account에 같은 이름의 Stream이 있어도 account context가 다르므로
충돌하지 않는다.

```text
checker.blog.$JS.API.STREAM.INFO.EVENTS
  -> blog / EVENTS

checker.eventpipe.$JS.API.STREAM.INFO.EVENTS
  -> eventpipe / EVENTS
```

## Export, import, activation token

세 요소는 서로 다른 판단을 담당한다.

| 요소 | 선언 주체 | 판단 질문 |
| --- | --- | --- |
| Export | 원본 account | 이 subject를 account 밖에 제공하는가? |
| Import | 대상 account | 어느 account의 export를 어떤 로컬 subject로 사용할 것인가? |
| Activation token | 원본 account가 서명, 대상 import가 보관 | 원본 account가 정확히 이 대상 account의 import를 허용했는가? |

### 공개 export

`TokenReq: false`이면 export와 일치하는 import가 activation token 없이 연결될 수
있다. 그래도 import에는 원본 account public key와 subject가 필요하다.

### token-required export

`TokenReq: true`이면 import 선언만으로 연결되지 않는다. 원본 account signing key로
서명한 activation JWT가 import에 포함되어야 한다.

activation token의 핵심 claims는 다음과 같다.

```text
issuer account: 원본 account public key
subject: 허용받는 대상 account public key
import subject: 허용한 subject
import type: service 또는 stream
```

이 프로젝트의 Stream Info 연결에서는 다음 관계가 된다.

```text
issuer: blog
subject/importer: asyncapi-checker
import subject: $JS.API.STREAM.INFO.>
import type: service
```

activation token은 매 request에 payload나 header로 보내는 bearer token이 아니다.
checker account JWT의 import에 저장되고, NATS 서버가 account claims를 로드하고
account 간 routing을 구성할 때 검증한다.

### 사용자 credentials와 activation token의 차이

```text
user JWT 권한
  asyncapi-checker user가 로컬 alias에 publish할 수 있는가?

activation token
  asyncapi-checker account가 blog account의 export를 import할 수 있는가?
```

호출이 성공하려면 두 계층 모두 통과해야 한다.

```text
checker user publish allow
  -> checker account import
  -> activation token 검증
  -> source account export
  -> source account의 responder
```

## NATS request/reply 실행

스크립트의 Stream Info 요청은 본질적으로 다음 명령이다.

```bash
nats \
  --server "$NATS_URL" \
  --creds "$NATS_CREDS" \
  request \
  --raw \
  --timeout 5s \
  'checker.blog.$JS.API.STREAM.INFO.BLOG_EVENTS' \
  ''
```

NATS request/reply는 publish와 임시 subscription의 조합이다.

1. CLI가 `_INBOX.<unique>` reply subject를 생성한다.
2. CLI가 해당 inbox를 subscribe한다.
3. CLI가 요청 subject에 빈 payload와 reply subject를 함께 publish한다.
4. service import가 요청을 blog account로 전달한다.
5. JetStream이 `BLOG_EVENTS` 정보를 조회한다.
6. JetStream이 reply subject로 JSON을 publish한다.
7. CLI가 inbox 응답을 받아 `--raw`로 payload만 출력한다.

개념적인 wire protocol은 다음과 같다.

```text
SUB _INBOX.x 1
PUB checker.blog.$JS.API.STREAM.INFO.BLOG_EVENTS _INBOX.x 0

<JetStream processing>

MSG _INBOX.x 1 <bytes>
{...stream info JSON...}
```

service import는 요청뿐 아니라 응답이 원래 requester의 inbox로 돌아가는 reply
routing도 관리한다.

## Checker status service

`checker.status.account`는 JetStream 표준 API가 아니라 이 프로젝트의 custom NATS
service다.

```text
checker local subject: checker.status.account
system account subject: nats.console.asyncapi.status
responder: NATS Console API 프로세스
```

구성:

```text
system account
  service export:
    nats.console.asyncapi.status
    tokenRequired: true

asyncapi-checker account
  service import:
    checker.status.account
    -> system account / nats.console.asyncapi.status

checker user
  publish allow:
    checker.status.account
```

Console API는 시작할 때 다음 subject를 subscribe한다.

```text
nats.console.asyncapi.status
```

account 상태 요청:

```json
{"account":"blog"}
```

응답:

```json
{
  "exists": true,
  "jetstreamEnabled": true,
  "streamInfoImportEnabled": true
}
```

`send` operation의 account Publish 권한 요청:

```json
{"account":"blog","publishSubject":"blog.draft.created"}
```

응답에는 account JWT의 기본 Publish Allow/Deny로 계산한 결과가 추가된다.
Allow가 비어 있으면 제한 없음으로 판단하고, Deny는 Allow보다 우선한다.

```json
{"publishAllowed":true}
```

source-sharing 상태까지 요청:

```json
{
  "account": "blog",
  "sourceTarget": "eventpipe"
}
```

응답의 추가 부분:

```json
{
  "sourceSharing": {
    "targetExists": true,
    "sourceSharingEnabled": true,
    "sourceExportsEnabled": true,
    "consumerAPIImportEnabled": true,
    "deliverySubjectImportEnabled": true,
    "flowControlImportEnabled": true
  }
}
```

이 API는 account key, JWT 원문, 전체 account 목록을 노출하지 않고 검사에 필요한
boolean만 반환한다.

## Cross-account JetStream source 구성

`x-source` 검사는 Stream Info import와 별개의 account 간 권한을 확인한다.
JetStream source가 account를 넘으려면 단순히 원본 Stream 이름만 지정해서는 안 된다.

예를 들어 blog `BLOG_EVENTS`를 eventpipe `EVENTPIPE_INBOX`의 source로 사용할 때 필요한
구성은 다음과 같다.

### 원본 blog account

| 종류 | Subject | 목적 |
| --- | --- | --- |
| Service export | `$JS.API.CONSUMER.>` | 원본 Stream에서 source consumer 생성·관리 |
| Stream export | `$JS.SOURCE.<eventpipe-public-key>.>` | source message delivery |
| Service export | `$JS.FC.>` | flow-control message 처리 |

세 export는 모두 token-required다.

### 대상 eventpipe account

| 종류 | 원본 | 목적 |
| --- | --- | --- |
| Service import | blog / `$JS.API.CONSUMER.>` | 원본 consumer API 호출 |
| Stream import | blog / delivery subject | 원본 message 수신 |
| Service import | blog / `$JS.FC.>` | flow-control 왕복 |

각 import는 blog account가 eventpipe account를 대상으로 서명한 별도의 activation
token을 포함한다.

### source-sharing enabled tag

원본 account claims에는 다음 tag가 있어야 한다.

```text
nats-console-source-enabled
```

이 tag는 source-sharing 사용 의사를 나타낸다. checker는 tag와 실제 export/import를
모두 확인한다. tag만 존재하거나 export/import만 일부 존재하면 준비된 것으로
판단하지 않는다.

## AsyncAPI에서 검사 계획 만들기

스크립트는 `asyncapi/*.yaml`의 다음 extension을 읽는다.

```yaml
servers:
  nats:
    x-nats-account: eventpipe

channels:
  BlogDraftUpdated:
    address: blog.draft.updated
    x-jetstream:
      stream: EVENTPIPE_INBOX
    x-source:
      account: blog
      stream: BLOG_EVENTS
      filterSubject: blog.draft.updated
```

각 channel은 다음 tuple로 정규화된다.

```text
target account, target stream, address, source account, source stream, source filter, send 여부
```

예시:

```text
eventpipe, EVENTPIPE_INBOX, blog.draft.updated, blog, BLOG_EVENTS, false
```

`NATS_ACCOUNT=blog`를 지정하면 다음 두 종류를 선택한다.

- `x-nats-account: blog`인 자체 channel
- 다른 문서에서 `x-source.account: blog`로 blog를 참조하는 downstream channel

따라서 blog 단일 검사에도 `blog -> eventpipe` source-sharing과
`blog BLOG_EVENTS <- eventpipe EVENTPIPE_INBOX` 연결이 포함된다.

## 판단 기준

### Account와 checker access

| 검사 | 데이터 출처 | 통과 기준 | 실패 후 건너뛰는 검사 |
| --- | --- | --- | --- |
| checker 상태 API | NATS request/reply | 유효한 JSON 응답 수신 | account, JetStream, import, Stream 이하 |
| 서비스 account 확인 | status `exists` | `true` | JetStream, import, Stream 이하 |
| JetStream 활성화 | status `jetstreamEnabled` | `true` | Stream Info import, Stream 이하 |
| checker Stream Info 연결 | status `streamInfoImportEnabled` | `true` | Stream 존재와 channel 검사 |

checker status API 성공은 내부 조회 성공이므로 정상 결과에는 출력하지 않는다.
실패할 때만 checker access 문제로 표시한다.

### 일반 channel

`operations.action: send`로 참조된 channel에만 Publish 권한 검사를 추가한다.
`receive` channel과 `x-source` 복제에는 서비스 account의 직접 Publish 권한을 요구하지 않는다.


| 검사 | 데이터 출처 | 통과 기준 |
| --- | --- | --- |
| 메시지 Publish 권한 | `action: send` channel address와 status `publishAllowed` | account 기본 Publish Allow에 포함되고 Deny에 포함되지 않음 |
| Stream 존재 | `$JS.API.STREAM.INFO.<stream>` | API error가 없고 `config`가 존재 |
| subject 저장 | `config.subjects` | channel address가 하나 이상의 NATS subject 패턴과 일치 |

NATS wildcard 판단:

- `*`: 정확히 한 token
- `>`: 패턴 마지막에서 하나 이상의 남은 token

예시:

```text
blog.draft.created matches blog.draft.>
blog.draft.created matches blog.*.created
blog.draft.created does not match blog.created
```

### `x-source` channel

| 검사 | 데이터 출처 | 통과 기준 |
| --- | --- | --- |
| 원본 account | status `exists` | `true` |
| 원본 JetStream | status `jetstreamEnabled` | `true` |
| 대상 account | source status `targetExists` | `true` |
| 공유 활성화 | `sourceSharingEnabled` | 원본 claims에 enabled tag 존재 |
| 원본 exports | `sourceExportsEnabled` | Consumer API, delivery, flow-control export가 모두 정확하고 token-required |
| 대상 imports | 세 import boolean | account, subject, type이 일치하고 activation token이 모두 존재 |
| 대상 Stream 존재 | 대상 Stream Info | `config` 존재 |
| 원본 Stream 연결 | 대상 `config.sources` 또는 `config.mirror` | 선언한 source Stream 이름 존재 |
| 원본 Stream filter | 일치하는 source의 `filter_subject` | `x-source.filterSubject`와 정확히 일치 |

source-sharing 구성 실패 시 실제 source/mirror 연결 검사는 불필요하므로
`건너뜀`으로 표시한다.

## 결과 상태

| 상태 | 의미 |
| --- | --- |
| `통과` | 해당 판단 기준을 직접 확인했고 만족함 |
| `실패` | 해당 판단 기준을 직접 확인했고 만족하지 않거나 요청이 실패함 |
| `건너뜀` | 선행 조건 실패로 후속 검사가 의미 없거나 수행할 수 없음 |

예를 들어 account가 없으면 다음과 같이 판단한다.

```text
서비스 account 확인: 실패
JetStream 활성화: 건너뜀
checker Stream Info 연결: 건너뜀
Stream 존재: 건너뜀
subject/source 연결: 건너뜀
```

## 보안 경계

checker user가 필요한 최소 권한:

```text
publish:
  checker.status.account
  checker.<enabled-account>.$JS.API.STREAM.INFO.>

subscribe:
  _INBOX.>
```

checker user에게 다음 권한은 주지 않는다.

```text
서비스 event subject
$JS.API.STREAM.CREATE.>
$JS.API.STREAM.UPDATE.>
$JS.API.STREAM.DELETE.>
$JS.API.CONSUMER.>
```

status service도 account claims 전체 대신 제한된 boolean만 반환한다. 이 구조는
system account credentials나 각 서비스 account credentials를 검사 스크립트에
배포하지 않으면서 필요한 정보만 읽게 한다.

## 운영 절차

1. Console AsyncAPI 화면에서 checker account를 생성한다.
2. `Update checker access`로 status export/import와 user 권한을 최신 상태로 만든다.
3. 검사할 서비스 account에서 `Import API`를 활성화한다.
4. `x-source`가 있으면 원본 account의 source sharing을 활성화하고 대상 account에 grant한다.
5. 새 checker credentials를 다운로드한다.
6. `NATS_CREDS`가 새 파일을 가리키게 한다.
7. `check-channels.py`를 실행한다.

```bash
NATS_URL=nats://localhost:4222 \
NATS_CREDS=/path/to/asyncapi-checker.creds \
./asyncapi/check-channels.py
```

## 문제 해결 기준

| 증상 | 실제 판단 | Console에서 수행할 작업 |
| --- | --- | --- |
| checker status 응답 없음 또는 publish violation | 기존 credentials에 status 권한이 없거나 status import가 오래됨 | `Update checker access`, 새 credentials 다운로드 |
| `streamInfoImportEnabled: false` | 서비스 account의 Stream Info export/import 없음 | 해당 account의 `Import API` 활성화, 새 credentials 다운로드 |
| account `exists: false` | Console repository에 account 없음 | account 생성 |
| `jetstreamEnabled: false` | account claims의 JetStream limit 비활성 | account JetStream 활성화 |
| source sharing disabled | 원본 account enabled tag 없음 | 원본 account의 source sharing 활성화 |
| source exports/imports 일부 false | source grant claims 불완전 | 원본에서 대상 account로 source grant 재설정 |
| Stream Info `stream not found` | 대상 account에는 있지만 선언한 Stream 없음 | 해당 account에 Stream 생성 또는 문서 수정 |
| subject 불일치 | Stream은 있지만 channel address를 저장하지 않음 | Stream subjects 또는 AsyncAPI address 수정 |
| source/mirror 불일치 | 권한은 있으나 대상 Stream config가 원본 Stream을 참조하지 않음 | 대상 Stream source 설정 수정 |

## 코드 위치

| 구현 | 파일 |
| --- | --- |
| 검사 계획, NATS 요청, 결과 판단 | `asyncapi/check-channels.py` |
| checker HTTP handlers | `api/internal/accounts/asyncapi_handler.go` |
| checker claims와 status 판단 | `api/internal/accounts/asyncapi.go` |
| cross-account source export/import | `api/internal/accounts/source_sharing.go` |
| custom status NATS responder | `api/internal/httpapi/server.go` |
| HTTP routes | `api/internal/httpapi/routes.go` |
| AsyncAPI UI | `web/src/pages/AsyncAPIPage.tsx` |

## 핵심 요약

```text
Export
  원본 account가 무엇을 제공할지 결정

Import
  대상 account가 어느 export를 어떤 로컬 subject로 사용할지 결정

Activation token
  원본 account가 특정 대상 account의 import를 승인

User JWT
  checker user가 로컬 alias를 호출할 수 있는지 결정

Account context
  같은 Stream 이름 중 어느 account의 Stream을 조회할지 결정

Stream Info API
  실제 Stream config를 반환

check-channels.py
  반환된 config와 AsyncAPI 선언을 비교
```

## Consumer declaration and check

A receive operation declares its durable Consumer with x-jetstream-consumer.name. The initial naming convention is STREAM_CONSUMER, and channels consumed from the same Stream share one name.

The checker validates account, JetStream, Stream, and Stream Source prerequisites first. It then requests checker.ACCOUNT.$JS.API.CONSUMER.INFO.STREAM.CONSUMER and passes only when the response contains the declared durable_name. Each account, Stream, and Consumer tuple is checked once.

The service account Import API action now manages both token-protected Stream Info and Consumer Info exports and imports, including checker publish permissions. Existing imports must be updated once and fresh checker credentials downloaded after introducing Consumer checks.
