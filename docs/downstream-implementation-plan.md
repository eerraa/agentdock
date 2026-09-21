# AgentDock 최소 downstream 구현 계획서

- 문서 ID: AD-DOWNSTREAM-PLAN-20260922-v1
- 작성일: 2026-09-22, Asia/Seoul
- 상태: 구현 지시용 계획. 이 문서 작성 시 제품 소스 수정·빌드·제품 시험·commit·push·배포는 수행하지 않았다.
- 검토 기준: `A-m-o-r-F-a-t-i/agentdock`, commit `e6380d4779a8bfe6093205a8b442d95246141a68`.
- HJ 기준선 위치: `D:\Engineering\agentdock`. Cursor Cloud에서는 실제 clone root를 사용한다.
- 실행 프롬프트: [grokbot-cursor-handoff.md](grokbot-cursor-handoff.md).

## 0. 목표, 우선순위, 실행 권한

### 0.1 최종 결과

현재 upstream을 유지하면서 다음 두 기능군만 구현한다.

1. Windows WPF UI 문자열의 공통 리소스화, 한국어 UI, 독립적인 한국어 installer 리소스.
2. 공개 `session_observe peek`와 current-runtime epoch 한정 `exec_command` request receipt.

장치 target guard는 설계 보류 결정만 유지한다. 이번 실행에서 guard 코드, instance/profile 설정 체계, identity UI를 추가하지 않는다. 나머지 Conversation / Call / Activity / Task / Thread / Workspace / Permission 엔진은 그대로 사용한다.

우선순위는 안전·기존 계약 보존 > 실제 검증 > downstream 크기 > 편의 기능이다. 아래의 파일명·내부 helper 이름은 권장 소유 위치이며 공개 wire 계약·불변조건·필수 시험은 구현 계약이다. 최신 upstream에 대응 기능이 생겼으면 의미와 시험을 확인하여 중복 패치를 생략한다.

### 0.2 역할과 순차 실행

Grokbot은 Cursor Cloud 실행 설정·문서 전달·작업 상태 확인·결과 수집을 담당한다. 제품 코드는 Cursor Cloud의 단일 worker가 수정한다. 요청 모델 설정은 `Grok 4.7 / 500k / xHigh`다. 실제 사용 가능 여부는 시작 전에 Cursor UI 또는 현재 API의 모델·variant 목록으로 확인한다. 모델 ID나 parameter 이름을 추측하지 않는다.

동시에 제품 소스를 수정하는 worker는 한 명만 허용한다. P0 → P1 → P2 → P3 → P4 → P5 → P6 순서로 진행한다. 일반적으로 동일 cloud agent·동일 feature branch를 재사용한다. worker 교체가 필요하면 이전 실행 종료와 작업 저장을 확인하고 다음 worker에게 동일 branch/HEAD와 checkpoint를 넘긴다. subagent, ACP, 다른 Bot의 병렬 구현·병렬 리뷰를 사용하지 않는다. 시험 내부의 동시성 재현 및 `t.Parallel`은 이 제한과 다르다.

### 0.3 권한 경계

이 계획서 자체는 클라우드 실행이나 원격 쓰기를 자동으로 시작하지 않는다. 사용자가 동봉 프롬프트를 전달하여 구현을 지시한 후, worker는 격리 개발 checkout에서 소스·직접 관련 시험·기능 문서를 수정하고 빌드·시험할 수 있다.

클라우드 안에서 필요한 Go/.NET/PowerShell/시험 의존성을 설치하는 것은 격리 개발 환경 준비로 한정한다. 운영 AgentDock 설치·업데이트·실행 설정 변경은 금지한다. HJ 운영 서비스, 실제 사용자 Task/Activity 저장소, OAuth 비밀, 시작프로그램, DNS/Tunnel, 장치 이름에는 접근하지 않는다. 제품 시험은 `t.TempDir` 또는 새 임시 fixture root만 사용한다.

클라우드 feature branch 안의 작업 commit은 인계 단위로 허용한다. 원격 push 및 draft PR은 사용자가 이미 구현 대상으로 명시한 writable downstream 저장소의 feature branch에 한해서만 수행한다. 그런 목적지가 확인되지 않으면 임의 fork·원격 저장소 생성·upstream push를 하지 않는다. Cursor의 자동 branch push를 제한할 수 없다면 목적지 승인 전에는 cloud 실행을 시작하지 않는다. `main` 직접 commit/push, force push, PR merge, tag/release 발행, 서명·배포는 제외한다.

HJ 기준선 저장소는 이번 문서 작성 이후에도 제품 구현 대상으로 직접 덮어쓰지 않는다. 클라우드 결과의 로컬 적용은 후속 별도 요청으로 남긴다.

### 0.4 구현 제외

ERA 코드 복사, k7/m2/v9/i1 제품 표면, 도구 숨김, guidance/admission/context/monitor 재구축, ERA installer/profile 이식, ERA 파일·경로 회귀시험의 일괄 이식, 자동 모델 재개, 20분 정체 해결 주장, unknown side effect 자동 replay는 제외한다.

macOS UI, 별도 Go tray, 웹 상태 페이지/OAuth HTML, 외부 `agentdock-protocol/mcpapps` 한국어화도 이번 구현 범위가 아니다. 서버가 생성한 과거 기록 본문·자유 형식 오류와 사용자 입력·명령 출력은 자동 번역하지 않는다.

## 1. 기준선과 증거

### 1.1 문서 작성 시 확인 결과

HJ root는 `D:/Engineering/agentdock`, branch는 `main`, HEAD는 위 commit이다. `origin`은 `https://github.com/A-m-o-r-F-a-t-i/agentdock.git`이다. 문서 작성 전 `git ls-remote origin refs/heads/main`은 같은 SHA를 반환했고 tracking 상태는 `+0 -0`, 작업 트리는 clean이었다. 문서 작성으로 추가되는 Markdown 파일은 제품 변경과 구분한다.

기준선 `go.mod`는 Go `1.26.5`, MCP Go SDK `v1.7.0`, `agentdock-protocol v0.8.1`을 지정한다. WPF 프로젝트는 `net8.0-windows10.0.19041.0`, single-file publish를 사용한다. 이를 작업 시작 시 다시 확인하되 문서만을 이유로 의존성을 업그레이드하거나 downgrade하지 않는다.

실제 수행한 검사는 Git 상태·원격 SHA 대조 및 읽기 전용 XML/key 비교다. Go, WPF, installer 시험의 존재와 assertion을 검토했으나 실행하지 않았다.

### 1.2 소스 근거 지도

다음 식별자는 이 문서의 기준선 사실을 가리킨다. 줄번호보다 파일·함수를 우선하고 최신 checkout에서 재탐색한다. 고정 Git 객체는 `git show e6380d4779a8bfe6093205a8b442d95246141a68:<path>`로 확인할 수 있다.

| ID | 파일·함수 | 확인한 사실 |
|---|---|---|
| S01 | `desktop/windows/control-panel/Localization/UiText.cs` | ResourceManager/Loc 경로, en·zh-CN 정규화, 명시적 `_resourceCulture`, preference 저장 |
| S02 | `desktop/windows/control-panel/Resources/UiStrings.resx`, `UiStrings.zh-CN.resx` | 각 324 key, key 집합 차이·중복 0 |
| S03 | `Localization/ActivityText.cs` | 영·중 dictionary 109항목, State/Kind/Choose의 코드 내 문자열 |
| S04 | `ExecutionWindow.xaml`, `.xaml.cs`, `.Actions.cs`, `ExecutionDialogs.cs`, `Models/ExecutionModels.cs`, `Services/ExecutionClient.cs`, `MainWindow.Access.cs` | 새 UI의 중국어 literal 및 표시 문자열에 의존한 일부 분기 |
| S05 | `MainWindow.xaml:317–322`, `App.xaml.cs:203–241`, `App.Activity.cs:47–59` | selector와 창 재생성을 통한 언어 변경 |
| S06 | `packaging/windows/AgentDock.iss`, `includes/messages.iss`, `includes/code.iss`, `languages/ChineseSimplified.isl` | 두 installer 언어, 각 54 custom message, CustomMessage 경로 |
| S07 | `scripts/test/windows_ui_test.go`, `scripts/test/install_windows_test.go`, `scripts/test/testdata/activity-center/*` | 현지화 관련 정적 검사 및 실제 WPF fixture harness |
| S08 | `internal/tool/command/request.go`, `contract.go`, `actions.go` | exec request ID 없음, 공개 session 관찰은 list/status |
| S09 | `internal/tool/command/command.go:33–159` | sync/빠른 auto 완료는 Session Store 미보존, 요청 취소/async는 보존 |
| S10 | `command.go:280–291,412–452`, `command_session_service_test.go:168–220` | 완료 status 소비·삭제, list는 소비하지 않음 |
| S11 | `internal/tool/command/session/session.go`, `session/activity.go` | 기존 buffer/공용 cursor, 내부 Peek, Activity의 독립 absolute cursor |
| S12 | `internal/tool/command/activity.go` | 원래 binding, 독립 Activity observer, redacted preview |
| S13 | `internal/app/execution_dispatch.go`의 `callObserved`, `executePrepared`, `decorateExecution`, `validateSessionOwnership` | Call/Permission/ownership 정본과 실행 ingress |
| S14 | `internal/app/execution_sessions.go`, `execution_scope.go`, `execution_runtime.go` | scope snapshot, 승인·경로 재검사, 승인된 fixed request 단일 dispatch |
| S15 | `internal/app/execution_recovery.go`, `runtime.go:140–148` | Runtime instance, 재시작 뒤 unknown/cancelled 처리 |
| S16 | `internal/activity/calls.go`, `model.go`, `conversation.go` | Call projection, 8 KiB preview, trusted SourceOwnerKey와 Conversation 소유권 |
| S17 | `internal/httpx/activity_api.go`, `execution_api.go`, `internal/mcp/server.go` | 관리 API는 direct-loopback, MCP `_meta`는 대화 상관정보이며 receipt 아님 |
| S18 | `internal/tool/contract/activity.go`, `internal/activity/execution_test.go:207–229` | retry_of_call_id는 새 retry, 실제 재시도 기록을 deduplicate하지 않음 |
| S19 | `internal/app/specs_command.go`, `input_decode.go`, `contract_drift_test.go` | canonical typed adapter, schema/typed request 일치 검사 |
| S20 | `internal/workspace/routing.go`, `registry.go:261–289`, `internal/mcp/apps.go` | 장치 선택과 경로 선택의 분리, context의 registry effect, Apps resource 별도 경로 |

