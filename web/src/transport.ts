import { reconstructItems } from "./agent-query";
import {
  App,
  applyDocumentTheme,
  applyHostStyleVariables,
} from "@modelcontextprotocol/ext-apps";
import { OpenAIExtensions } from "@openai/mcp-extensions/app";
import "@openai/mcp-extensions/app/styles.css";

export type Data = Record<string, any>;
export type Envelope = { kind: string; data: any; retrieved_at: string };
export const embedded = window.parent !== window;
let csrf = "";
let initial: Envelope | undefined;
let app: App | undefined;
let listener: ((value: Envelope) => void) | undefined;
export function onResult(fn: (value: Envelope) => void) {
  listener = fn;
  if (initial) fn(initial);
}
function receive(value: unknown) {
  if (
    !value ||
    typeof value !== "object" ||
    !("kind" in value) ||
    !("data" in value)
  )
    return;
  initial = value as Envelope;
  listener?.(initial);
}
export async function connect() {
  if (!embedded) {
    const session = await fetch("/api/session", { cache: "no-store" }).then(
      (r) => r.json(),
    );
    csrf = session.csrf || "";
    return { signedIn: Boolean(session.signed_in), canMessage: false };
  }
  app = new App({ name: "KLAP", version: "0.1.0" });
  new OpenAIExtensions(app);
  const style = (ctx: ReturnType<App["getHostContext"]>) => {
    if (ctx?.theme) applyDocumentTheme(ctx.theme);
    if (ctx?.styles?.variables) applyHostStyleVariables(ctx.styles.variables);
    if (ctx?.safeAreaInsets)
      for (const [side, value] of Object.entries(ctx.safeAreaInsets))
        document.documentElement.style.setProperty(
          `--safe-${side}`,
          `${value}px`,
        );
  };
  app.ontoolresult = (result) => receive(result.structuredContent);
  app.onhostcontextchanged = style;
  await app.connect();
  style(app.getHostContext());
  return {
    signedIn: true,
    canMessage: Boolean(app.getHostCapabilities()?.message),
  };
}
async function callOnce(name: string, args: Data = {}): Promise<Envelope> {
  if (embedded) {
    if (!app?.getHostCapabilities()?.serverTools)
      throw new Error(
        "이 호스트에서 앱의 도구 호출을 지원하지 않습니다. 채팅에서 KLAP 도구를 요청해 주세요.",
      );
    const result = await app.callServerTool({ name, arguments: args });
    if (result.isError)
      throw new Error(
        result.content
          ?.map((c) => (c.type === "text" ? c.text : ""))
          .join("\n") || "요청에 실패했습니다.",
      );
    const value = result.structuredContent;
    if (!value || typeof value !== "object" || !("data" in value))
      throw new Error("응답 형식을 확인할 수 없습니다.");
    return value as Envelope;
  }
  return ownerRequest("/api/call", { name, arguments: args });
}
export async function ownerRequest(path: string, data: Data = {}) {
  if (embedded)
    throw new Error("연결 설정은 인스턴스의 보안 설정 화면에서 진행해 주세요.");
  const response = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: JSON.stringify(data),
  });
  const text = await response.text();
  let body: Data = {};
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    throw new Error(text || "요청에 실패했습니다.");
  }
  if (!response.ok)
    throw new Error(
      body.error ||
        (response.status === 401
          ? "로그인이 만료되었습니다. 다시 로그인해 주세요."
          : "요청에 실패했습니다."),
    );
  return body as any;
}
export async function openURL(url: string) {
  if (!/^https?:\/\//.test(url)) return;
  if (embedded) {
    if (app?.getHostCapabilities()?.openLinks) await app.openLink({ url });
    else throw new Error("호스트가 링크 열기를 지원하지 않습니다.");
  } else window.open(url, "_blank", "noopener,noreferrer");
}
export async function requestMonitoring(term: string) {
  if (!app?.getHostCapabilities()?.message)
    throw new Error(
      "채팅에서 “KLAP의 새 공지·과제·강의를 감지하면 알려줘”라고 요청해 주세요.",
    );
  const result = await app.sendMessage({
    role: "user",
    content: [
      {
        type: "text",
        text: `KLAP ${term} 학기의 새 공지사항·과제·강의를 MCP Events로 모니터링해 줘. notice.created, assignment.created, lecture.created를 구독하고 새 항목이 생기면 요약해서 알려줘. 첫 조회의 기존 항목은 알리지 마.`,
      },
    ],
  });
  if (result.isError)
    throw new Error("모니터링 요청을 채팅으로 전달하지 못했습니다.");
}

export async function call(name: string, args: Data = {}): Promise<Envelope> {
 const first=await callOnce(name,args);
 if (!first.data?.page) return first;
 const items=[...first.data.items];let page=first.data.page;const seen=new Set<string>();
 while (!page.complete) {
  const cursor=page.next_cursor;if (!cursor || seen.has(cursor)) throw new Error("조회 이어받기 정보가 올바르지 않습니다.");
  seen.add(cursor);const next=await callOnce(name,{cursor});items.push(...next.data.items);page=next.data.page;
 }
 return {...first,data:{...first.data,items:reconstructItems(items),page}};
}
