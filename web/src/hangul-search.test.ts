import { test } from "node:test";
import assert from "node:assert/strict";
import { matchesSearch, searchLearning } from "./hangul-search";

test("Hangul search matches choseong, mixed and incomplete input", () => {
  for (const query of ["ㅈㄹㄱㅈ", "자료ㄱㅈ", "자료구ㅈ", "자료구", "자료 구조", "자료구조".normalize("NFD")])
    assert.equal(matchesSearch(query, "자료구조"), true, query);
  assert.equal(matchesSearch("ㄸㅆ", "따뜻한 쌀"), false);
  assert.equal(matchesSearch("ㄸㄸㅎㅆ", "따뜻한 쌀"), true);
  assert.equal(matchesSearch("Core ML", "CORE ml 실습"), true);
  assert.equal(matchesSearch("ㅈㄹ", "데이터과학"), false);
  assert.equal(matchesSearch("", "자료구조"), false);
  assert.equal(matchesSearch("  ", "자료구조"), false);
  assert.equal(matchesSearch("자료", null, undefined, 12), false);
  assert.equal(matchesSearch(".*", "자료구조"), false);
});

test("search fetches unfiltered sources sequentially and preserves partial errors", async () => {
  const calls: string[] = [];
  const result = await searchLearning("ㅈㄹㄱㅈ", "2026,2", async (name, args) => {
    assert.deepEqual(args, {term:"2026,2"});
    calls.push(name);
    if (name === "klap_notices") throw new Error("공지 조회 실패");
    const data = name === "klap_courses" ? [{value:"2026,2",subjList:[{name:"자료구조"}]}, {value:"2026,1",subjList:[{name:"자료구조"}]}]
      : name === "klap_lectures" ? [{row:{ID:"l",CourseName:"자료구조",Lecture:{Title:"순회"}},reason:"완료"}]
      : name === "klap_academic" ? {Events:[{Title:"개강"}]}
      : [{ID:"a",CourseName:"자료구조",Assignment:{Title:"과제"}}, {CourseName:"데이터과학"}];
    return {data,retrieved_at:"now"};
  });
  assert.deepEqual(calls, ["klap_courses","klap_assignments","klap_notices","klap_lectures","klap_academic"]);
  assert.equal(result.data.Courses.length,1);
  assert.equal(result.data.Assignments.length,1);
  assert.equal(result.data.Lectures[0].ID,"l");
  assert.equal(result.data.Academics.length,0);
  assert.deepEqual(result.data.Errors,[{Section:"공지",Error:"공지 조회 실패"}]);
});

test("empty query performs no network calls; total failure remains visible", async () => {
  const empty = await searchLearning(" ", "", async () => {throw new Error("must not call");});
  assert.equal(empty.data.Errors.length,0);
  const failed = await searchLearning("과제", "", async () => {throw new Error("offline");});
  assert.equal(failed.data.Errors.length,5);
});