## 2. 단계·Gate·최종 완료 정의

| 단계 | 작업 | 다음 단계에 필요한 Gate |
|---|---|---|
| P0 | 모델/권한/clone/기준선/시험 환경 확인 | 실제 root·SHA·remote·모델 설정 확인, 변경 보존, 재현 가능한 baseline |
| P1 | WPF 공통 리소스화와 문자 의존 UI 분기 제거 | resource 참조·중립/중국어 parity 검사, 실행 가능한 관련 시험, 핵심 source review |
| P2 | ko-KR·selector·installer 한국어·현지화 시험 | 번역 key/placeholder 검사, Linux에서 가능한 정적 검사, Windows 검증 대기 여부 명시 |
| P3 | 기존 Session buffer의 absolute read와 공개 session_id peek | 비소비/cursor/gap/완료/ownership/legacy 호환 시험 |
| P4 | epoch request receipt, atomic claim, receipt Session 보존 | 동일 ID 단일 시작, 승인 단일화, sync 응답 유실, 소유권·보존 경계 시험 |
| P5 | 실제 MCP transport 유실·경합·restart 통합 및 회귀 | public wire 계약과 no-replay 통합 시험, 관련 race 검사 |
| P6 | Windows native 검증·review 빌드·self-review·인계 | 최종 Gate 모두 통과하거나 미검증 상태를 명시한 draft 인계 |

각 단계는 구현 → 직접 시험 → 실제 diff 자기 검토 → 결함 수정 → 짧은 checkpoint 순서다. 구현 중 생긴 관련 결함은 해당 단계에서 해결한다. 문서 수정을 구현 완료로 보고하지 않는다.

단계 의존성은 순차지만 검증 상태는 별도 축이다. Linux Cloud에 Windows 실행 환경이 없을 때 P1/P2의 native UI 검사를 `NOT_RUN_PLATFORM`으로 기록하고 portable Gate가 통과한 뒤 P3~P5를 진행할 수 있다. 이때 최종 상태는 절대로 `VERIFIED`나 merge-ready가 아니다. 이미 승인된 격리 Windows runner가 있으면 같은 후보 HEAD에서 P6를 수행한다. 없다면 필요한 Windows 시험과 실행 명령을 담아 `IMPLEMENTED / WINDOWS_VALIDATION_REQUIRED`로 인계한다.

검사 상태는 `PASS`, `FAIL`, `BASELINE_FAIL`, `NOT_RUN_PLATFORM`, `BLOCKED_PERMISSION`, `BLOCKED_MODEL`로 구분한다. Exit 0, 시험 skip, test executable 빌드 성공, 실제 시험 통과는 각각 다르다. 기반 실패와 신규 실패를 분리하고, 기존 실패를 이유로 새 기능 시험을 삭제하거나 약화하지 않는다.

## 3. P0 — 실행 환경과 작업 기준선

### 3.1 Grokbot 시작 전 점검

1. 첨부 계획서 전체를 읽고 문서 ID와 SHA-256을 확인한다. HJ 로컬 경로만 cloud worker에게 보내지 않는다. 계획서가 remote branch에 없으면 파일 첨부 또는 내용 전달로 worker checkout에 복원하고 동일 hash를 확인한다.
2. 요청 설정 `Grok 4.7 / 500k / xHigh`를 실제 Cursor selector 또는 account API에서 선택·확인한다. `Auto`, 다른 모델, smaller context, 다른 effort로 무단 대체하지 않는다. API 사용 시 현재 모델 목록의 실제 ID와 허용 params/variant를 사용한다. 자연어 프롬프트에 모델 이름을 쓰는 것만으로 설정이 적용됐다고 보고하지 않는다.
3. 실제 writable downstream 목적지와 feature branch만 쓰기 대상으로 허용한다. public upstream origin의 존재나 GitHub 권한 보유만으로 upstream push를 승인받았다고 추정하지 않는다. 기존 작업 설정으로 확정할 수 없는 권한·모델·원격 목적지만 blocker로 보고한다.
4. Cursor Cloud가 이용하는 checkout과 Grokbot의 persistent computer를 구분한다. 구현 코드는 Cloud worker checkout에만 두며 HJ 운영 MCP는 worker에 제공하지 않는다.
5. 새 유료 구독·과금 상한 상향·추가 계정 연결은 자동 수행하지 않는다. 기존 사용자가 승인한 사용량·도구 권한 안에서만 실행한다.

### 3.2 Worker 기준선 확인

다음 명령은 조회 예시다. 실제 OS shell에 맞춰 실행하고 각 exit code를 확인한다.

```text
git rev-parse --show-toplevel
git branch --show-current
git rev-parse HEAD
git status --porcelain=v2 --branch
git remote -v
git ls-remote <verified-upstream-remote-or-url> refs/heads/main
```

현재 root와 적용 AGENTS/하위 지침을 확인한다. 문서 작성 기준선에는 root AGENTS가 없었지만 이후 추가될 수 있다. retained evidence를 다시 전부 읽지 말고 현재 변경과 담당 소스만 확인한다.

Cloud checkout의 origin이 downstream fork이면 remote 이름에 의존하지 말고 URL로 upstream과 downstream을 구분한다. 최신 upstream SHA와 현재 checkout을 비교한다. 독립 cloud feature branch에서는 최신 upstream을 확인하고 기준 BASE_SHA를 고정해 시작한다. HJ working tree에 reset/checkout/pull을 수행하지 않는다. 기존 작업이 있는 branch는 덮어쓰지 않으며 이전 stage 완료 HEAD인지 확인한다.

BASE_SHA가 검토 SHA와 달라졌으면 A/B 관련 파일의 delta만 검토하고 실제 경로/시험 이름/이미 구현된 기능을 짧게 기록한다. 새 upstream이 이 계획의 owner 모델을 바꿔 재설계가 필요하면 해당 단계만 중단한다. 사소한 파일 이동·동등 구현은 근거를 남기고 적응한다. 실행 중 main을 매 단계 자동 merge하지 않는다.

### 3.3 환경과 baseline

OS/architecture, `go version`, `dotnet --info`, PowerShell 가용성, Windows runner 가용성을 기록한다. PowerShell 7이 있으면 Windows 작업의 기본 shell로 사용한다. Linux에서 PowerShell이 없다는 이유로 제품에 PowerShell 의존성을 추가하지 않는다.

관련 Go baseline은 우선 `./internal/tool/command/...`, `./internal/activity`, `./internal/app`, `./internal/httpx`, `./internal/mcp`, `./internal/workspace`, `./scripts/test`다. 별도 fixture root만 사용하고 timeout을 둔다. 전체 package 실행이 환경 제약으로 실패하면 정확한 실패·skip을 보존하고 관련 테스트의 독립 실행 결과를 기록한다.

Windows baseline은 기존 `scripts/test/test-windows-activity-center.ps1`과 WPF fixture다. 이 script의 `TestRoot`는 새 디렉터리여야 하며 `BuildRoot`는 source worktree 밖이어야 한다. 실행 전 script가 운영 설치를 건드리지 않는지 확인한다. 새로운 시험은 Windows preference 파일이나 `%LOCALAPPDATA%\AgentDock` 실데이터를 쓰지 않아야 한다.

## 4. P1 — WPF 공통 리소스화

### 4.1 최종 구조

```text
Resources/UiStrings.resx          neutral English
Resources/UiStrings.zh-CN.resx    Simplified Chinese
Resources/UiStrings.ko-KR.resx    Korean (P2)
                |
        existing UiText.Get/Format
                |
      LocExtension / ActivityText facade
                |
     MainWindow / Execution / Activity / Dialogs
```

새 localization framework, 실행 시 번역 서비스, resx 생성·동기화 subsystem, 한국어 전용 창을 만들지 않는다. 모든 고정 UI 문구는 같은 ResourceManager를 사용한다. 기존 neutral key를 일괄 이름 변경하거나 기존 324항목을 무의미하게 재정렬하지 않는다. [S01–S05]

