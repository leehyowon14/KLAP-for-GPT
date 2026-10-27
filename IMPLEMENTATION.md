# KLAP for GPT 구현 기록

## 제품 범위

개인 소유 인스턴스에 KLAS 계정 하나를 연결한다. KLAP-core가 인증, 응답 파싱,
안정적인 리소스 ID, 조회, 수강, Apple 동기화 정책을 소유한다. CLI·KLAP_mac·MCP는
같은 코어를 사용하고 KLAS API를 각각 구현하지 않는다. Go 모듈 경로는 기존
클라이언트 호환을 위해 `github.com/leehyowon14/KLAP-cli`를 유지한다.

- 서버 설치: Docker Compose로 사용자가 소스에서 빌드. Apple 기능은 노출하지 않는다.
- Mac 설치: 사용자 Mac에서 Go와 Swift를 네이티브 빌드. Keychain과 EventKit 사용.
- 혼합 설치: 서버와 Mac helper의 인증된 outbound 연결. 연결이 끊기면 새 Apple 작업을 받지 않는다.
- OAuth Authorization Code + PKCE로 MCP 연결. 학번·암호는 인증된 설정 화면에서만 입력.
- 조회와 MCP Apps 화면: 마감, 공지, 과제, 강의, 요일×교시 시간표, 출석, 학사일정.
- 수강 작업: 영속 상태, 순차 실행, 멱등 요청, 진행/취소, 재시작 시 interrupted.
- Events: `notice.created`, `assignment.created`, `lecture.created`; 첫 조회는 기준선.
  5분 주기 fresh 조회, 중복 방지, 지속적인 outbox, 검증된 HTTPS webhook에 서명 전달.
  MCP2 `2026-07-28`, `server/discover`, `events/list|subscribe|unsubscribe` 지원.
- 다운로드·전사·강의평가·예약 수강은 플러그인 v1 도구에서 제외.
  기존 CLI와 Mac 기능은 공통 코어에 보존.

## 완료된 기반

- [x] 고정 커밋의 `core/` 서브모듈.
- [x] Mac 앱의 7개 패치와 Go 브리지를 코어에 통합, 변경별 테스트·커밋.
- [x] `cmd/klap-core` 네이티브 JSON 브리지와 `pkg/core` Go API.
- [x] 요청별 학기 격리와 서버 자격증명 저장소 주입.
- [x] Mac 앱의 코어 직접 빌드 경로 전환. 최종 회귀 결과는 아래 기록.
- [x] 영속 저장소와 OAuth, 설정 화면.
- [x] 조회 도구와 MCP Apps UI.
- [x] 수강 작업과 취소.
- [x] Events 구독, 감지, 서명, 전송 재시도.
- [x] 서버 이미지 및 Mac 앱·helper 빌드, 서버 배포와 격리된 Mac 기동.
- [x] Plugin Creator에 비공개 패키지 저장 및 저장된 구성 확인.
- [ ] 실제 호스트 설치, UI·도구·에이전트 호출 검증.

## 검증 기준

컴파일, 단위 테스트, 로컬 MCP 통합 테스트, 배포, 호스트 연결, 실제 KLAS 응답,
실제 이벤트에 의한 에이전트 실행을 구분해 기록한다. 비밀정보는 저장소와 로그에 넣지 않는다.
테스트는 중복/순서 변경/부분 실패/취소/재시작/권한 만료를 포함한다.
변경은 독립적으로 설명·revert 가능한 단위로 즉시 커밋한다.

## 2026-10-27 검증 결과

