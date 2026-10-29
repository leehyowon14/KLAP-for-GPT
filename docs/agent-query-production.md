# Agent 조회 v2 운영 검증

2026-10-29 10:49–10:53 KST에 운영 컨테이너와 실제 연결된 KLAP MCP 도구로 확인했다.

## 배포

- 서버: `https://klap.leehyowon14.dev`, Raspberry Pi의 `klap-gpt-klap-1`.
- 소스: `6e9bf2bf85932a9a223271e218d4c77e48485c7a`.
- core: `af21383819d90fd609671f9fd06a947240b3e1c6`.
- 실행 이미지: `sha256:f595470ff7d095bae01729f4bdd5e6a1ef8f8aa818f0a32f63e37069325ebc03`.
- 추적 파일만 패키징했으며 배포 후 546개 파일의 SHA-256 일치 확인.
- 이미지 빌드 중 웹 테스트 12개, 웹 빌드, `go test -timeout 90s ./...`, Linux 서버 빌드 통과.
- 컨테이너 healthy 및 외부 `/health`의 `status=ok` 확인.
- 기존 이미지 `klap-gpt-klap:rollback-query-6e9bf2b`와 `rollback/before-query-6e9bf2b/source.tar.gz` 보관.
  장애 시 해당 이미지를 `klap-gpt-klap:latest`로 태그한 뒤 `docker compose up -d --no-build klap`으로 복구한다.
  소스도 보관본으로 복구하며 기존 데이터 볼륨과 환경 파일은 유지한다.

## 실제 연결 결과

`2026,2`의 여섯 과목을 조회했다. 모든 아래 목록 응답은 coverage complete, 6/6 성공,
selection verified, page complete였다. 항목 수는 검증 당시의 관찰값이며 고정 기대값이 아니다.

| 조회 | 결과 |
| --- | --- |
| 강의 overview | 29개 |
| next_opening | 최초 시작 시각이 같은 2개 전부 반환 |
| available_now | 0개; 현재 열린 강의는 이미 완료 상태 |
| unfinished, include_expired=false | 2개 |
| unfinished, include_expired=true | 5개; 기간 종료 미완료 3개 추가 |
| completed | 24개 |
| due_in_period, this_week | 0개; 미완료 강의 기준 |
| 과제 pending | 2개 |
| 공지 recent / overview | 4개 / 29개 |

완료·미완료·기간 종료 포함 여부·최초 시작 동률의 ID 집합을 전체 목록과 대조하여 일치 확인.
`2026-2` 요청도 응답에서 `2026,2`로 정규화됐다.

- 완료/미완료 강의 ID를 한 번에 조회하여 각각 completed/100%, incomplete/0% 확인.
  중복 ID는 한 번만 반환됐고, 재조회 시 각 fetched_at이 갱신됐다.
- 공지·과제·강의 혼합 상세 조회 모두 ok. 종류별 content/submission/completion을 반환하고
  지원하지 않는 영역은 항목별 unsupported_sections로 명시했다.
- 공지 29개의 content 일괄 조회도 모두 ok였으며 한 페이지로 완료됐다.
- structuredContent에 v2 데이터를 반환하고 content.text는 83자 안내만 반환했다.
- unfinished에서 include_expired를 생략하면 실제 연결에서도 오류를 반환했다.
- count 인수는 공개 스키마에 없다. 연결 경유 호출에 limit=3을 추가한 진단에서는 오류 대신
  29개 전체가 반환됐다. 중간 계층이 미정의 인수를 제거했는지는 이 관찰만으로 확정하지 않는다.
  따라서 연결 경유의 미정의 인수 오류를 검증했다고 주장하지 않는다. 서버의 직접 인수 거부는
  `TestAgentToolContractAndContinuation`에서 검증하며 결과가 3개로 잘리지 않는 점은 실제 확인했다.

## 검증 경계

실제 인증된 클라우드 커넥터를 통한 도구 호출을 확인했다. ChatGPT 웹 화면에서 새 대화의
자연어 질문을 입력해 도구 선택과 최종 답변까지 재현한 검사는 아니다.
이번 실제 데이터는 모두 한 페이지였으므로 cursor 및 큰 항목 조각 전달은 로컬 회귀 테스트의 증거로 구분한다.
실제 과목 실패나 unknown 완료 상태를 고의로 만들지 않았다. 해당 분기는 로컬 테스트로 검증했다.
일시적 누락의 영구 부재나 운영 p50/p95, 모델 토큰 감소율을 이 결과만으로 주장하지 않는다.

마지막 상태 조회에서 연결 유지, 활성 구독 3개, 진행 중인 수강 작업 0개를 확인했다.
이 검증은 수강 작업이나 새 구독을 생성하지 않았으며 새 이벤트 전달·에이전트 실행 E2E는 수행하지 않았다.
