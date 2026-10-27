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
- [ ] Mac 앱의 코어 직접 빌드 회귀 검증.
- [ ] 영속 저장소와 OAuth, 설정 화면.
- [ ] 조회 도구와 MCP Apps UI.
- [ ] 수강 작업과 취소.
- [ ] Events 구독, 감지, 서명, 전송 재시도.
- [ ] 설치 경로별 빌드 및 연결 검증.
- [ ] 실제 호스트 설치, UI·도구·에이전트 호출 검증.

## 검증 기준

컴파일, 단위 테스트, 로컬 MCP 통합 테스트, 배포, 호스트 연결, 실제 KLAS 응답,
실제 이벤트에 의한 에이전트 실행을 구분해 기록한다. 비밀정보는 저장소와 로그에 넣지 않는다.
테스트는 중복/순서 변경/부분 실패/취소/재시작/권한 만료를 포함한다.
변경은 독립적으로 설명·revert 가능한 단위로 즉시 커밋한다.