### 4.2 파일별 작업

| 소유 파일 | 수행할 수정 | 보존할 것 |
|---|---|---|
| `Localization/UiText.cs` | 문화권 정규화를 작은 지원 locale 목록으로 정리, P2 추가 지점 확보 | `_resourceCulture` 명시 조회, CurrentCulture 기반 숫자/시간 format, preference 저장 경로 |
| `Localization/ActivityText.cs` | 영·중 dictionary 값과 State/Kind 문구를 resx로 이동 | 기존 Get/State/Kind facade와 ActivityLocExtension 호출 계약 |
| `ExecutionWindow.xaml` | Text/Content/Header/ToolTip/AutomationProperties.Name 등 고정 UI 값을 Loc으로 변경 | x:Name, binding, 이벤트, style, command/action 값 |
| `ExecutionWindow.xaml.cs`, `.Actions.cs` | 메뉴·경고·대화상자·복사 안내·서식 문자열을 UiText로 변경 | 서버 요청·선택 상태·필터·작업 진행 semantics |
| `ExecutionDialogs.cs` | 버튼/권한/설정/입력 안내 리소스화 | 확인 기본값·사용자 취소·권한 변경 방식 |
| `Models/ExecutionModels.cs` | State/Mode/Origin/출력 경고/기본 UI 라벨을 key 조회로 변경 | wire status·id·원문 user title·command/output |
| `Services/ExecutionClient.cs` | 이 client가 자체 생성하는 사용자 표시 오류 리소스화 | SSE protocol token·파서 제한·cursor·재연결 동작 |
| `MainWindow.Access.cs` | 실행 통계 요약을 서식 key로 변경 | 실제 통계와 refresh 처리 |
| `App.Activity.cs`, 기타 기존 UI 소스 | 사용자 표시 literal이 남으면 최소 범위만 리소스화 | runtime-root 및 창 생명주기, 서비스 시작 정책 |

새 key는 기존 key와 충돌하지 않는 `Activity_*`, `Execution_*` prefix를 사용한다. Activity facade는 예를 들어 `Get("NeedsAttention")`를 `UiText.Get("Activity_NeedsAttention")`에 연결할 수 있다. 기존 key를 그대로 호출할 수 있도록 facade에서만 prefix를 붙인다. State/Kind의 code→key 매핑은 유지해도 되지만 code→영어/중국어/한국어 문장 분기는 없어야 한다. 알 수 없는 protocol 상태는 원래 값을 유지한다.

기존 C# 문자열의 placeholder, 줄바꿈, 앰퍼샌드, XAML escaping, trailing space 의미를 보존한다. 문장 조각을 여러 언어 key로 이어 붙이기보다 완전한 문장 format key와 `{0}`, `{1}`를 사용한다. 식별자·`running`, `pending_approval`, `task_id`, action, theme token, JSON key는 번역하지 않는다.

### 4.3 번역 문구에 의존한 분기 제거

기준선 `ExecutionWindow.Actions.cs`의 `DetailsTitle.Text == "连接"`, `.xaml.cs`의 특정 중국어 경고 접두어 판단처럼 표시문구가 제어 흐름에 쓰이는 코드를 찾아 제거한다. 기존 선택 상태나 `terminated_at`, 연결 panel/view의 상태를 사용한다. 부득이하면 해당 Window 안의 작은 `DetailsKind`/warning-kind만 추가한다. Task/Conversation의 병렬 상태 저장소는 만들지 않는다.

`Models/ExecutionModels.cs`의 `"新对话"` 취급은 legacy 데이터 호환으로 분리한다. 제목이 사용자 수동 제목인지 구분할 정형 필드가 있는지 확인하고 manual title을 덮어쓰지 않는다. legacy sentinel을 유지해야 하면 한 곳의 legacy predicate에 이유를 남기고 literal 예외 목록에 포함한다. 운영 journal의 중국어 제목을 일괄 수정하지 않는다.

중국어 glyph `审`처럼 상태 문자를 glyph로 사용하는 부분도 고정 UI 문구다. resource key 또는 언어 중립적인 기존 icon으로 처리하되 새 icon 체계나 레이아웃을 만들지 않는다. 접근성 이름은 언어별로 제공한다.

### 4.4 P1 Gate

중립/중국어 key parity, 중복 key, resource 참조 유효성, format placeholder 검사가 통과해야 한다. 새로운 UI-owned 영·중 문자열의 언어 분기는 없어야 한다. 중국어 literal 검색은 owner 파일의 string literal/XAML 속성 대상으로 한정하고 주석·fixture·wire token·legacy sentinel을 구분한다. 저장소 전체 CJK 문자 금지 같은 시험은 만들지 않는다.

기존 기능 시험에서 번역된 버튼 글자로 control을 찾는 부분은 기존 control name/automation id/resource 기대값으로 바꾼다. 중국어 fixture 사용자 제목이나 Unicode output assertion은 그대로 유지한다. 새 neutral English 화면에서도 기존 기능이 동일해야 한다.

## 5. P2 — 한국어 UI와 installer

### 5.1 Locale 선택의 확정 동작

`UiText`는 최소 다음 계약을 가진다.

| 입력 | 기대 locale |
|---|---|
| preference=system, system=ko-KR 또는 ko | ko-KR |
| preference=ko-KR, system=en-US/zh-CN | ko-KR |
| preference=en, system=ko-KR | en |
| preference=zh-CN, system=ko-KR | zh-CN |
| 기존 zh/zh-CN/zh-SG/zh-Hans 계열 | 기존 zh-CN 매핑 유지 |
| 빈 값·알 수 없는 preference | 기존 system fallback |
| 지원하지 않는 시스템 locale | en |

Trim/대소문자 정규화는 일관되게 처리한다. 지원하지 않는 locale을 임의로 유사 언어에 연결하지 않는다. `CultureInfo.CurrentCulture`까지 한국어로 강제 변경하지 않는다. UI 언어와 숫자·날짜 형식 정책을 혼합하지 않는다. [S01]

`MainWindow.xaml`에 `Tag="ko-KR"`과 `KoreanLanguage` resource를 추가한다. 각 언어에서 해당 항목은 `한국어`처럼 사용자가 인지할 수 있는 자국어 이름으로 표시할 수 있다. 기존 SelectionChanged handler와 창 재생성 경로를 사용한다. 서버 재시작, 서비스 설정 변경, 언어 전용 event handler는 추가하지 않는다. [S05]

### 5.2 번역 범위와 용어

`UiStrings.ko-KR.resx`를 neutral 전체 key 집합에 맞춰 작성한다. 내용 없는 placeholder 번역이나 영어 전체 복사는 완료로 보지 않는다. 제품명, 기술 식별자, 의도적으로 동일한 token만 예외다.

권장 기본 용어는 Conversation=대화, Call=호출, Activity=활동 기록, Task=작업, Thread=실행 분기, Workspace=작업 공간, Permission=권한, pending approval=승인 대기, unknown result=결과 확인 필요다. command session은 명령 세션으로 구분한다. UI 공간 때문에 용어를 바꾸면 같은 개념에 일관되게 적용한다.

승인·삭제·취소·작업 재개 안내는 효과를 정확히 번역한다. 특히 상태 복원이 모델 자동 실행을 뜻하는 것처럼 번역하지 않는다. Call/Task 기록 삭제가 프로젝트 소스 삭제처럼 보이게 만들지 않는다. `command_ok=false`, timeout, 결과 미확인 안내를 성공 문구로 번역하지 않는다.

### 5.3 Fallback과 시험 방법

한국어 satellite에 key가 없으면 neutral English로 fallback한다. neutral에도 key가 없으면 기존 `UiText.Get`의 key 문자열 반환을 유지한다. 임의 catch로 전부 빈 문자열을 반환하지 않는다. 명시적 `_resourceCulture`를 유지한다.

production ko-KR의 엄격 parity와 누락 key fallback 시험은 모순이 아니다. fallback 시험은 test-only neutral/satellite fixture resource 또는 기존 ResourceManager의 테스트 fixture로 재현한다. production 번역을 일부러 누락하거나 테스트 중 실제 resx를 수정하지 않는다.

문화권 resolve/lookup 시험은 순수 helper 및 메모리 내 culture 설정을 사용한다. 실제 `SetPreference()`를 호출하여 사용자의 언어 preference를 쓰는 시험은 금지한다. persistence 시험이 필요하면 새 fixture profile/root에 한정되는 seam을 좁게 분리한다. 제품에 범용 설정 주입 framework를 추가하지 않는다.

### 5.4 작은 자동 검사

`./scripts/test`의 기존 Go 정적 검사 방식을 확장하는 신규 `localization_resources_test.go`를 우선 고려한다. `encoding/xml`로 resx의 `<data name>`을 읽는다. key 중복, neutral/zh-CN/ko-KR key 차이, 빈 번역, format placeholder index 차이를 검사한다. 원문이 의도적으로 빈 key는 neutral을 기준으로 허용한다.

.NET format의 escaped braces와 alignment/format suffix를 처리한다. 정규식 한 줄로 format 전체를 검증했다고 주장하지 않는다. 실제 C# fixture에서는 대표 format 문자열이 올바르게 format되는지도 확인한다. 운영 소스에서 쓰는 literal key 참조와 XAML Loc key 누락을 검사하고, dynamic key는 해당 State/Kind mapping을 별도 검증한다.

