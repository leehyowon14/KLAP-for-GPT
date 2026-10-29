import { test } from "node:test";
import assert from "node:assert/strict";
import { timetable, safeURL, selectedEligible, formatDate, paginate } from "./model";
const entry = {
  SubjectID: "a",
  SubjectName: "수업",
  Weekday: 1,
  Period: 1,
  Span: 1,
  Room: "비마관",
  Professor: "",
  Online: false,
};
test("timetable preserves overlaps, period zero, weekends, spanning lessons, and unplaced entries", () => {
  const result = timetable([
    entry,
    { ...entry, SubjectID: "b", Period: 0, Span: 3 },
    { ...entry, Weekday: 6, Period: 10, Span: 2 },
    { ...entry, Online: true },
    { ...entry, Weekday: 9 },
  ]);
  assert.equal(result.grid.get("1:1")?.length, 2);
  assert.equal(result.start, 0);
  assert.equal(result.end, 11);
  assert.equal(result.days, 6);
  assert.equal(result.online.length, 1);
  assert.equal(result.unscheduled.length, 1);
  assert.equal(timetable([]).grid.size, 0);
});
test("school URLs never execute script or expose URL credentials", () => {
  for (const url of [
    "javascript:alert(1)",
    "data:text/html,a",
    "http://klas.kw.ac.kr",
    "https://user:pass@klas.kw.ac.kr",
    null,
  ])
    assert.equal(safeURL(url), undefined);
  assert.equal(
    safeURL("https://klas.kw.ac.kr/a?q=1"),
    "https://klas.kw.ac.kr/a?q=1",
  );
});
test("a stale selection cannot attend a removed or newly ineligible lecture", () => {
  assert.deepEqual(
    selectedEligible(new Set(["a", "b", "removed"]), [
      { row: { ID: "a" }, reason: "" },
      { row: { ID: "b" }, reason: "완료" },
    ]),
    ["a"],
  );
  assert.equal(formatDate("not a date"), "—");
  assert.equal(formatDate("0001-01-01T00:00:00Z"), "—");
});

test("event pagination handles empty, exact, partial and stale pages", () => {
  assert.deepEqual(paginate([], 1), {items: [], page: 1, pages: 1});
  assert.equal(paginate([1,2,3,4,5], 1).pages, 1);
  assert.deepEqual(paginate([1,2,3,4,5,6], 2), {items: [6], page: 2, pages: 2});
  assert.equal(paginate([1], 9).page, 1);
  for (const page of [-1, 0, NaN, Infinity]) assert.equal(paginate([1], page).page, 1);
});
