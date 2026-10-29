import { displayQuery, queryWarning } from "./agent-query";
import { disassemble, getChoseong } from "es-hangul";

type Data = Record<string, any>;
type Call = (name: string, args: Data) => Promise<{ data: any; retrieved_at: string }>;
const normalize = (text: string) => text.normalize("NFC").toLocaleLowerCase().replace(/\s+/g, "");

export function matchesSearch(query: string, ...values: unknown[]) {
  const needle = normalize(query);
  if (!needle) return false;
  return values.some(value => {
    if (typeof value !== "string") return false;
    const text = normalize(value);
    if (disassemble(text).includes(disassemble(needle))) return true;
    const chars = Array.from(needle), target = Array.from(text);
    return target.some((_, start) => chars.every((char, offset) => {
      const candidate = target[start + offset];
      if (!candidate) return false;
      if (/^[ㄱ-ㅎ]$/.test(char)) return getChoseong(candidate) === char;
      return char === candidate || (offset === chars.length - 1 && disassemble(candidate).startsWith(disassemble(char)));
    }));
  });
}

// Fetch unfiltered candidates: server substring search would discard choseong matches.
export async function searchLearning(query: string, term: string, call: Call) {
  const data: Data = { Notices: [], Assignments: [], Lectures: [], Courses: [], Academics: [], Errors: [] };
  if (!query.trim()) return { data, retrieved_at: "" };
  let retrieved_at = "";
  const sources = [
    ["courses", "Courses", "과목"], ["assignments", "Assignments", "과제"],
    ["notices", "Notices", "공지"], ["lectures", "Lectures", "온라인 강의"],
    ["academic", "Academics", "학사일정"],
  ];
  // KLAS shares a session, so avoid overlapping requests.
  for (const [tool, kind, section] of sources) {
    try {
      const args:Data=term?{term}:{};
      if (["lectures","assignments","notices"].includes(tool)) args.intent="overview";
      const result = await call(`klap_${tool}`, args);
      const warning=queryWarning(result.data);if (warning) data.Errors.push({Section:section,Error:warning});
      result.data=displayQuery(tool,result.data);
      let rows: Data[];
      if (tool === "courses") rows = result.data.flatMap((t: Data) =>
        (!term || t.value === term) ? (t.subjList || []).map((Course: Data) => ({ Course, Term: t })) : []);
      else if (tool === "academic") rows = result.data.Events || [];
      else if (tool === "lectures") rows = result.data.map((r: Data) => r.row || r);
      else rows = result.data;
      data[kind] = rows.filter(r => matchesSearch(query, r.ID, r.CourseName, r.Course?.name, r.Course?.value,
        r.Notice?.Title, r.Notice?.Author, r.Assignment?.Title, r.Lecture?.Title, r.Lecture?.ModuleTitle,
        r.Title, r.Year, r.Month, r.Date, r.Note));
      retrieved_at = result.retrieved_at;
    } catch (error) {
      data.Errors.push({ Section: section, Error: error instanceof Error ? error.message : "조회에 실패했습니다." });
    }
  }
  return { data, retrieved_at };
}

// Share in-flight requests as well as resolved snapshots across keystrokes.
export function createLearningSearch(call: Call) {
  const cache = new Map<string, ReturnType<Call>>();
  const cachedCall: Call = (name, args) => {
    const key = JSON.stringify([name, args]);
    let pending = cache.get(key);
    if (!pending) {
      pending = call(name, args).catch(error => { cache.delete(key); throw error; });
      cache.set(key, pending);
    }
    return pending;
  };
  return (query: string, term: string) => searchLearning(query, term, cachedCall);
}