기존 코드 내 영·중 리터럴이 다시 추가되면 지정한 UI owner 범위에서만 실패하는 작은 검사를 둔다. 예외가 필요하면 항목·파일·이유를 명시한다. 새 key가 추가될 때 한국어 번역자가 원칙적으로 수정할 파일은 ko-KR resx 하나다. upstream이 새 literal을 직접 넣었을 때는 검사가 알려주고 해당 owner만 리소스화한다.

### 5.5 Installer의 확정 범위

`AgentDock.iss`의 `[Languages]`에 `korean`을 추가한다. 기준선의 `UsePreviousLanguage=no`, `LanguageDetectionMethod=uilanguage`, `ShowLanguageDialog=no`는 유지한다. 표준 메시지는 Inno compiler 버전과 호환되는 `languages/Korean.isl`을 사용하고 출처·버전·라이선스를 짧게 기록한다. 존재 여부가 확인되지 않은 `compiler:Languages\Korean.isl` 경로를 가정하지 않는다. 기존 방식에 맞춰 Default.isl 기반 fallback을 유지한다.

`includes/messages.iss`에 기존 54 key와 대응하는 `korean.<Key>` 항목을 추가한다. 전체 installer 메시지 저장 체계를 재구성하지 않는다. custom message key 집합 및 `%1`, `%n`, `[name]` 등 Inno placeholder 보존을 검사한다. WPF의 locale code `ko-KR`와 Inno language name `korean`을 혼동하지 않는다. WPF preference를 installer에서 쓰거나 덮어쓰지 않는다. [S06]

installer language 파일 번역 때문에 install/uninstall PowerShell의 제품 동작을 수정하지 않는다. 한국어 shortcut 이름은 기존 CustomMessage 경로로만 처리한다. 다국어 shortcut의 과거 잔여 파일 정리 등 독립적인 installer 재설계는 제외한다.

### 5.6 현지화 수용 시험

| ID | 시험 | 기대 결과 |
|---|---|---|
| L01 | 세 locale key 집합·중복·빈 값 검사 | neutral/zh-CN/ko-KR parity 및 유효 값 |
| L02 | locale resolve 표 전체 | 지정 표와 일치, 기존 zh 매핑 유지 |
| L03 | 명시적 en/zh-CN/ko-KR가 system override | 선택한 언어 유지 |
| L04 | test-only 누락 satellite key | 영어 fallback, key fallback도 구분 |
| L05 | format index/escaped braces/대표 runtime formatting | 누락 인수·FormatException 없음 |
| L06 | Activity Get/State/Kind/legacy Activity XAML | 한 ResourceManager 경로, 알 수 없는 상태 원문 유지 |
| L07 | Execution 메뉴·Dialog·Models·SSE 오류·Main 요약 | UI-owned 고정 문구가 선택 언어 사용 |
| L08 | 언어 변경 후 창 재생성·async refresh | 이전 culture로 회귀하지 않음, 서비스 재시작 없음 |
| L09 | 버튼·warning 분기 언어 변경 | 문자열 자체로 동작을 판단하지 않음 |
| L10 | 사용자 중국어/한국어 제목·command·output fixture | 원문 불변, wire/action key 불변 |
| L11 | 접근성 이름·한국어 glyph·DPI 레이아웃 | 읽을 수 있고 필수 버튼·경고가 잘리지 않음 |
| L12 | installer 언어 등록·54 key·Inno placeholder | compile 가능한 리소스, 누락 없음 |
| L13 | single-file publish한 exe에서 한국어 로드 | 개발 bin이 아닌 배포 exe에서도 satellite 사용 |
| L14 | 기존 영어·중국어 및 source literal 검사 | 기존 번역 지원 유지, 새 UI literal 누락 감지 |

실제 Windows WPF 시험은 기존 activity-center harness를 확장한다. 새 `LocalizationCases.cs` 같은 파일을 추가할 수 있다. UI fixture 자체는 test-only HTTP service를 사용하고 운영 Core를 띄우지 않는다. 화면 검증은 현재 harness의 light/dark, 기존 100/125/150/200% DPI 범위를 재사용한다. 외부 번역 서비스나 ERA 테스트 harness는 가져오지 않는다.

## 6. P3 — 공개 비소비 peek

### 6.1 Wire 계약

기존 canonical tool 이름을 유지한다. `session_observe.action`에 `peek`를 추가한다. P3는 session_id로 동작하고 P4에서 execution_request_id를 연결한다. 최종 입력 계약은 다음과 같다.

| 필드 | 최종 조건 |
|---|---|
| action | list / status / peek. 생략은 기존 list |
| session_id | status에서는 필수. peek에서는 두 selector 중 정확히 하나 |
| execution_request_id | peek에서 session_id 대신 사용. 두 selector 동시 전달은 INVALID_ARGUMENT |
| stdout_offset / stderr_offset | peek 전용, 기본 0, 원본 byte absolute offset. 음수·분수·2^53-1 초과 거절 |
| max_output_bytes | 기존 기본 65536·상한 4194304 유지. peek에서는 최소 4, legacy status에서는 기존 최소 1 유지 |

peek 최소 4 bytes는 UTF-8 한 code point를 반환할 수 있도록 하는 신규 action의 제한이다. action별 조건을 공개 JSON Schema와 typed request validation에 같이 반영한다. peek 필드를 list/status에 섞어 의미 없이 무시하지 않는다. 전체 schema를 `additionalProperties=true`로 풀지 않는다. [S08, S19]

최종 결과에 각 stream의 `stdout/stderr`, `*_next_offset`, `*_retained_from`, `*_total_bytes`, `*_gap`, `*_truncated`, `*_pending_utf8`, `*_invalid_utf8`를 제공한다. status·session_id·runtime/workdir·완료 exit_code/command_ok/timed_out은 기존 Session에서 가져온다. 존재하지 않거나 아직 실행되지 않은 결과에 command_ok=true를 넣지 않는다.

### 6.2 Byte/cursor 알고리즘

기존 Session의 buffer, total byte count, dropped byte count와 mutex를 사용한다. observer별 서버 registry나 output 복사 저장소를 만들지 않는다. 클라이언트가 offset을 보관한다. `OutputCursor`의 개념과 기존 ring eviction 계산을 재사용하되 공개 함수는 caller cursor를 input/output 값으로 취급한다. [S11]

한 stream에서 요청 offset을 A, 보존 시작을 R, 관찰 순간 total을 T라고 할 때 다음을 적용한다.

```text
A > T             -> INVALID_OUTPUT_CURSOR
start             = max(A, R)
gap               = A < R
returned raw span = [start, next_offset)
next_offset       <= T
more output       = next_offset < T (미완성 UTF-8 tail은 pending_utf8로 구분)
```

같은 mutex snapshot에서 상태와 두 stream의 range를 얻는다. 실제 반환한 원본 byte 구간만큼 next_offset을 이동한다. 출력 제한을 걸어놓고 total까지 cursor를 이동시키지 않는다. byte 수와 .NET/JSON 표시 문자열의 글자 수를 혼동하지 않는다. legacy 공용 `stdoutCursor/stderrCursor`를 읽거나 변경하지 않는다.

UTF-8은 완전한 code point 단위로 출력 budget 안에서 prefix를 반환한다. 잘못된 raw byte는 표시용 replacement 문자로 변환하고 `invalid_utf8=true`로 알린다. next_offset은 replacement 문자열 길이가 아니라 소비한 원본 byte 수로 계산한다. budget은 반환되는 표시 문자열의 UTF-8 bytes 상한으로 유지한다. 실행 중인 stream 끝의 미완성 UTF-8 sequence는 다음 쓰기까지 보류하고 `pending_utf8=true`로 표시한다. 완료된 stream의 불완전 tail은 replacement와 invalid flag로 마무리한다. eviction 또는 요청 offset 때문에 code point 중간에서 시작할 수 있음을 시험한다. binary 원문을 무손실 다운로드하는 기능은 이 API의 목표가 아니다.

stdout과 stderr는 기존 계약처럼 각각 max_output_bytes를 적용한다. `gap`은 ring eviction으로 필요한 과거 byte가 사라진 경우이고 `truncated`는 이번 출력 제한으로 보존 중인 나머지를 아직 반환하지 않은 경우다. 둘을 같은 boolean으로 뭉개지 않는다.

### 6.3 권장 소스 위치

`internal/tool/command/session/peek.go`에 read-only absolute snapshot helper를 추가할 수 있다. 기존 `Session.Peek(status,maxBytes)`는 mutation tool이 쓰므로 함부로 같은 이름/semantics로 대체하지 않는다. 새 helper는 `ReadOutputAt` 등 구분 가능한 이름을 사용한다. ActivitySnapshot도 같은 낮은 range helper를 재사용할 수 있으나 redaction 정책과 기존 cursor 흐름을 바꾸지 않는다.

`command/request.go`, `contract.go`, `actions.go`에 신규 action을 연결하고 `command/peek.go`에 service adapter를 둔다. `specs_command.go`의 observer handler는 필요한 경우 ctx를 유지하는 Runtime adapter로 연결한다. 공개 descriptor와 실제 입력 검사·typed request는 같은 source of truth를 유지한다. `contract_drift_test.go`, output contract coverage의 영향도 확인한다.

