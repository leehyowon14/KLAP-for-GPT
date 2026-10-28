# KLAP 클라우드 MCP 연결 복구

## 현재 확인 상태

- 기존 비공개 패키지는 `plugins_6ae05a8b1af08191a7b593f119b7f914`, 버전 `0.1.2`이다.
- 사용자가 ChatGPT 상세 화면에서 **데스크톱 전용** 표시를 확인했다.
- 패키지에는 HTTPS `mcp.json`이 있지만 등록된 클라우드 App의 `.app.json` 바인딩은 없다.
- `requires_local_executor: true`를 명시한 파일은 없다. 따라서 배지만 없애는 변경으로
  클라우드 도구가 등록됐다고 주장할 수 없다.
- Plugin Management의 `not_installed` 응답만으로 미설치를 근본 원인으로 확정했던
  안내는 정정한다. 패키지 저장, 데스크톱 설치, 클라우드 MCP 등록은 따로 검증한다.

### 2026-10-28 Safari에서 확인한 등록 화면

- 로그인된 Safari의 기존 KLAP 상세 화면에는 MCP 서버 1개와
  **데스크톱 앱에서 열기** 버튼이 표시됐다. 관리 메뉴에는 ZIP 다운로드,
  새 버전 업로드, 삭제만 있고 클라우드 연결 전환 항목은 없었다.
- 플러그인 목록의 **추가 → 사용자 지정 MCP 서버 추가**에서 기존 MCP URL을
  입력하자 ChatGPT가 DCR, `klap` scope, authorization/token/registration URL,
  issuer와 resource를 자동 검색했다.
- 기본 및 필수 권한 범위를 `klap`으로 준비했다. 최초 **플러그인으로 생성**은
  자동 승인 심사가 기존 브라우저 접근 허용 범위를 넘는다고 판단해 차단했다.
  이후 사용자의 명시적 생성 승인을 받아 요청을 진행했다.
- 서버 access log에서 `POST /oauth/register`의 `201`과 metadata 조회의 `200`을
  확인했다. 이는 OAuth client 등록 성공이며, 인증된 MCP 도구 호출 성공은 아니다.
- ChatGPT의 **KLAP 연결** 데이터 접근 동의 화면까지 도착했다. 소유자가 연결 및
  후속 OAuth 동의를 직접 완료하도록 인계했다. 실제 connector ID와 도구 검색 결과,
  설치 완료 및 이벤트 전달은 아직 확인하지 않았다.

### Safari 연결 허용 후 정지 원인과 수정

소유자가 동의 버튼을 눌렀을 때 서버에는 `POST /oauth/authorize`의 `303`이
기록됐지만, Safari 콘솔에는 리다이렉트 목적지가 CSP `form-action`에 없다는
차단 오류가 표시됐다. 동의 페이지가 `form-action 'self'`만 허용해 ChatGPT로의
복귀를 막은 것이다. 무응답이나 OAuth client 등록 실패로 분류하지 않는다.

`e1841c2`는 Fosite가 등록 정보와 대조한 callback origin만 해당 동의 페이지의
`form-action`에 추가한다. 로그인 페이지의 정책, PKCE, CSRF, Origin 검증은 유지한다.
와일드카드 및 CSP 구분자를 포함한 host는 정책에 삽입하지 않는다.
`go test -race -count=1 -timeout 90s ./internal/auth ./internal/server`가 통과했다.
운영 Docker 빌드에서도 전체 Go 테스트와 웹 빌드·테스트 3개가 통과했다.
배포 전 519개 소스 파일 해시를 대조하고 수정 코드·테스트 두 파일만 교체했다.
이전 이미지와 두 파일 및 manifest의 롤백 사본을 보존했다. 새 컨테이너는 healthy이며
내부·공개 `/health` 모두 정상 응답했다.

기존 Safari 동의 페이지를 새로고침하고 Web Inspector에서 문서 응답 `200`,
`Cache-Control: no-store`, `Referrer-Policy: same-origin`,
`form-action 'self' https://chatgpt.com`이 포함된 CSP를 확인했다.
소유자에게 **연결 허용** 재시도를 요청했다. 실제 ChatGPT 복귀·토큰 교환·도구 검색과
MCP Events 노출은 아직 확인하지 않았다.

## 사용할 기존 서버와 인증 설정

| 항목 | 검증한 값 |
| --- | --- |
| 이름 | KLAP |
| MCP URL | `https://klap.leehyowon14.dev/mcp` |
| 전송 | Streamable HTTP |
| 인증 | OAuth 2.1 / PKCE S256 |
| OAuth client 등록 | DCR, public client (`none`) |
| Scope | `klap` |
| Resource | `https://klap.leehyowon14.dev/mcp` |
| Authorization server | `https://klap.leehyowon14.dev` |
| Metadata | `https://klap.leehyowon14.dev/.well-known/oauth-authorization-server` |
| Protected resource metadata | `https://klap.leehyowon14.dev/.well-known/oauth-protected-resource/mcp` |

기존 서버와 KLAS 저장소를 유지한다. 별도 Sites 서버나 프록시를 새로 만들지 않는다.
설치 토큰, 소유자 암호, KLAS 암호, OAuth token은 패키지에 넣지 않는다.

## 복구할 연결

1. 로그인된 ChatGPT의 기존 KLAP 관리 화면에서 실행 환경과 MCP 등록 상태를 확인한다.
2. 기존 서버를 클라우드 MCP로 연결하는 지원 UI에서 위 설정으로 서버 검색을 수행한다.
   기존 플러그인을 수정하는 경로를 우선하며, 중복 플러그인 생성으로 우회하지 않는다.
3. 지원 흐름이 반환한 실제 App/connector ID와 도구 검색 결과를 확보한다.
   ID를 추측하거나 배포 URL을 App ID로 대신 넣지 않는다.
4. 기존 플러그인에 등록된 App 연결을 반영하고, 클라우드 실행 가능 상태를 다시 확인한다.
   서버 등록 UI가 생성하는 바인딩과 별도 로컬 MCP 설정을 구분한다.
5. 소유자의 OAuth 동의는 브라우저의 정식 연결 흐름에서 직접 완료한다.

## 완료 기준

클라우드 채팅에서 일반 KLAP 조회 성공 → 이벤트 소스에서 실제 connector ID 확인 →
그 ID의 webhook 스키마 검색 성공 → 반환된 스키마에 맞는 구독 생성 → 실제 새 항목 수신.
이벤트 검증은 MCP Events를 지원하는 Cloud Work 채팅에서 수행한다.

서버의 비인증 응답은 OAuth discovery URL을 포함한 HTTP 401이다.
격리된 OAuth·MCP 도구 검색·이벤트 검색 통합 테스트는 통과했지만,
위 클라우드 호스트 완료 기준을 대신하지 않는다.

공식 지침:
- [MCP 연결과 설치](https://developers.openai.com/plugins/deploy/connect-chatgpt)
- [등록된 App 매핑](https://developers.openai.com/plugins/build/plugins)
- [MCP Events의 클라우드 실행 조건](https://developers.openai.com/plugins/build/mcp-events)
