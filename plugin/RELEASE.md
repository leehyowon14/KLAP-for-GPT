# KLAP 비공개 릴리스

2026-10-27에 생성하고 2026-10-28에 클라우드 연결을 반영한 사용자 계정의 비공개 플러그인이다.

- 이름: KLAP
- 버전: `0.2.0`
- Plugin ID: `plugins_6ae05a8b1af08191a7b593f119b7f914`
- Release ID: `pluginrel_6ae2a56c556c81918713fc5a9123777d`
- [플러그인 열기](https://chatgpt.com/plugins/plugins_6ae05a8b1af08191a7b593f119b7f914)
- MCP: `https://klap.leehyowon14.dev/mcp`

**클라우드 도구 연결 확인:** 등록된 App 바인딩과 OAuth 연결을 반영했고, 실제
`klap_open` 호출이 성공했다. ChatGPT 상세에는 도구 18개와 이벤트 3종이 표시됐다.
초기 연결 당시 활성 이벤트 구독은 0개였다. 2026-10-29 조회에서는 기존 활성 구독 3개를 확인했다.
검증 단계와 초기 문제는 [클라우드 연결 복구](CLOUD_CONNECTION.md)에 기록했다.

저장 후 `get_plugin_files`로 스킬·매니페스트·아이콘과 생성된 호환 매니페스트를 확인했다.
계정에 저장된 상태, 호스트의 OAuth·도구 연결, 실제 이벤트에 의한 에이전트 실행은
별개이며 마지막 단계는 아직 확인하지 않았다.

패키지 재생성:

```sh
python3 scripts/package-plugin.py
# 직접 MCP를 사용하는 별도 클라이언트용 export (클라우드 등록을 대신하지 않음):
python3 scripts/package-plugin.py --portable
# 별도 개인 인스턴스에 배포했다면, 실제 접근 확인 후 주소 변경:
python3 scripts/package-plugin.py --origin https://your-klap.example
```

기존 비공개 플러그인을 수정할 때는 위 Plugin ID와 현재 release를 조회한 뒤
Plugin Creator의 update 흐름을 사용한다. 같은 플러그인을 새로 생성하지 않는다.

## 0.1.1 브랜드 개편

KLAP_mac의 `KLAP-Mark.svg`와 `KLAP-Sparks.svg`를 조합한 로고로 대표 아이콘을 교체했다.
기존 Plugin ID·비공개 범위·MCP 연결·스킬·프롬프트는 유지했다.
저장 후 대표 아이콘의 원본 바이트 일치, 두 매니페스트의 버전 변경만 존재하는 것,
전체 파일 목록 보존을 `get_plugin_files`로 확인했다. 설치된 호스트의 아이콘 캐시 갱신은 별도다.

웹 앱의 Tossface 아이콘은 서버의 자체 포함 UI 번들에 있으며 플러그인 패키지에는 포함하지 않는다.

## 0.1.2 투명 로고

대표 로고를 둘러싼 배경 rect를 제거하고 KLAP_mac의 Mark·Sparks 경로만 남겼다.
기존 Plugin ID·MCP·스킬·프롬프트·비공개 범위는 유지했다.
저장 후 아이콘 원본 바이트와 매니페스트 및 전체 파일 목록 보존을 재조회했다.

## 0.1.3 클라우드 App 연결

공식 사용자 지정 MCP 서버 등록 흐름에서 확인한 실제 App을 기존 KLAP 패키지에 연결했다.
로고·스킬·기본 프롬프트·Plugin ID·비공개 범위는 유지했고, 로컬 실행으로 분류되던
직접 MCP 선언은 클라우드 패키지에서 제거했다. 직접 MCP export는 `--portable` 또는
`--origin`으로 별도 생성한다. 패키징 테스트 5개와 저장 후 소스 재조회가 통과했다.

## 0.2.0 의도별 조회 안내

2026-10-29에 기존 release를 조건으로 Plugin Creator update를 적용했다.
의도별 필터, unfinished의 include_expired 선택, 일괄 상세와 completion 조회,
coverage/selection 및 cursor/fragment 처리 지침을 스킬에 반영했다.
저장 후 get_plugin_files로 0.2.0과 새 release를 확인했다. 기존 App 바인딩은 동일하고,
두 매니페스트는 버전 이외의 필드가 그대로이며 아이콘과 전체 파일 목록도 유지됐다.
USER/PRIVATE 범위와 Plugin ID를 유지했다.

운영 서버의 v2 도구를 실제 클라우드 연결로 호출한 결과는
[Agent 조회 운영 검증](../docs/agent-query-production.md)에 기록했다.
설치된 각 호스트의 스킬 캐시 갱신이나 자연어 도구 선택은 별도 확인 대상이다.