### 6.4 Ownership

session_id로 찾는 peek도 기존 `validateSessionOwnership`을 반드시 통과한다. SourceOwnerKey 불일치는 거절한다. 원래 Conversation이 있으면 `conversations.Owns`와 같은 Conversation 또는 허용된 동일 Task 조건을 적용한다. Task/Thread binding을 현재 default로 바꾸지 않는다. Conversation gate 및 기존 Permission 판정도 유지한다. [S13–S14]

P3에서 일반 legacy session에 peek를 추가하더라도 다른 legacy status가 완료 세션을 소비하면 나중에 SESSION_NOT_FOUND가 될 수 있다. 이를 숨기지 않는다. P4의 receipt 대상 Session만 완료 소비로부터 추가 보존한다. 모든 legacy 세션의 수명을 조용히 바꾸는 패치는 만들지 않는다.

## 7. P4 — Request receipt 확정 설계

### 7.1 보장과 비보장

보장은 같은 Runtime epoch, 같은 인증 owner, 같은 execution_request_id에서 프로세스 시작 시도를 중복하지 않는 것이다. 동일 ID를 재전송하면 원래 Call/Session을 조회한다. 새 명령으로 retry하지 않는다. 이것은 임의 shell command의 외부 effect까지 exactly-once로 만드는 보장이 아니다.

명시적 새 실행은 반드시 새 ID다. 같은 명령 문자열 + 새 ID는 다시 실행할 수 있다. MCP JSON-RPC id, CallID, retry_of_call_id, SessionID는 request ID와 서로 다르다. 검토에 사용한 ERA 연결의 request_id wire 계약을 복사하지 않는다. [S08–S18]

### 7.2 execution_request_id와 epoch

`exec_command`에 optional string `execution_request_id`를 추가한다. 생략하면 legacy 동작을 유지한다. 이름 alias로 `request_id`를 추가하거나 모든 도구에 ID를 강제하지 않는다.

v1 형식은 `<epoch32hex>.<nonce32hex>`이며 정규식은 `^[a-f0-9]{32}\.[a-f0-9]{32}$`, 길이는 65다. nonce는 클라이언트가 의도한 실행마다 새로 생성한 임의 128-bit 값이다. 같은 attempt의 통신 복구에는 같은 값을 유지한다.

Runtime의 기존 `executionInstance`가 `call_` prefix를 가진 임의 128-bit 값이므로 이를 재사용해 `execution_epoch`를 만들 수 있다. 별도 persistent device identity나 새 epoch 파일은 만들지 않는다. epoch는 Runtime과 command Store 수명이 같다는 전제에 묶인다. Store를 독립 재생성하는 경로가 있다면 이전 epoch를 계속 수락하면 안 되므로 P0에서 확인한다. [S15]

`agentdock_context.runtime`에 `execution_epoch` 및 작은 `command_recovery` capability를 추가한다. 권장 내용은 version=1, request_id_field=execution_request_id, deduplication_scope=runtime_epoch, peek=true, durable=false다. 이 값은 transport-owned 장치 target이 아니라 복구 capability다. context를 매 명령마다 다시 호출할 필요는 없다. epoch를 잃은 클라이언트는 기존 ID로 조회한 뒤, mismatch가 나면 자동 재실행 없이 중단한다.

### 7.3 Digest의 확정 의미

첫 claim 전에 공개 입력 전체를 검증하고 JSON-compatible clone을 만든다. digest는 `SHA-256("agentdock.exec.receipt.v1\n" + canonical-json(arguments-without-execution_request_id))`로 계산한다. Go의 표준 JSON encoding을 이용하여 object key 순서를 안정화하고 동일 map 순서 차이는 허용한다. typed request로 먼저 축소하여 아직 처리하지 않은 필드를 잃지 않는다.

이 v1은 의도적으로 엄격하다. `cmd`, env, stdin, workdir, runtime, skill, timeout, mode, yield, max_output_bytes, 명시적 task/thread/workspace/step/retry/label 등 ID 이외의 모든 공개 필드가 digest에 포함된다. `cmd` 공백을 trim하거나 env를 redaction 후 hash하지 않는다. 생략과 명시적 default를 같은 요청으로 normalize하는 고급 동치 처리는 하지 않는다. 같은 JSON 의미의 숫자는 기존 map decode 표현을 기준으로 직렬화한다. 출력 크기만 바꿔 조회하려면 exec를 재전송하지 말고 peek를 사용한다.

동일 raw 요청을 재전송했을 때 변경된 시스템 env/default task/workspace를 다시 해석하지 않는다. 원래 해석 결과는 기존 preparedExecution/Session과 첫 receipt binding snapshot을 사용한다. 새 환경에서 실행하려는 의도는 새 ID다. digest나 원본 env/stdin을 journal·error·UI에 노출하지 않는다. digest는 보안 credential도 아니다.

### 7.4 정본과 최소 index

claim owner는 `internal/app`의 기존 실행 ingress다. `internal/app/command_receipts.go`와 Runtime의 작은 map을 권장한다. key는 `(SourceOwnerKey, execution_request_id)`이고 epoch는 ID 안에 있다. 다른 owner의 같은 nonce는 같은 명령이 아니다.

entry에는 digest, 원래 CallID, 초기/확정 binding snapshot, claim 시각과 초기 준비 완료 여부 정도만 둔다. 승인·실행 상태의 정본은 Permission/Call, 프로세스·출력의 정본은 기존 Session이다. 전체 request/result/stdout/stderr를 entry에 복사하지 않는다. 승인용 fixed request는 기존 pendingCalls가 계속 소유한다.

SessionID 연결은 기존 Session Store를 CallID로 찾아도 된다. 최대 retained Session 수가 제한되어 있으므로 `command.Service.SessionForCall(callID)` 같은 bounded scan을 우선 고려한다. 별도 Session 복사나 두 번째 process store를 만들지 않는다. pending 상태에서는 원래 Call의 ApprovalID를 읽는다. Session이 없고 초기화도 끝나지 않았으면 `preparing`, 초기화가 끝났지만 결과 자료가 사라졌으면 `unknown` 또는 명시적인 unavailable 응답을 사용한다. 실제 상태를 추측해 성공으로 만들지 않는다.

`r.executionMu`로 index claim을 원자화하는 것이 우선이다. 새 claim 관련 critical section 안에서 process start, 파일 I/O, journal 조회, permission 대기, 다른 mutex를 잡은 callback을 수행하지 않는다. 기존 lock을 재사용하기 어렵다는 근거가 있을 때만 receipt 전용 작은 mutex를 추가하고 lock order를 시험으로 확인한다.

### 7.5 Ingress 순서와 CallID

receipt 없는 호출은 기존 `callObserved` 경로를 바꾸지 않는다. receipt 호출은 다음 순서로 처리한다.

```text
인증된 transport ctx
  -> trusted source와 현재 scope 확인
  -> 공개 입력/ID/epoch 검증, immutable args clone
  -> owner-scoped existing receipt lookup
       hit: 원래 binding의 접근권한 + digest 검사 -> 조회 반환; dispatch 없음
       miss: 새 CallID와 digest를 atomic insert -> 이 caller만 최초 claim 소유
  -> 최초 claim만 기존 call.created / call.bound / Permission 경로
  -> 기존 fixed request 승인 및 재검사
  -> 기존 executePrepared -> command.Service.Exec
  -> 기존 Session에 원래 CallID 고정, 필요 시 조회
```

claim은 Permission 승인 자체가 아니다. Permission/Conversation gate를 우회하지 않는다. 하지만 claim을 command 함수 안에만 두면 동일 요청의 pending approval이 여러 개 만들어질 수 있으므로 승인 요청 생성 전 ingress에서 claim해야 한다.

기존 callObserved의 CallID 생성부를 작은 helper로 분리하여 최초 claim에서 예약한 CallID를 사용한다. 모델이 call_id를 공급하는 새 입력은 만들지 않는다. 성공한 동일-ID 재전송은 새 exec Call이나 새 approval/command.started를 만들지 않고 원래 CallID를 반환하는 receipt 조회로 처리한다. 잘못된 인수, conflict, owner/gate 거절은 기존 rejection audit 경로를 이용하며 필요하면 별도 거절 attempt의 Call을 기록한다. 원래 Call을 failed로 덮어쓰지 않는다.

원래 binding은 prepareObservedExecution 이후 확정되고 바뀌지 않는다. 준비 도중 조회는 초기 trusted owner/Conversation만 사용하며, Task가 아직 확정되지 않았는데 cross-conversation Task 예외를 미리 적용하지 않는다. 동시에 들어온 동일 요청은 준비 중임을 즉시 반환할 수 있으며 mutex를 잡고 원래 명령 완료까지 기다리지 않는다.

이미 있는 receipt에 대한 exec 재전송은 기존 session_observe에 해당하는 읽기 권한으로 재검사한다. 원래 명령의 쓰기 Permission을 다시 결정하여 새 approval을 만들지 않는다. 명시적인 관찰 금지 또는 Conversation gate가 있으면 조회도 거절하고 원래 실행은 바꾸지 않는다. 이 읽기 검사는 임의 로컬 인증·local-management privilege를 주입하지 않고 기존 policy owner를 사용한다.

