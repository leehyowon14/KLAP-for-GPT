# KLAP 비공개 릴리스

2026-10-27에 Plugin Creator로 사용자 계정에 저장한 비공개 플러그인이다.

- 이름: KLAP
- 버전: `0.1.2`
- Plugin ID: `plugins_6ae05a8b1af08191a7b593f119b7f914`
- Release ID: `pluginrel_6ae084ef6b48819180d032a1c964e419`
- [플러그인 열기](https://chatgpt.com/plugins/plugins_6ae05a8b1af08191a7b593f119b7f914)
- MCP: `https://klap.leehyowon14.dev/mcp`

저장 후 `get_plugin_files`로 스킬·매니페스트·아이콘과 생성된 호환 매니페스트를 확인했다.
계정에 저장된 상태와 호스트에 설치·OAuth 연결된 상태는 별개다. 이 기록 시점에는
호스트 도구 연결과 실제 이벤트에 의한 에이전트 실행은 아직 확인하지 않았다.

패키지 재생성:

```sh
python3 scripts/package-plugin.py
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
