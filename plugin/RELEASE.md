# KLAP 비공개 릴리스

2026-10-27에 Plugin Creator로 사용자 계정에 저장한 비공개 플러그인이다.

- 이름: KLAP
- 버전: `0.1.0`
- Plugin ID: `plugins_6ae05a8b1af08191a7b593f119b7f914`
- Release ID: `pluginrel_6ae05a8cc2b08191a6139ac245d9d081`
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