### 7.6 Claim 이후 실패와 수명주기

claim 이후 발생한 검증 실패·Permission deny·승인 reject/expire·journal 실패·프로세스 시작 실패에서도 같은 ID를 삭제하여 다시 실행 가능하게 만들지 않는다. 최초 요청의 error를 기존 Call에 기록하고 가능한 경우 receipt 참조를 error details에 넣는다. 기록 자체가 실패하면 receipt는 남기고 상태를 unknown/unavailable로 보수적으로 반환한다.

프로세스 시작 후 stdin write/close 오류는 특히 중요하다. 시작된 Session을 먼저 안전하게 등록·binding한 뒤 오류를 처리해야 한다. 오류 반환 전에 claim을 제거하거나 새로운 ID로 자동 replay하지 않는다. 원래 프로세스가 이미 effect를 냈을 수 있다.

기존 `RuntimeApprovalDecision`, expirePendingApprovals, Conversation termination, Runtime close와 복구 경로를 사용한다. receipt 때문에 승인·취소·Task progress의 병렬 상태 머신을 만들지 않는다. UI에서 승인된 동일 fixed request의 dispatch는 기존 Permission claim으로 한 번만 이루어져야 한다. [S13–S15]

### 7.7 Receipt 대상 Session 보존

`ExecRequest.ExecutionRequestID`를 command 계층으로 전달한다. Session에는 private recoverable marker/request ID를 둘 수 있다. 전역 Activity.Binding schema까지 새 request ID를 전파하는 변경은 v1에 필요하지 않다. CallID는 이미 binding에 있으므로 이를 사용한다.

receipt 요청은 `invocation.start`가 반환한 뒤 execution context와 activity binding을 설정하고, stdin 처리·foreground wait·결과 반환 전에 기존 Session Store에 한 번 등록한다. fast sync/auto도 보존한다. process start와 Session 등록 사이에는 receipt가 이미 claim되어 있어야 한다.

`TryReserve`, `FinishStart`, `AddReserved`, `ReleaseReservation`, Runtime Close의 기존 자원 수명을 유지한다. 조기 등록한 receipt Session을 async 반환 시 다시 AddReserved하지 않는다. reservationActive 플래그를 일관되게 정리하고 double release, 시작 창 누락, close deadlock이 없어야 한다. runner/OS process controller가 완성되기 전 FinishStart를 호출하지 않는다. [S09, S11]

다음 계약을 공통 소비 helper에 구현한다.

| 경로 | legacy Session | receipt Session |
|---|---|---|
| running status | 기존 공용 incremental cursor | 기존 공용 incremental cursor; peek에는 영향 없음 |
| completed status | 기존 결과 반환 후 삭제 | 결과 반환하되 Store에서 소비 삭제하지 않음 |
| 완료된 write/kill 및 kill_all | 기존 소비/삭제 | 기존 실제 종료 결과를 유지하고 recovery용 Session은 보존 |
| peek | 보존 중인 raw range 비소비 조회 | 동일, 완료 후 반복 가능 |
| output TTL/capacity prune | 기존 정리 | 동일한 정리 한계 적용, receipt claim은 유지 |
| Runtime close | 기존 process 정지/배수 처리 | 보존 때문에 close가 멈추지 않음; 새 Runtime은 새 epoch |

현재 4 MiB/stream buffer, 1시간 완료 보존, 최대 32 동시 실행, 최대 128 retained Session을 기본 유지한다. 새 메모리 buffer를 더 만들거나 기록 전체를 영구 저장하지 않는다. 1시간은 용량 경쟁·명시적 수명주기에 의해 단축될 수 있는 보존 상한/정리 기준이며 최소 보존 SLA가 아니다. 시험은 시간을 길게 sleep하지 말고 fixture clock/completion time과 작은 test limit을 사용한다.

### 7.8 Receipt metadata 보존 한계

v1 기본은 Runtime 전체 최대 16,384개, 인증 owner별 최대 4,096개 claim metadata다. 값은 내부 상수로 두고 시험에서는 작은 limit을 주입할 수 있게 한다. 초기화에 거대한 배열을 할당하지 않는다. 실제 entry 크기·대표 사용량을 P4 결과에 기록하되 추측 메모리 수치로 통과시키지 않는다.

한도에 도달하면 새 ID는 process/approval 생성 전에 `EXECUTION_RECEIPT_LIMIT`으로 거절한다. 기존 ID lookup/retransmission은 계속 허용한다. LRU/TTL로 기존 claim을 지워 같은 epoch의 ID가 다시 실행되게 하지 않는다. 출력 prune와 UI의 Call 기록 삭제도 claim을 지우지 않는다. quota 오류를 받은 client가 ID를 생략하거나 새 epoch를 얻기 위해 서버를 자동 재시작하도록 안내하지 않는다.

장시간 uptime에서 quota가 소진될 수 있다는 제한을 사용자 문서에 적는다. 이번 범위는 durable receipt DB나 무제한 exactly-once가 아니다. 이 제약을 없애야 한다는 요구가 생기면 별도 설계로 분리한다.

### 7.9 Peek by request ID와 owner 검증

app-layer adapter가 `execution_request_id`를 owner-scoped receipt로 먼저 해석한다. receipt의 원래 binding에 대해 기존 Session ownership과 동일한 helper를 호출한 뒤 Session/Call을 찾는다. `validateSessionOwnership`가 session_id가 없다는 이유로 early-return하는 경로를 그대로 사용하여 우회하지 않는다.

다른 authenticated owner가 ID를 알아도 원래 CallID/SessionID/상태/output을 알 수 없어야 한다. owner-scoped lookup miss는 `EXECUTION_REQUEST_NOT_FOUND`로 처리하여 다른 owner의 존재를 노출하지 않는다. 같은 owner의 다른 Conversation도 기존과 같이 같은 Conversation 또는 명시적으로 소유 작업을 resume한 동일 Task 조건이 필요하다. Thread/workspace를 현재 값으로 재binding하지 않는다. unattributed source의 기존 owner 범위를 임의 global Conversation으로 확장하지 않는다.

Session output이 prune됐으면 receipt와 Call 참조는 남긴다. `output_unavailable=true`를 반환하고 stdout/stderr가 비어 있다는 것을 실제 출력이 없었다는 뜻으로 표시하지 않는다. Call 이력까지 사라졌으면 `status=unknown`, `history_unavailable=true`와 원래 식별 정보만 반환할 수 있다. `command_ok`, `exit_code`를 추정하지 않는다. 보존 중인 Session이 있으면 process 상태는 Session에서 직접 읽는다.

### 7.10 결과 envelope

receipt 요청의 첫 exec와 동일-ID exec 재조회는 원래 exec의 call_id를 사용한다. 일반적인 session_observe peek 자체는 새 관찰 Call이므로 top-level call_id는 관찰 CallID이며 `receipt.call_id`가 원래 exec다. 기존 `decorateExecution`의 top-level binding 장식을 깨지 않는다. [S13]

권장 receipt 최소 필드는 version, execution_request_id, execution_epoch, call_id, session_id(있을 때), approval_id(있을 때), deduplication_scope=runtime_epoch다. status/exit/output은 기존 Call/Session에서 계산한다. `new_execution_started`는 이번 요청에 의해 실제 OS 프로세스가 새로 시작되었는지 아는 경우에만 반환한다. 중복 조회에는 false이고 pending/preparing에도 false다. 기존 실행의 최종 성공 여부와 혼동하는 top-level `ok=true`를 새로 만들지 않는다.

예시 모양은 아래와 같다. angle-bracket 값은 설명용이며 실제 계약의 유효 ID로 바꿔서 시험한다.

```json
{
  "call_id": "<이번 관찰 CallID>",
  "status": "exited",
  "session_id": "<원래 SessionID>",
  "execution_epoch": "<32 hex>",
  "receipt": {
    "version": 1,
    "execution_request_id": "<epoch32hex>.<nonce32hex>",
    "call_id": "<원래 exec CallID>",
    "session_id": "<원래 SessionID>",
    "deduplication_scope": "runtime_epoch"
  },
  "new_execution_started": false,
  "command_ok": true,
  "exit_code": 0,
  "stdout": "done\n",
  "stdout_next_offset": 5,
  "stdout_retained_from": 0,
  "stdout_total_bytes": 5,
  "stdout_gap": false,
  "stdout_truncated": false,
  "stderr": "",
  "stderr_next_offset": 0,
  "stderr_retained_from": 0,
  "stderr_total_bytes": 0,
  "stderr_gap": false,
  "stderr_truncated": false
}
```

P4의 error code는 최소 다음을 정의한다. 기존 ToolError/category/envelope를 사용한다.

