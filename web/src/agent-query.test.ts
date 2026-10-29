import test from "node:test";
import assert from "node:assert/strict";
import { reconstructItems, displayQuery, queryWarning } from "./agent-query";
test("fragments restore original text and reject missing or reordered parts",()=>{
 const text=JSON.stringify({id:"a",content:"한글😀"});const part=(n:number,t:string)=>({item_fragment:{id:"a",encoding:"json",part:n,parts:2,text:t}});
 assert.deepEqual(reconstructItems([part(0,text.slice(0,10)),part(1,text.slice(10))]),[JSON.parse(text)]);
 assert.throws(()=>reconstructItems([part(1,text)]));assert.throws(()=>reconstructItems([part(0,text)]));
});
test("unknown completion cannot become an attendable lecture",()=>{
 const result=displayQuery("lectures",{query:{term:"2026,2"},courses:{c:"과목"},items:[{id:"a",course_id:"c",completion:{status:"unknown"},availability:"open"}]});
 assert.equal(result[0].reason,"완료 여부 미확인");assert.equal(result[0].row.Lecture.Progress,"?");
 assert.match(queryWarning({coverage:{status:"partial"},selection:{status:"uncertain"}}),/전체 결과/);
});
