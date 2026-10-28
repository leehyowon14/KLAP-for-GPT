# KLAP 클라우드 MCP 연결 복구

## 현재 확인 상태

- 기존 비공개 패키지는 `plugins_6ae05a8b1af08191a7b593f119b7f914`, 버전 `0.1.2`이다.
- 사용자가 ChatGPT 상세 화면에서 **데스크톱 전용** 표시를 확인했다.
- 패키지에는 HTTPS `mcp.json`이 있지만 등록된 클라우드 App의 `.app.json` 바인딩은 없다.
- `requires_local_executor: true`를 명시한 파일은 없다. 따라서 배지만 없애는 변경으로
  클라우드 도구가 등록됐다고 주장할 수 없다.
- Plugin Management의 `not_installed` 응답만으로 미설치를 근본 원인으로 확정했던
  안내는 정정한다. 패키지 저장, 데스크톱 설치, 클라우드 MCP 등록은 따로 검증한다.

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