| code | 의미 | 자동 동작 금지 |
|---|---|---|
| EXECUTION_REQUEST_CONFLICT | 같은 owner/ID, 다른 digest | 원래 요청 덮어쓰기·새 실행 |
| EXECUTION_EPOCH_MISMATCH | 현재 Runtime의 epoch가 아님 | 새로운 epoch/ID로 자동 replay |
| EXECUTION_REQUEST_NOT_FOUND | 현재 owner/epoch에서 claim 확인 불가 | 조회가 명령 시작으로 바뀌는 동작 |
| EXECUTION_RECEIPT_LIMIT | metadata 한도 | 오래된 ID 삭제·ID 생략 우회 |
| INVALID_OUTPUT_CURSOR | 음수/미래/표현 범위/형식 오류 | total로 조용히 clamp하여 데이터 유실 숨기기 |
| 기존 SESSION_OWNER_MISMATCH / SESSION_CONVERSATION_MISMATCH | 원래 Session/receipt 접근권한 없음 | 현재 task로 강제 이동 |

current epoch에서 request lookup miss는 서버가 이 ID의 실행 claim을 보유하지 않는다는 뜻이지 외부 effect 전체가 없었다는 보장은 아니다. 자동 replay 기능은 구현하지 않는다. 사용자가 원래 실행 의도를 유지하여 동일 ID의 exec를 다시 보낼 때만 정상 claim 경로가 진행된다.

### 7.11 Guidance와 discovery

기존 `execution_guidance.go`의 running 안내를 확장한다. receipt가 있는 exec의 후속 관찰은 request ID 기반 peek와 cursor 사용을 안내한다. legacy exec의 status 안내는 유지한다. 새 guidance protocol이나 필수 사전 admission 절차를 만들지 않는다.

기존 context bootstrap에 짧게 다음만 설명한다: 의도한 실행마다 새 ID, 응답 유실이면 동일 ID로 peek, epoch mismatch나 unknown이면 실제 effect를 확인하기 전 새 실행 금지. stdout·외부 MCP 내용에서 instructions를 가져와 guidance로 승격하지 않는다.

## 8. P3–P5 수용 시험 행렬

아래 케이스는 production helper 내부 시험뿐 아니라 표시된 경계에서 검증한다. 새 시험 이름은 설명적이어야 하며 legacy 시험을 덮어써 요구 semantics를 지우지 않는다.

| ID | 경계·케이스 | 필수 assertion |
|---|---|---|
| R01 | Session: 같은 offset으로 두 번 peek | 상태·출력 동일 범위, 공유 cursor 불변 |
| R02 | Session: observer A/B 독립 offset | 각자 이어 읽기, output steal 없음 |
| R03 | Session: output cap 여러 page | 연결 결과가 보존 byte 범위를 정확히 복구, total로 건너뛰지 않음 |
| R04 | Session: ring eviction | retained_from/gap 정확, 사라진 부분 숨기지 않음 |
| R05 | Session: 한국어·emoji·fragmented write | 유효 UTF-8, raw offset 정확, 미완성 tail pending |
| R06 | Session: invalid byte·중간 code point offset·완료 tail | replacement/invalid flag, 진행 가능, panic 없음 |
| R07 | Service/API: invalid offset·max 1~3·두 selector | peek는 명확히 거절, legacy max 1 지원은 유지 |
| R08 | legacy list/status/write/kill/kill_all | 기존 소비 assertion 유지 |
| R09 | receipt Session: status 후 다른 peek | 완료 output 재조회 가능, 공용 cursor와 독립 |
| R10 | receipt Session: write/kill/kill_all 후 peek | 실제 종료 상태·출력 보존, 소비 삭제 없음 |
| R11 | app: 같은 ID 32개 동시 요청 | start counter=1, 원래 CallID/SessionID 동일, 새 approval 중복 없음 |
| R12 | app: 같은 ID 다른 cmd/env/stdin/workdir/옵션 | conflict, 원래 process/Call 불변, secret digest 미노출 |
| R13 | app: object key 순서만 다름 | 같은 digest, 재실행 없음 |
| R14 | app: 같은 cmd 새 ID | 의도한 별도 실행 두 번 허용 |
| R15 | sync/빠른 auto의 전체 응답 유실 | ID로 원래 완료 Session과 output 복구, start=1 |
| R16 | async/running 응답 유실·client cancel | 원래 session 계속 관찰, 중복 start 없음 |
| R17 | pending 승인 응답 유실·동시 같은 ID | original approval 1개, 승인 전 start=0, 승인 후 start=1 |
| R18 | 승인 reject/expire/Permission deny 후 같은 ID | 새 approval/start 없음, 원래 기록·식별 보존 |
| R19 | 준비 중 task/thread/workspace 변경 | 원래 scope 고정, 조회를 이유로 새 default에 실행하지 않음 |
| R20 | 다른 principal 및 forged host metadata | request ID를 알아도 output/원래 CallID 유출 없음 |
| R21 | 같은 owner 다른 Conversation, 동일 Task resume 전/후 | 기존 Session ownership과 같은 허용/거절 |
| R22 | no-host-metadata/unattributed | 가짜 global Conversation 미생성, owner 범위 유지 |
| R23 | journal append 실패·start 실패·stdin 오류 | claim 유지, 자동 replay 없음, unknown/오류 정직 보고 |
| R24 | output TTL/capacity prune | output_unavailable, 동일 ID start 없음 |
| R25 | Activity/Call retention·삭제 | claim 재사용 금지, 필요 시 history_unavailable |
| R26 | 작은 metadata quota 고갈 | 기존 lookup 성공, 새 ID effect 전 거절, LRU 삭제 없음 |
| R27 | runtime epoch 재생성 | 이전 ID mismatch, 재시작 전 unknown 상태 자동 replay 없음 |
| R28 | close 중 receipt 초기화/Session 시작/관찰 경합 | reservation 누수·deadlock·double start 없음 |
| R29 | public MCP schema/result/errors | closed schema, typed contract 일치, original vs observer CallID 구분 |
| R30 | retry_of_call_id | 기존 confirmed-terminal 새 retry 의미 유지, unknown retry 계속 거절 |
| R31 | Permission/Conversation gate가 바뀐 뒤 조회 | 관찰 권한 유지, 기존 실행을 재승인·재dispatch하지 않음 |
| R32 | 관련 packages race 검사 | data race 없음, Session/entry snapshot aliasing 없음 |

시험 fixture의 프로세스 effect는 임시 디렉터리의 counter append 같은 관찰 가능한 한 가지 동작으로 한정한다. 동시성/timeout은 채널 barrier를 우선 사용하고 긴 sleep이나 무한 polling으로 통과시키지 않는다.

### 8.1 실제 transport 응답 유실 시험

P5에서는 `internal/httpx`의 기존 MCP integration fixture를 확장한다. 실제 SDK → HTTP handler → Runtime → command runner 경로를 통과시킨다. 서버가 처리한 첫 tools/call 응답을 test proxy/transport에서 버리거나 연결을 끊는다. 이후 새 JSON-RPC id의 요청으로 같은 execution_request_id를 peek한다. 단순히 Go 변수에 반환값을 대입하지 않는 시험만으로 HTTP 응답 유실 검증을 끝내지 않는다.

빠른 sync 완료 후 응답이 사라지는 경우와 프로세스가 실행 중일 때 연결이 끊기는 경우를 분리한다. SDK request cancellation propagation과 command lifetime 분리도 확인한다. 외부 ChatGPT 서비스의 자동 retry 동작까지 검증했다고 주장하지 않는다. 네트워크 단절은 격리 fixture 내부에서만 만들고 운영 tunnel을 끊지 않는다.

## 9. P6 — 시험 실행·빌드·완료 증거

### 9.1 Portable 명령 예시

각 명령은 현재 repo의 toolchain과 테스트 가용성을 확인한 뒤 사용한다. 아래 명령을 실행했다고 문서만으로 체크하지 않는다.

```sh
go test -count=1 ./internal/tool/command/...
go test -count=1 ./internal/activity ./internal/workspace ./internal/app
go test -count=1 ./internal/httpx ./internal/mcp
go test -count=1 ./scripts/test
go test -race -count=1 ./internal/tool/command/... ./internal/activity ./internal/app ./internal/httpx ./internal/mcp
git diff --check
```

`go test ./...`는 격리 환경에서 실행 가능한 경우 최종 전체 회귀로 수행한다. 외부 서비스 의존·OS 전용·네트워크 제약 때문에 못한 항목은 구체적으로 분리한다. `-race` toolchain을 실행할 수 없는 환경을 race pass로 쓰지 않는다. Go 1.26.5를 만족하지 못하면 go.mod를 낮추지 말고 격리 toolchain을 준비한다.

### 9.2 Windows native fixture

이미 승인된 격리 Windows 환경에서 PowerShell 7로 다음 형태를 사용한다.

```powershell
$testRoot = Join-Path $env:TEMP ('agentdock-ko-recovery-' + [guid]::NewGuid().ToString('N'))
$buildRoot = $testRoot + '-build'
& ./scripts/test/test-windows-activity-center.ps1 -TestRoot $testRoot -BuildRoot $buildRoot -RuntimeIdentifier win-x64
if ($LASTEXITCODE -ne 0) { throw 'Windows activity fixture failed.' }
```

script의 실제 예외·process exit·result.json을 함께 확인한다. 기존 script/harness에 ko-KR와 공통 리소스 검사를 추가하고 실행 결과로 L02~L11을 입증한다. script의 현재 intermediate output 경로 처리에 충돌이 있으면 직접 관련된 최소 수정만 한다. 원래 `%LOCALAPPDATA%\AgentDock`에는 쓰지 않는다.

