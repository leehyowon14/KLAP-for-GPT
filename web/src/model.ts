export type TimetableEntry = {
  SubjectID: string;
  SubjectName: string;
  Weekday: number;
  Period: number;
  Span: number;
  Room: string;
  Professor: string;
  Online: boolean;
};
export function timetable(entries: TimetableEntry[]) {
  const grid = new Map<string, TimetableEntry[]>();
  const online: TimetableEntry[] = [];
  const unscheduled: TimetableEntry[] = [];
  let start = 1,
    end = 6,
    days = 5;
  for (const e of entries) {
    if (e.Online) {
      online.push(e);
      continue;
    }
    if (
      !Number.isInteger(e.Weekday) ||
      e.Weekday < 1 ||
      e.Weekday > 7 ||
      !Number.isInteger(e.Period) ||
      e.Period < 0 ||
      e.Period > 11
    ) {
      unscheduled.push(e);
      continue;
    }
    const span = Math.min(12 - e.Period, Math.max(1, Math.floor(e.Span || 1)));
    start = Math.min(start, e.Period);
    end = Math.max(end, e.Period + span - 1);
    days = Math.max(days, e.Weekday);
    for (let p = e.Period; p < e.Period + span; p++) {
      const key = `${e.Weekday}:${p}`;
      grid.set(key, [...(grid.get(key) || []), e]);
    }
  }
  return { grid, online, unscheduled, start, end, days };
}
export function safeURL(raw: unknown): string | undefined {
  if (typeof raw !== "string") return;
  try {
    const u = new URL(raw);
    if (u.protocol === "https:" && !u.username && !u.password) return u.href;
  } catch {}
}
export function selectedEligible(
  selected: Set<string>,
  rows: { row: { ID: string }; reason: string }[],
) {
  return rows
    .filter((r) => !r.reason && selected.has(r.row.ID))
    .map((r) => r.row.ID);
}
export function formatDate(raw: unknown, time = true) {
  if (typeof raw !== "string" || !raw || raw.startsWith("0001-")) return "—";
  const date = new Date(raw);
  if (!Number.isFinite(date.valueOf())) return "—";
  return new Intl.DateTimeFormat("ko-KR", {
    month: "short",
    day: "numeric",
    ...(time ? ({ hour: "2-digit", minute: "2-digit" } as const) : {}),
    timeZone: "Asia/Seoul",
  }).format(date);
}
