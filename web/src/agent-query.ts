type Data = Record<string, any>;

export function reconstructItems(items: Data[]): Data[] {
  const output: Data[] = [];
  let pending: { id: string; total: number; parts: string[] } | undefined;
  for (const item of items) {
    const f = item.item_fragment;
    if (!f) {
      if (pending) throw new Error("분할된 조회 항목이 완성되지 않았습니다.");
      output.push(item); continue;
    }
    if (f.encoding !== "json") throw new Error("지원하지 않는 응답 조각입니다.");
    if (!pending) pending = { id: f.id, total: f.parts, parts: [] };
    if (pending.id !== f.id || pending.total !== f.parts || f.part !== pending.parts.length)
      throw new Error("조회 조각 순서가 일치하지 않습니다.");
    pending.parts.push(f.text);
    if (pending.parts.length === pending.total) {
      output.push(JSON.parse(pending.parts.join(""))); pending = undefined;
    }
  }
  if (pending) throw new Error("분할된 조회 항목이 완성되지 않았습니다.");
  return output;
}

export function queryWarning(data: Data): string {
  const warnings: string[] = [];
  if (data?.coverage?.status === "partial") warnings.push("일부 과목 조회에 실패했습니다. 전체 결과로 판단할 수 없습니다.");
  if (data?.selection?.status === "uncertain") warnings.push("날짜나 완료 상태를 확인하지 못한 후보가 포함되어 있습니다.");
  return warnings.join(" ");
}

// Keep existing display components compatible during the v2 transition.
export function displayQuery(kind: string, data: any): any {
  if (!data?.items || !data?.query) return data;
  const names = data.courses || {};
  if (kind === "due") return { Items:data.items.map((item:Data)=>({ID:item.id,Kind:({lecture:"강의",assignment:"과제",academic:"학사일정"} as Data)[item.kind],Title:item.title,CourseName:names[item.course_id],DueAt:item.end_at,Status:item.completion?.status === "unknown" || item.submitted === "unknown" ? "상태 미확인" : ""})), Errors:[] };
  return data.items.map((item: Data) => {
    const common = { ID: item.id, CourseName: names[item.course_id] || item.course_id, TermValue: data.query.term };
    if (kind === "lectures") return { row: { ...common, Lecture: {
      Title: item.title, WeekNo: item.week, StartAt: item.start_at, EndAt: item.end_at,
      Progress: item.completion?.progress == null ? "?" : String(item.completion.progress),
      AchievedTime: item.completion?.achieved ?? "?", RequiredTime: item.completion?.required ?? "?",
    } }, reason: item.attendable ? "" : item.completion?.status === "completed" ? "수강 완료" : item.completion?.status === "unknown" ? "완료 여부 미확인" : ({ upcoming: "수강 예정", expired: "수강 기간 종료", unknown: "수강 기간 미확인" } as Data)[item.availability] || "자동 수강 미지원" };
    if (kind === "assignments") return { ...common, Assignment: { Title: item.title, StartAt: item.start_at, DueAt: item.end_at, Submitted: item.submitted === "submitted", SubmissionStatus: item.submitted } };
    if (kind === "notices") return { ...common, Notice: { Title: item.title, Registered: item.registered_at, Top: item.pinned } };
    return item;
  });
}