Windows command/session의 runner, exit, kill, close 관련 기존 시험과 신규 R 케이스도 같은 후보 SHA에서 실행한다. Linux command 시험 통과를 Windows runner 통과로 대체하지 않는다. ARM64와 WSL native 시험이 미실행이면 그대로 적고 지원을 새로 입증했다고 주장하지 않는다.

### 9.3 Review build와 installer

현재 Windows release build의 담당 script와 csproj를 사용한다. 빌드 출력은 source worktree 밖에 둔다. `dotnet publish` 후 실제 single-file exe에서 ko-KR resource 로드와 언어 변경을 확인한다. satellite이 누락된 경우에만 원인을 찾아 project/publish packaging의 최소 수정을 수행한다. 사전에 resource 복사 subsystem을 추가하지 않는다.

Inno compiler가 있는 격리 환경에서 한국어 language 등록과 custom message를 포함한 installer compile을 확인한다. 실제 제품 설치·업데이트·uninstall E2E는 이번 기본 Gate에 포함하지 않는다. 운영 설치 경로를 변경하여 시험하지 않는다. unsigned review artifact는 unsigned로 표시한다. 제품 버전을 임의로 다음 release 번호로 올리거나 서명된 배포물처럼 명명하지 않는다.

CI를 사용할 경우 사용자가 이미 허용한 downstream branch의 격리 runner만 사용한다. 기존 CI에 작은 localization 검사 연결은 가능하지만 전체 release pipeline이나 운영 self-hosted runner 설정을 바꾸지 않는다. runner가 없으면 installer compile과 Windows runtime 검증을 미실행으로 남긴다.

### 9.4 최종 증거와 self-review

최종 보고는 실제 BASE_SHA, 후보 HEAD, 문서 ID, worker 모델/설정 확인값, branch/remote, 파일 목록, 단계별 결과, 실행 명령/exit/시험 수/skip, L/R 수용 ID 결과, artifact hash와 미검증 범위를 포함한다. 모델 설정 확인은 secret을 제외한 selector/API 결과로 남긴다.

self-review는 특히 다음을 검사한다: 새 abstraction·새 상태 저장소가 불필요하게 생겼는가, legacy no-ID 경로가 바뀌었는가, ID lookup이 owner 검사를 우회하는가, quota/retention이 같은 ID 재실행을 허용하는가, sync나 stdin 실패가 유실되는가, 출력 제한 때문에 cursor가 건너뛰는가, 번역된 문구가 제어 흐름에 다시 들어왔는가, device guard가 무단 구현됐는가.

모든 필수 Gate가 pass이면 `IMPLEMENTED_AND_VERIFIED`로 보고한다. Windows 미검증이면 `IMPLEMENTED / WINDOWS_VALIDATION_REQUIRED`다. 관련 기능 시험 실패가 남아 있으면 `INCOMPLETE`와 차단 사유를 적는다. 코드 작성과 기능 검증 완료를 합쳐 표시하지 않는다. draft PR은 최종 merge 승인이 아니다.

## 10. 예상 변경 경계와 패치 분리

파일 수는 상한 quota가 아니라 drift를 탐지하기 위한 예상이다. 안전한 필수 변경을 숫자 맞추려고 숨기지 않는다. 범위를 크게 넘으면 이유를 checkpoint에 적는다.

### 10.1 Localization 패치

핵심 소유자는 `Localization/UiText.cs`, `ActivityText.cs`, 기존 resx 두 개 및 신규 ko-KR resx, MainWindow selector/요약, ExecutionWindow 3개, Dialog/Models/Client다. 관련 Go 정적 검사와 C# fixture를 확장한다. installer는 AgentDock.iss, messages.iss, Korean.isl 및 검사다.

이 파일군 밖의 제품 로직 변경은 원칙적으로 없다. source literal을 key로 바꾸는 넓고 기계적인 diff와 실제 동작 분리 diff를 검토하기 쉽게 나눈다. upstream에 제안 가능한 genericization과 한국어 번역은 별도 논리 패치다.

### 10.2 Recovery 패치

권장 신규 파일은 `internal/tool/command/session/peek.go`, `internal/tool/command/peek.go`, `internal/app/command_receipts.go`와 직접 시험이다. 기존 변경 지점은 command request/contract/actions/Exec/consumption, Session marker·lookup, Runtime 초기화, callObserved, specs_command adapter, execution_sessions의 ownership 재사용, context capability/schema, execution_guidance 및 계약 시험이다.

Activity/Permission의 파일 포맷 migration, 별도 receipt DB, 전체 Call projection 재작성은 기본 범위가 아니다. 기존 CallID와 binding을 활용한다. 필요한 조회 helper는 기존 owner 안에 추가한다. user-facing 기능 문서는 기존 `docs/execution-center-api.md`, `docs/task-activity.md`, `docs/agents-context.md` 중 실제 소유 문서에만 짧게 반영한다.

### 10.3 권장 commit 단위

후속 실행에서 허용된 cloud feature branch에 한하여 다음 논리 단위로 commit할 수 있다. 단계 번호와 commit 개수가 반드시 같을 필요는 없다.

```text
refactor(windows): route activity and execution UI text through resources
feat(windows): add Korean UI and installer resources
feat(command): expose non-consuming absolute output peek
feat(command): add runtime-scoped execution request receipts
test(recovery): cover lost responses ownership retention and races
```

문서와 관련 시험은 각 기능 commit에 포함한다. 중간 미완료 작업을 green으로 표시하지 않는다. installer exe, screenshot/video, fixture runtime, bin/obj, 로그, credentials는 Git에 넣지 않는다. reviewer에게 전달할 최종 artifact와 짧은 verification summary만 별도 보관한다.

## 11. Device target 보류 결정

이번 범위에서 `expected_target`, instance/profile alias 설정, identity revision, 공통 target schema decorator를 구현하지 않는다. Workspace는 수신한 서버 안에서 경로를 검증하고 다른 connector 선택을 막지 않는다는 경계를 문서로만 유지한다. 기존 SourceOwnerKey, Conversation gate, Permission, Workspace 검사는 recovery 기능을 이유로 약화하지 않는다. [S13–S20]

후속 구현을 검토할 조건은 실제 다중 장치 운영에서 방지가 필요해지고 host가 사용자 의도의 expected target을 전달할 경로가 확인됐을 때다. 그때의 최소 후보는 운영자 alias와 공통 ingress 검증이며 ERA 전체 identity/guidance를 재구축하지 않는다. 이 후속 후보는 이번 worker의 할 일 목록이 아니다.

## 12. Grokbot의 중간 보고와 복구 규칙

각 단계 경계에서만 문서 ID, stage, 실제 HEAD, 완료한 L/R IDs, 실행한 시험·skip, 다음 작업, blocker를 짧게 갱신한다. 대규모 선독 기록이나 장황한 작업 일지를 저장소에 누적하지 않는다. 필요한 단일 checkpoint는 cloud task artifact에 둔다.

작업 호출 응답이 없으면 기존 cloud agent/run의 상태·conversation·artifact를 조회한다. 새 agent를 만들어 같은 branch에 중복 작업시키지 않는다. 상태를 모르는 cloud 실행이 남아 있으면 다음 writer를 시작하지 않는다. 코드 변경 도중 process가 멈췄으면 current diff를 보존하고 해당 stage의 검사부터 복구한다.

단순한 파일명·내부 helper 선택·번역 문구는 worker가 이 계약 안에서 결정한다. 모델 제공 불가, writable remote 승인 부재, 운영 접근 필요, public contract 충돌, 병렬 상태 시스템이 필요한 수준의 재설계는 blocker다. 작은 구현 선택마다 사용자의 재승인을 요구하지 않는다.

최종 인계 후 main merge·운영 설치·배포를 자동으로 계속하지 않는다.

## 13. 외부 실행 환경 근거

이 문서의 제품 코드 사실은 1.2의 실제 Git 소스를 기준으로 한다. 다음 공식 문서는 Cloud 실행·문서 전달·모델 선택의 환경 가정을 확인하기 위한 것이다. 실제 계정의 가용 기능/허용 variant는 실행 직전 다시 조회한다.

- Cursor Cloud Agents: https://cursor.com/docs/cloud-agent — 독립 cloud 환경, repository branch 인계와 모델/context 선택.
- Cursor Cloud Agents API: https://cursor.com/docs/cloud-agent/api/endpoints — `GET /v1/models`의 실제 model id, parameters, variants를 조회하여 선택. 이 문서는 특정 Grok 모델의 실제 계정 제공을 보장하지 않는다.
- Cursor Models & Pricing: https://cursor.com/docs/models-and-pricing — 지원 모델의 공개 안내. 문서 작성 시 요청한 `Grok 4.7 / 500k / xHigh` 조합의 계정 가용성은 확인하지 못했다.
- Grok Bot: https://cursor.com/docs/grok-bot — Bot의 cloud computer와 작업 역할.
- Work with Grok Bot: https://cursor.com/docs/grok-bot/work — 파일 첨부 및 작업·권한 경계를 포함한 지시 전달.

이 계획의 권고로 표시된 신규 수치·error code·wire field·단계는 downstream v1 설계 선택이지 현재 upstream이 이미 제공하는 기능이라는 주장이 아니다.