| 항목 | 결과와 범위 |
| --- | --- |
| Linux Go | `go test -race -timeout 60s ./...` 통과. 최종 Docker 소스 빌드에서도 `go test -timeout 90s ./...` 통과 |
| Mac Go | MCP/OAuth 통합, helper, jobs, klap, Events의 race 테스트 통과. 두 실행 명령 빌드 통과 |
| KLAP-core | 최종 `68cb7d4`의 app·pkg/core race 테스트 통과. 신규 학기 격리, stable ID, 기준선, Apple 어댑터 및 부분 오류 테스트 포함 |
| React | TypeScript 검사·단일 HTML 번들·UI 모델 테스트 3개 통과. 시간표 겹침/0교시/주말/온라인, 안전한 링크, 오래된 수강 선택 검사 |
| 브라우저 | 기존 Safari의 예시 데이터에서 대시보드·요일×교시 시간표·완료 강의 선택 불가·작업 등록과 취소 표시 확인. 실제 KLAS 및 호스트 iframe 검증과 구분 |
| Mac 네이티브 | GPT 앱과 helper의 Go·Swift 빌드 및 코드 서명 검사 통과. 격리된 임시 설정으로 Mac 모드의 health·초기 설정·종료 확인 |
| KLAP_mac 회귀 | 최종 전체 실행에서 11 통과·1 실패·3 건너뜀. 새 core pin의 브리지 빌드 후 실패한 시작 화면 18개 검사와 건너뛴 Go 브리지·Swift 미리알림 정책 검사를 개별 재실행하여 모두 통과. Sparkle 업데이트 설치는 미실행 |
| Mac helper | 페어링 만료/재사용/폐기, 오프라인 거절, 연결 종료, 요청 취소, 재전송 방지 테스트 통과. 실제 EventKit 쓰기와 사용자 권한 승인은 미수행 |
| 배포 | rpi5-server의 Docker 소스 빌드 및 health 통과. nginx IPv4/IPv6와 외부 HTTPS 확인. 익명 MCP 401과 OAuth 메타데이터 확인 |
| 비공개 플러그인 | `plugins_6ae05a8b1af08191a7b593f119b7f914`, `0.1.0` 저장. OAuth MCP 구성, 스킬, 아이콘 및 호환 파일 재조회 확인 |

수강·Events 테스트는 부분 실패, 중복, 재시작, 권한 폐기, 만료, 서명 키 교체,
HTTP 재시도와 영구 실패를 포함한다. 만료된 구독 64개가 새 구독을 영구적으로
막지 않도록 만료 시점의 할당량 회수도 검증했다.

KLAP_mac의 첫 실행에서 `firstClickOpened`가 한 번 false였으며 재실행에서는
재현되지 않았다. 이를 고치기 위한 UI 소스 변경이나 테스트 완화는 하지 않았다.
새 core pin의 빌드·서명 갱신 전에는 해당 SHA의 캐시가 없어 두 검사가 건너뛰어졌고,
빌드 후 명시적으로 실행했다. 이를 처음부터 단일 실행에서 모두 통과한 것으로 기록하지 않는다.

참조 배포는 `https://klap.leehyowon14.dev`, MCP는 `/mcp`, 모드는 `server`이다.
서버 경로는 `/home/ubuntu/apps/klap-for-gpt`, 포트는 `127.0.0.1:3030`, 데이터 볼륨은
`klap-gpt_data`이다. 기존 Cloudflare 터널의 IPv6 loopback 전달도 nginx에 반영했다.

공통 코어 변경은 [초안 PR #1](https://github.com/leehyowon14/KLAP-cli/pull/1)로
리뷰할 수 있다. 원래 KLAP_cli 체크아웃과 GPT의 submodule, KLAP_mac의 core-ref는
같은 코어 SHA를 사용한다. GPT 저장소의 커밋은 로컬에 있으며 새 원격 저장소는 만들지 않았다.

## 사용자 연결 후 남은 확인

- 소유자 암호 설정과 실제 KLAS 계정 연결.
- 비공개 KLAP 플러그인 설치, OAuth 동의 후 실제 조회 및 MCP App 표시.
- 지원되는 Cloud Work 채팅에서 세 이벤트 구독 후 실제 새 콘텐츠로 webhook 수신과
  에이전트 실행을 각각 확인. 구현·모의 전송 테스트가 실제 에이전트 실행을 증명하지 않는다.
- Mac 또는 hybrid를 사용할 경우 LaunchAgent 설치, Keychain 페어링, EventKit 권한과
  실제 캘린더·미리알림 충돌 처리 확인. 현재 운영 서버에는 Apple 도구를 노출하지 않는다.
- 실제 강의 수강은 사용자가 선택하여 요청한 경우에만 별도 검증.
