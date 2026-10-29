// Explicit fixture preview, bound only to loopback. Never used by the server build.
import http from "node:http";
import { readFile } from "node:fs/promises";
const now = new Date().toISOString(),
  due = new Date(Date.now() + 2 * 86400000).toISOString();
const term = {
  label: "2026학년도 2학기",
  value: "2026,2",
  subjList: [
    { name: "자료구조", value: "c1" },
    { name: "데이터과학", value: "c2" },
    { name: "물리적 AI", value: "c3" },
  ],
};
const assignments = [
  {
    ID: "assignment:fixture:1",
    CourseName: "자료구조",
    Assignment: {
      Title: "이진 탐색 트리 구현 과제",
      DueAt: due,
      Submitted: false,
    },
  },
  {
    ID: "assignment:fixture:2",
    CourseName: "데이터과학",
    Assignment: {
      Title: "분류 모델 비교 실험 보고서",
      DueAt: due,
      Submitted: true,
    },
  },
];
const notices = [
  {
    ID: "notice:fixture:1",
    CourseName: "물리적 AI",
    Notice: {
      Title: "실험 결과 보고서 제출 안내",
      Author: "담당 교수",
      Registered: now,
      Top: true,
    },
  },
  {
    ID: "notice:fixture:2",
    CourseName: "자료구조",
    Notice: {
      Title: "중간고사 범위 및 실습 안내",
      Author: "담당 교수",
      Registered: now,
    },
  },
];
const lectures = [
  {
    row: {
      ID: "lecture:fixture:1",
      CourseName: "자료구조",
      Lecture: {
        Title: "이진 트리의 순회",
        WeekNo: "8",
        Progress: "25",
        AchievedTime: "10분",
        RequiredTime: "40분",
        EndAt: due,
      },
    },
    reason: "",
  },
  {
    row: {
      ID: "lecture:fixture:2",
      CourseName: "데이터과학",
      Lecture: {
        Title: "모델 평가와 교차 검증",
        WeekNo: "8",
        Progress: "100",
        AchievedTime: "35분",
        RequiredTime: "35분",
        EndAt: due,
      },
    },
    reason: "이미 수강을 완료한 강의입니다.",
  },
];
let jobs = [];
const fixtures = {
  klap_terms: [{ Term: term, Current: true }],
  klap_courses: [term],
  klap_assignments: assignments,
  klap_notices: notices,
  klap_lectures: lectures,
  klap_due: {
    Items: [
      {
        Kind: "과제",
        ID: assignments[0].ID,
        CourseName: "자료구조",
        Title: assignments[0].Assignment.Title,
        DueAt: due,
        Status: "미제출",
      },
      {
        Kind: "온라인 강의",
        ID: lectures[0].row.ID,
        CourseName: "자료구조",
        Title: "이진 트리의 순회",
        DueAt: due,
        Status: "수강 가능",
      },
      {
        Kind: "학사일정",
        Title: "2학기 수강 철회 신청",
        DueAt: due,
        Status: "",
      },
    ],
    Errors: [],
  },
  klap_timetable: {
    Term: term,
    Entries: [
      {
        SubjectID: "c1",
        SubjectName: "자료구조",
        Weekday: 1,
        Period: 1,
        Span: 2,
        Room: "비마관 312호",
      },
      {
        SubjectID: "c2",
        SubjectName: "데이터과학",
        Weekday: 2,
        Period: 3,
        Span: 2,
        Room: "새빛관 202호",
      },
      {
        SubjectID: "c3",
        SubjectName: "물리적 AI",
        Weekday: 4,
        Period: 2,
        Span: 3,
        Room: "실습실",
      },
      {
        SubjectID: "c1",
        SubjectName: "자료구조",
        Weekday: 3,
        Period: 1,
        Span: 2,
        Room: "비마관 312호",
      },
      { SubjectID: "c4", SubjectName: "온라인 교양", Online: true },
    ],
  },
  klap_attendance: {
    Term: term,
    Rows: [
      {
        Course: { Name: "자료구조" },
        Sessions: [
          {
            Week: "8주차",
            Slots: [
              { Index: 1, Status: "출석", Date: "10/26" },
              { Index: 2, Status: "출석", Date: "10/28" },
            ],
          },
        ],
      },
    ],
  },
  klap_academic: {
    Year: "2026",
    Events: [
      { Month: "10", Date: "26 ~ 30", Title: "2학기 중간고사", Note: "" },
    ],
  },
  klap_search: { Notices: notices, Assignments: assignments, Errors: [] },
};
http
  .createServer(async (req, res) => {
    res.setHeader("Cache-Control", "no-store");
    const json = (v) => {
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify(v));
    };
    if (req.url === "/api/session")
      return json({ signed_in: true, csrf: "fixture" });
    if (req.url === "/api/call") {
      let raw = "";
      for await (const chunk of req) raw += chunk;
      const { name, arguments: args = {} } = JSON.parse(raw);
      let data = fixtures[name];
      if (["klap_lectures","klap_assignments","klap_notices"].includes(name) && args.intent) {
        const items=name==="klap_lectures" ? lectures.map(({row:r,reason})=>({id:r.ID,kind:"lecture",title:r.Lecture.Title,course_id:r.CourseName,week:r.Lecture.WeekNo,end_at:r.Lecture.EndAt,completion:{status:r.Lecture.Progress==="100"?"completed":"incomplete",progress:Number(r.Lecture.Progress)},availability:"open",attendable:!reason}))
          : name==="klap_assignments" ? assignments.map(r=>({id:r.ID,kind:"assignment",title:r.Assignment.Title,course_id:r.CourseName,end_at:r.Assignment.DueAt,submitted:r.Assignment.Submitted?"submitted":"pending"}))
          : notices.map(r=>({id:r.ID,kind:"notice",title:r.Notice.Title,course_id:r.CourseName,registered_at:r.Notice.Registered,pinned:r.Notice.Top}));
        data={query:{intent:args.intent,term:term.value,as_of:now},coverage:{status:"complete"},selection:{status:"verified",matched_total:items.length},courses:{},items,page:{complete:true,next_cursor:null}};
      }
      if (name === "klap_get_items") data={items:args.ids.map(id=>({id,status:"ok",title:"예시 상세 본문",course_id:"자료구조",fetched_at:now,sections:{content:'UI 검증용 원문입니다.\n<script>alert("실행되지 않음")</script>'}})),page:{complete:true,next_cursor:null}};
      if (name === "klap_state")
        data = {
          connection: {
            connected: true,
            mode: "server",
            apple_sync: false,
            settings_url: "http://127.0.0.1:4173/",
          },
          events: {
            active_subscriptions: 3,
            pending_deliveries: 0,
            last_scan: now,
            recent: [
              {
                name: "notice.created",
                title: "실험 결과 보고서 제출 안내",
                status: "received",
                at: now,
              },
            ],
          },
          jobs,
        };
      if (name.endsWith("_detail"))
        data = {
          CourseName: "자료구조",
          Detail: {
            Title: "예시 상세 본문",
            ContentText:
              '이 화면은 UI 검증을 위한 예시 데이터입니다.\n\n<script>alert("HTML은 실행되지 않습니다")</script>',
            DueAt: due,
          },
          DetailURL: "https://klas.kw.ac.kr/",
        };
      if (name === "klap_start_attendance") {
        data = {
          id: "fixture-job",
          status: "queued",
          items: args.ids.map((id) => ({ id, status: "queued" })),
          progress: {},
          created_at: now,
        };
        jobs = [data];
      }
      if (name === "klap_cancel_attendance") {
        jobs = jobs.map((j) => ({ ...j, status: "cancelled" }));
        data = jobs[0];
      }
      if (!data) {
        res.statusCode = 400;
        return json({ error: "fixture does not implement this operation" });
      }
      return json({ kind: name, data, retrieved_at: now });
    }
    let html = await readFile(
      new URL("../dist/index.html", import.meta.url),
      "utf8",
    );
    html = html.replace(
      "</head>",
      '<style>body:before{content:"UI 검증용 예시 데이터 · 실제 KLAS 아님";display:block;padding:8px;text-align:center;background:#fcf0d5;color:#755224;font:11px system-ui;}</style></head>',
    );
    res.setHeader("Content-Type", "text/html");
    res.end(html);
  })
  .listen(Number(process.env.KLAP_PREVIEW_PORT || 4173), "127.0.0.1", () =>
    console.log(`Fixture preview: http://127.0.0.1:${process.env.KLAP_PREVIEW_PORT || 4173}`),
  );
