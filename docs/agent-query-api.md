# Agent 조회 API v2 (0.2.0)

서버 소스의 구현 계약이다. 배포와 호스트 도구 재검색은 별도 절차이며, 문서 변경만으로 설치된 플러그인이 갱신되지는 않는다.
기존 CLI 목록과 Events 원본 스냅샷은 유지된다. MCP 목록의 `data`는 배열에서 아래의 품질 메타데이터와 `items` 객체로 바뀐다.

## 조회

개수 제한 인수는 받지 않는다. `term`은 `2026,2` 또는 `2026-2`, `course_ids`는 정확한 과목 ID 배열이다.

| 도구 | intent | 추가 조건 |
| --- | --- | --- |
| `klap_lectures` | next_opening, available_now, due_in_period, unfinished, completed, overview | unfinished에는 include_expired 필수; due_in_period에는 period 필수 |
| `klap_assignments` | pending, submitted, overdue, due_in_period, overview | due_in_period에는 period 필수 |
| `klap_notices` | recent, in_period, overview | recent는 최근 7일; in_period에는 period 필수 |
| `klap_due` | next_deadline, in_period | 선택 kinds: assignment, lecture, academic; in_period에는 period 필수 |
| `klap_search` | search | query 필수; 선택 kinds: notice, assignment, lecture, academic; 선택 period |

```json
{"term":"2026,2","intent":"unfinished","include_expired":false}
```

false는 기간 종료가 확인된 강의를 제외하고, true는 기간 종료분을 포함한다. 날짜·완료 상태를 알 수 없는 후보는 보존한다.
학기는 자동으로 확장하지 않는다. 기간 종료와 자동 수강 지원 여부는 완료 여부와 별개다.

```json
{"intent":"due_in_period","period":{"name":"this_week"}}
```

기간은 today, this_week, next_week, next_7_days 또는 custom이다. custom에는 from/until이 필요하다.
날짜만 지정하면 KST, 주간은 월요일 시작이며 모든 기간은 `[from, until)`이다. 종료 시각과 같은 순간의 강의는 코어 정책상 아직 기간 내다.
next_opening은 가장 이른 미래 시작 시각의 미완료 후보 전체를, next_deadline은 가장 이른 마감 시각의 후보 전체를 반환한다.
미확인 후보가 최솟값에 영향을 줄 수 있으면 확정하지 않는다.

검색 필드는 title/course_name뿐이다. 기간은 강의 시작, 과제 마감, 공지 게시, 학사일정 종료를 기준으로 적용한다.
학사일정의 기간 없는 조회는 현재 연도와 다음 연도이며, 사용자 기간은 최대 5개 연도까지 지원한다. 학사일정은 과목에 속하지 않는다.

## 일괄 상세와 완료 확인

```json
{"ids":["조회에서 받은 stable ID"],"sections":["completion"]}
```

단건도 같은 도구 `klap_get_items`를 사용한다. 강의 완료 상태는 최신 원본의 completed/incomplete/unknown이며,
일괄 호출 안에서만 같은 학기·과목의 원본을 재사용한다. 새 호출은 다시 조회한다. 과거 학기 ID도 지원한다.

| 종류 | 지원 sections |
| --- | --- |
| 강의 | completion, learning |
| 공지 | content, attachments |
| 과제 | content, submission |

본문은 원문 텍스트이며 파일 다운로드나 영상 전사는 실행하지 않는다. 공지 첨부는 원본이 제공한 메타데이터 문자열이다.
지원하지 않는 영역은 `unsupported_sections`, 전체가 미지원이면 status=unsupported다.
중복 ID는 첫 등장 순서대로 한 번만 반환한다. 각 항목은 ok/not_found/error와 원본 fetched_at을 제공한다.
실패한 항목을 incomplete로 바꾸지 않는다.

## 품질과 전송

- `coverage`: 요청/성공 과목, 실패 과목과 코드, 원본 조회 시각 범위. 통합 조회는 `sources`에 종류별 상태도 제공한다.
- `selection`: verified/uncertain. 확정된 경우에만 matched_total이 숫자이며, 불확실하면 null과 observed_matches를 제공한다.
- `page`: complete와 next_cursor. 후속 요청은 같은 도구에 `{ "cursor": "..." }`만 전달한다.

원본 조회 실패가 없고 selection이 verified일 때만 빈 결과를 ‘없음’으로 판단한다.
전체·동률 묶음의 전달 완료는 page.complete까지 확인해야 한다.

서버는 전체 의미상 결과를 선택한 다음 약 28 KiB의 data 페이지로 나누어 MCP 외피 포함 32 KiB에 여유를 둔다.
5분 스냅샷은 호출 주체, 계정, 도구와 선택 결과에 묶이며 재시도는 같은 페이지를 반환한다.
필터와 cursor 혼용, 만료, 재시작, 다른 계정의 cursor는 오류다. 첫 조회부터 다시 시작해야 한다.
스냅샷 저장 예산은 64 MiB다. 초과하면 오류로 알리며 항목을 조용히 버리지 않는다.

큰 단일 항목은 `item_fragment`의 id/part/parts/encoding/text로 나눈다. part는 0부터 시작한다.
같은 ID의 text를 순서대로 이어 붙인 JSON이 원래 항목이다. UTF-8 경계를 지키며 전부 수신하기 전에는 완성된 항목이 아니다.
메타데이터 자체가 페이지 예산을 초과하면 범위를 좁히라는 오류를 반환한다.

`structuredContent`가 전체 결과이며 `content.text`는 짧은 안내다. 전체 JSON을 두 번 반환하지 않는다.
MCP Apps 미지원 호스트도 structuredContent를 읽어야 한다. text만 읽는 구형 클라이언트는 이 버전의 지원 대상이 아니다.

## 누락 방지와 운영 경계

동일 세션의 과목 선택/조회는 클라이언트 간 직렬화한다. Unix는 같은 UID·임시 디렉터리를 쓰는 프로세스 간 파일 잠금도 사용한다.
다른 호스트·컨테이너의 공유되지 않는 임시 디렉터리 및 Windows 프로세스 간에는 잠금이 공유되지 않는다.
각 배포는 독립 로그인 세션을 사용해야 한다. 과목·학기 불일치는 `course_mismatch`로 표시한다.

MCP Events의 전체 원본 조회는 이 필터나 cursor를 사용하지 않는다. 기존 항목 수정 이벤트의 추가는 이번 변경 범위가 아니다.

## 검증 기록 (2026-10-29)

- core 전체 회귀 테스트, klas/app/core race 검사 통과.
- 서버 전체 `go test -race ./...` 통과. MCP count 인수 거부·인증별 cursor·동일 cursor 재시도·유니코드 조각 복원 검증.
- 웹 테스트 및 TypeScript/웹 빌드 통과. 인앱브라우저에서 v2 강의 요약과 공지의 지연 상세 표시 확인.
- 패키지 검증 5개 통과. 플러그인 소스 버전 0.2.0.
- 40개 강의 고정 fixture에서 최초 시작 동률 4개를 보존하면서 비교 응답 바이트 42,910 → 1,546 (96.4% 감소).
  이는 fixture의 직렬화 바이트 비교이며 실제 모델 토큰 수·운영 p50/p95·전체 답변 시간 측정이 아니다.
- 새 버전의 운영 배포, 실제 KLAS 원본 E2E, ChatGPT 도구 재검색 및 클라우드 플러그인 릴리스는 아직 수행하지 않았다.
