import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  call,
  connect,
  embedded,
  onResult,
  openURL,
  ownerRequest,
  requestMonitoring,
  type Data,
  type Envelope,
} from "./transport";
import {
  formatDate,
  safeURL,
  selectedEligible,
  paginate,
  timetable,
  type TimetableEntry,
} from "./model";
import { Icon, BrandLogo, AssetCredits } from "./icons";
import "./style.css";

type View =
  | "home"
  | "assignments"
  | "lectures"
  | "notices"
  | "timetable"
  | "attendance"
  | "academic"
  | "monitor"
  | "settings"
  | "search";
const nav: [View, string, string][] = [
  ["home", "대시보드", "home"],
  ["assignments", "과제", "check"],
  ["lectures", "온라인 강의", "play"],
  ["notices", "공지사항", "bell"],
  ["timetable", "시간표", "grid"],
  ["attendance", "출석 현황", "chart"],
  ["academic", "학사일정", "calendar"],
  ["monitor", "새 소식 모니터링", "activity"],
];
const statusName: Data = {
  queued: "대기",
  running: "수강 중",
  completed: "완료",
  failed: "실패",
  cancelled: "취소됨",
  interrupted: "중단됨",
  received: "호스트 수신",
  retrying: "재시도 대기",
  delivered: "호스트 수신",
};
function Empty({
  title,
  description,
}: {
  title: string;
  description?: string;
}) {
  return (
    <div className="empty">
      <span className="empty-mark">
        <Icon name="done" />
      </span>
      <h3>{title}</h3>
      {description && <p>{description}</p>}
    </div>
  );
}
function Problems({ items }: { items?: Data[] }) {
  return (
    <>
      {(items || []).map((e, i) => (
        <p role="alert" className="alert" key={i}>
          {e.Section || e.section}:{" "}
          {typeof e.Err === "string"
            ? e.Err
            : e.Error || e.error || "일부 정보를 가져오지 못했습니다."}
        </p>
      ))}
    </>
  );
}
function App() {
  const [ready, setReady] = useState(false),
    [signedIn, setSignedIn] = useState(false),
    [canMessage, setCanMessage] = useState(false);
  const [state, setState] = useState<Data>(),
    [view, setView] = useState<View>("home"),
    [term, setTerm] = useState(""),
    [course, setCourse] = useState(""),
    [terms, setTerms] = useState<Data[]>([]);
  const [data, setData] = useState<any>(),
    [loading, setLoading] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [updated, setUpdated] = useState("");
  const [query, setQuery] = useState(""),
    [search, setSearch] = useState(""),
    [revision, setRevision] = useState(0),
    [selected, setSelected] = useState(new Set<string>());
  const [detail, setDetail] = useState<Data>(),
    [detailBusy, setDetailBusy] = useState(false),
    [sync, setSync] = useState<Data>();
  const [eventPage, setEventPage] = useState(1);
  const eventHistory = paginate<Data>(state?.events?.recent || [], eventPage);
  const initialized = useRef(false);
  const detailElement = useRef<HTMLElement>(null);
  const detailRequest = useRef(0);
  const closeDetail = () => {
    detailRequest.current++;
    setDetail(undefined);
  };
  useEffect(() => {
    if (!detail) return;
    const previous = document.activeElement as HTMLElement | null;
    const element = detailElement.current;
    const focusable = () =>
      Array.from(
        element?.querySelectorAll<HTMLElement>(
          'button:not(:disabled), a[href], select, input, [tabindex="0"]',
        ) || [],
      );
    focusable()[0]?.focus();
    const keydown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeDetail();
      } else if (event.key === "Tab") {
        const items = focusable();
        const first = items[0],
          last = items[items.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener("keydown", keydown);
    return () => {
      document.removeEventListener("keydown", keydown);
      previous?.focus();
    };
  }, [Boolean(detail)]);
  const attendanceRequest = useRef<{ signature: string; key: string } | null>(
    null,
  );
  const connected = Boolean(state?.connection?.connected);
  const apply = (r: Envelope) => {
    if (["klap_state", "klap_open", "klap_home"].includes(r.kind)) {
      setState(r.data);
      setUpdated(r.retrieved_at);
      initialized.current = true;
    }
  };
  const refreshState = async () => {
    const r = await call("klap_state");
    apply(r);
  };
  useEffect(() => {
    let active = true;
    onResult((r) => {
      if (active) apply(r);
    });
    connect()
      .then(async (c) => {
        if (!active) return;
        setSignedIn(c.signedIn);
        setCanMessage(c.canMessage);
        setReady(true);
        if (c.signedIn && !initialized.current) await refreshState();
      })
      .catch((e) => {
        setReady(true);
        setError(e.message);
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => {
    if (!signedIn || !ready) return;
    const timer = setInterval(
      () => refreshState().catch(() => {}),
      state?.jobs?.some((j: Data) => ["queued", "running"].includes(j.status))
        ? 3000
        : 30000,
    );
    return () => clearInterval(timer);
  }, [
    ready,
    signedIn,
    state?.jobs?.some((j: Data) => ["queued", "running"].includes(j.status)),
  ]);
  useEffect(() => {
    if (!connected) return;
    let active = true;
    call("klap_terms")
      .then((r) => {
        if (active) setTerms(r.data || []);
      })
      .catch((e) => {
        if (active) setError(e.message);
      });
    return () => {
      active = false;
    };
  }, [connected]);
  useEffect(() => {
    setSelected(new Set());
    setCourse("");
  }, [term]);
  useEffect(() => {
    if (
      !ready ||
      !signedIn ||
      !connected ||
      ["monitor", "settings"].includes(view)
    )
      return;
    let active = true;
    setData(undefined);
    setError("");
    setLoading(true);
    const tool = view === "home" ? "due" : view;
    const args: Data = term ? { term } : {};
    if (["notices", "assignments", "lectures"].includes(view) && course)
      args.course_id = course;
    if (view === "search") args.query = search;
    call(`klap_${tool}`, args)
      .then((r) => {
        if (active) {
          setData(r.data);
          setUpdated(r.retrieved_at);
        }
      })
      .catch((e) => {
        if (active) setError(e.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [view, term, course, revision, ready, signedIn, connected, search]);
  async function action(work: () => Promise<void>) {
    setError("");
    setNotice("");
    setBusy(true);
    try {
      await work();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function showDetail(kind: string, id: string) {
    const request = ++detailRequest.current;
    setDetailBusy(true);
    setDetail({});
    try {
      const r = await call(`klap_${kind}_detail`, { id });
      if (request === detailRequest.current) setDetail(r.data);
    } catch (e) {
      if (request === detailRequest.current)
        setDetail({ error: (e as Error).message });
    } finally {
      if (request === detailRequest.current) setDetailBusy(false);
    }
  }
  const go = (next: View) => {
    setView(next);
    setError("");
    setNotice("");
    setData(undefined);
  };
  const jobs: Data[] = state?.jobs || [];
  const activeJobs = jobs.filter((j) =>
    ["queued", "running"].includes(j.status),
  );
  const selectedTerm =
    terms.find((t) => t.Term?.value === term) ||
    terms.find((t) => t.Current) ||
    terms[0];
  const courses: Data[] = selectedTerm?.Term?.subjList || [];
  const heading =
    nav.find((n) => n[0] === view)?.[1] ||
    (view === "settings" ? "연결 설정" : "검색 결과");
  if (!ready)
    return (
      <div className="boot" role="status">
        <BrandLogo />
        <p>KLAP을 연결하고 있어요.</p>
      </div>
    );
  if (!signedIn && !embedded)
    return (
      <main className="welcome">
        <BrandLogo />
        <span className="eyebrow">YOUR CAMPUS, CONNECTED</span>
        <h1>
          대학 생활을
          <br />
          한곳에서.
        </h1>
        <p>
          오늘의 과제부터 새로운 공지까지.
          <br />
          나의 KLAS를 KLAP과 연결하세요.
        </p>
        <a className="primary button" href="/login">
          내 인스턴스에 로그인 <Icon name="arrow" />
        </a>
        <small>개인 서버의 소유자 계정으로 로그인합니다.</small>
        {error && <p className="alert">{error}</p>}
      </main>
    );
  return (
    <div className="layout">
      <aside className="sidebar">
        <button className="brand" onClick={() => go("home")}>
          <BrandLogo />
          <span>
            KLAP<small>나의 캠퍼스</small>
          </span>
        </button>
        <div className="nav-label">나의 수업</div>
        <nav aria-label="메인 메뉴">
          {nav.map(([id, label, icon]) => (
            <button
              key={id}
              className={view === id ? "active" : ""}
              aria-current={view === id ? "page" : undefined}
              onClick={() => go(id)}
            >
              <Icon name={icon} />
              {label}
              {id === "lectures" && activeJobs.length > 0 && (
                <span className="nav-count">{activeJobs.length}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className="core-mark">
            <span className={`dot ${connected ? "online" : ""}`} />
            {connected ? "KLAS 연결됨" : "KLAS 연결 필요"}
            <small>수업의 흐름을 한곳에서</small>
          </div>
          <button
            className={view === "settings" ? "active" : ""}
            onClick={() => go("settings")}
          >
            <Icon name="settings" />
            연결 설정
          </button>
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <span className="breadcrumb">
            나의 캠퍼스 <span>/</span> {heading}
          </span>
          <form
            className="search"
            onSubmit={(e) => {
              e.preventDefault();
              if (query.trim()) {
                setSearch(query.trim());
                go("search");
              }
            }}
          >
            <Icon name="search" />
            <input
              aria-label="수업 자료 검색"
              placeholder="공지, 과제, 강의 검색"
              value={query}
              maxLength={200}
              onChange={(e) => setQuery(e.target.value)}
            />
          </form>
          <button
            className="icon-button"
            aria-label="새로고침"
            disabled={loading || busy}
            onClick={() =>
              action(async () => {
                await refreshState();
                setRevision((v) => v + 1);
              })
            }
          >
            <Icon name="refresh" />
          </button>
        </header>
        <main className="content">
          <div className="page-heading">
            <div>
              <span className="eyebrow">
                {view === "home" ? "나의 캠퍼스" : "캠퍼스 라이프"}
              </span>
              <h1>{view === "home" ? "대시보드" : heading}</h1>
              <p>
                {view === "home"
                  ? "마감과 새 소식, 수업의 흐름을 함께 확인하세요."
                  : view === "monitor"
                    ? "새로운 공지·과제·강의를 감지하고 에이전트에게 전달합니다."
                    : view === "settings"
                      ? "학교 계정과 이 인스턴스의 연결을 관리합니다."
                      : selectedTerm?.Term?.label ||
                        "연결된 KLAS의 최신 정보를 확인합니다."}
              </p>
            </div>
            {!["settings", "monitor"].includes(view) && connected && (
              <select
                aria-label="학기 선택"
                value={term}
                onChange={(e) => setTerm(e.target.value)}
              >
                <option value="">기본 학기</option>
                {terms.map((t: Data) => (
                  <option key={t.Term.value} value={t.Term.value}>
                    {t.Term.label}
                  </option>
                ))}
              </select>
            )}
          </div>
          {error && (
            <div role="alert" className="alert">
              {error}
              <button aria-label="오류 닫기" onClick={() => setError("")}>
                <Icon name="close" />
              </button>
            </div>
          )}
          {notice && (
            <p role="status" className="success">
              {notice}
            </p>
          )}
          {!connected && view !== "settings" ? (
            <section className="card connect-card">
              <div className="connect-symbol">
                <Icon name="grid" />
              </div>
              <h2>나의 수업을 연결해 볼까요?</h2>
              <p>
                KLAS 계정을 연결하면 과제, 강의, 시간표를
                <br />이 화면에서 함께 볼 수 있어요.
              </p>
              <button className="primary" onClick={() => go("settings")}>
                KLAS 연결하기 <Icon name="arrow" />
              </button>
            </section>
          ) : (
            <>
              {connected &&
                ["assignments", "lectures", "notices"].includes(view) &&
                courses.length > 0 && (
                  <div className="filters">
                    <button
                      className={!course ? "chip selected" : "chip"}
                      onClick={() => setCourse("")}
                    >
                      전체 과목
                    </button>
                    {courses.map((c) => (
                      <button
                        key={c.value}
                        className={
                          course === c.value ? "chip selected" : "chip"
                        }
                        onClick={() => setCourse(c.value)}
                      >
                        {c.name}
                      </button>
                    ))}
                  </div>
                )}
              {loading ? (
                <div className="loading" role="status">
                  <div />
                  <div />
                  <div />
                  <span>최신 정보를 가져오고 있어요…</span>
                </div>
              ) : (
                <>
                  {view === "home" && data && (
                    <>
                      <div className="stats dashboard-stats">
                        <Stat
                          label="다가오는 일정"
                          value={(data.Items || []).length}
                          note="앞으로 14일"
                          icon="calendar"
                        />
                        <Stat
                          label="진행 중인 수강"
                          value={activeJobs.length}
                          note="대기 · 수강 중"
                          icon="play"
                        />
                      </div>
                      <section className="card">
                        <div className="section-title">
                          <h2>
                            다가오는 일정{" "}
                            <span>{(data.Items || []).length}</span>
                          </h2>
                          <span className="muted">14일 이내</span>
                        </div>
                        <Problems items={data.Errors} />
                        {data.Items?.length ? (
                          <div className="rows">
                            {data.Items.map((r: Data, i: number) => (
                              <div className="row" key={r.ID || i}>
                                <span
                                  className={`kind ${r.Kind === "과제" ? "pink" : "blue"}`}
                                >
                                  <Icon
                                    name={
                                      r.Kind === "과제"
                                        ? "check"
                                        : r.Kind === "학사일정"
                                          ? "calendar"
                                          : "play"
                                    }
                                  />
                                </span>
                                <div className="row-main">
                                  <small>{r.CourseName || r.Kind}</small>
                                  <strong>{r.Title}</strong>
                                </div>
                                <div className="row-end">
                                  <span>{formatDate(r.DueAt)}</span>
                                  <small>{r.Status}</small>
                                </div>
                                {r.Kind === "과제" && (
                                  <button
                                    className="icon-button"
                                    aria-label={`${r.Title} 상세`}
                                    onClick={() =>
                                      showDetail("assignment", r.ID)
                                    }
                                  >
                                    <Icon name="arrow" />
                                  </button>
                                )}
                              </div>
                            ))}
                          </div>
                        ) : (
                          <Empty
                            title="다가오는 일정이 없어요"
                            description="앞으로 14일 동안의 과제와 강의 마감을 확인했습니다."
                          />
                        )}
                      </section>
                      <div className="home-bottom">
                        <button
                          className="shortcut card"
                          onClick={() => go("timetable")}
                        >
                          <Icon name="grid" />
                          <span>
                            <strong>이번 학기 시간표</strong>
                            <small>요일과 교시별로 한눈에</small>
                          </span>
                          <Icon name="arrow" />
                        </button>
                        <button
                          className="shortcut card"
                          onClick={() => go("monitor")}
                        >
                          <Icon name="activity" />
                          <span>
                            <strong>새 소식을 놓치지 않도록</strong>
                            <small>에이전트에게 모니터링 맡기기</small>
                          </span>
                          <Icon name="arrow" />
                        </button>
                      </div>
                    </>
                  )}
                  {view === "assignments" && data && (
                    <section className="card">
                      <div className="section-title">
                        <h2>
                          과제 <span>{data.length}</span>
                        </h2>
                      </div>
                      {data.length ? (
                        data.map((r: Data) => (
                          <button
                            className="row full-row"
                            key={r.ID}
                            onClick={() => showDetail("assignment", r.ID)}
                          >
                            <span
                              className={`kind ${r.Assignment.Submitted ? "green" : "pink"}`}
                            >
                              <Icon name="check" />
                            </span>
                            <div className="row-main">
                              <small>{r.CourseName}</small>
                              <strong>{r.Assignment.Title}</strong>
                              <small>
                                마감 {formatDate(r.Assignment.DueAt)}
                              </small>
                            </div>
                            <span
                              className={`badge ${r.Assignment.Submitted ? "green" : "neutral"}`}
                            >
                              {r.Assignment.Submitted ? "제출 완료" : "미제출"}
                            </span>
                            <Icon name="arrow" />
                          </button>
                        ))
                      ) : (
                        <Empty title="등록된 과제가 없어요" />
                      )}
                    </section>
                  )}
                  {view === "notices" && data && (
                    <section className="card">
                      <div className="section-title">
                        <h2>
                          공지사항 <span>{data.length}</span>
                        </h2>
                      </div>
                      {data.length ? (
                        data.map((r: Data) => (
                          <button
                            className="row full-row"
                            key={r.ID}
                            onClick={() => showDetail("notice", r.ID)}
                          >
                            <span className="kind neutral">
                              <Icon name="bell" />
                            </span>
                            <div className="row-main">
                              <small>{r.CourseName}</small>
                              <strong>
                                {r.Notice.Top && (
                                  <span className="pin">고정</span>
                                )}
                                {r.Notice.Title}
                              </strong>
                              <small>{r.Notice.Author}</small>
                            </div>
                            <div className="row-end">
                              <span>
                                {formatDate(r.Notice.Registered, false)}
                              </span>
                            </div>
                            <Icon name="arrow" />
                          </button>
                        ))
                      ) : (
                        <Empty title="등록된 공지사항이 없어요" />
                      )}
                    </section>
                  )}
                  {view === "lectures" && data && (
                    <>
                      <section className="card">
                        <div className="section-title">
                          <h2>
                            온라인 강의 <span>{data.length}</span>
                          </h2>
                          <button
                            disabled={
                              busy ||
                              selectedEligible(selected, data).length === 0
                            }
                            className="primary"
                            onClick={() =>
                              action(async () => {
                                const ids = selectedEligible(selected, data);
                                const signature = JSON.stringify(ids);
                                if (
                                  attendanceRequest.current?.signature !==
                                  signature
                                ) {
                                  attendanceRequest.current = {
                                    signature,
                                    key: crypto.randomUUID(),
                                  };
                                }
                                await call("klap_start_attendance", {
                                  ids,
                                  idempotency_key:
                                    attendanceRequest.current.key,
                                });
                                attendanceRequest.current = null;
                                setSelected(new Set());
                                await refreshState();
                                setNotice(
                                  "수강 작업을 등록했습니다. 아래에서 진행 상태를 확인할 수 있어요.",
                                );
                              })
                            }
                          >
                            {selectedEligible(selected, data).length || ""} 선택
                            수강
                          </button>
                        </div>
                        {data.length ? (
                          data.map(
                            ({
                              row: r,
                              reason,
                            }: {
                              row: Data;
                              reason: string;
                            }) => (
                              <label
                                className={`row lecture ${reason ? "unavailable" : ""}`}
                                key={r.ID}
                              >
                                <input
                                  aria-label={`${r.Lecture.Title} 선택`}
                                  type="checkbox"
                                  disabled={Boolean(reason) || busy}
                                  checked={selected.has(r.ID)}
                                  onChange={(e) =>
                                    setSelected((prev) => {
                                      const next = new Set(prev);
                                      if (e.target.checked) next.add(r.ID);
                                      else next.delete(r.ID);
                                      return next;
                                    })
                                  }
                                />
                                <div className="row-main">
                                  <small>
                                    {r.CourseName} · {r.Lecture.WeekNo}주차
                                  </small>
                                  <strong>
                                    {r.Lecture.Title || r.Lecture.ModuleTitle}
                                  </strong>
                                  <small>
                                    {reason ||
                                      `수강 기한 ${formatDate(r.Lecture.EndAt)}`}
                                  </small>
                                </div>
                                <div className="row-end">
                                  <strong>{r.Lecture.Progress || "0"}%</strong>
                                  <small>
                                    {r.Lecture.AchievedTime} /{" "}
                                    {r.Lecture.RequiredTime}
                                  </small>
                                </div>
                              </label>
                            ),
                          )
                        ) : (
                          <Empty title="온라인 강의가 없어요" />
                        )}
                      </section>
                      <JobList
                        jobs={jobs}
                        busy={busy}
                        cancel={(id) =>
                          action(async () => {
                            await call("klap_cancel_attendance", {
                              job_id: id,
                            });
                            await refreshState();
                          })
                        }
                      />
                    </>
                  )}
                  {view === "timetable" && data && (
                    <Timetable entries={data.Entries || []} />
                  )}
                  {view === "attendance" && data && (
                    <section className="card">
                      <div className="section-title">
                        <h2>과목별 출석</h2>
                      </div>
                      {data.Rows?.length ? (
                        data.Rows.map((r: Data, i: number) => (
                          <div className="attendance-course" key={i}>
                            <h3>
                              {r.Course?.Name ||
                                r.Course?.SubjectName ||
                                `과목 ${i + 1}`}
                            </h3>
                            {r.Error ? (
                              <p className="alert">{r.Error}</p>
                            ) : (
                              <div className="attendance-sessions">
                                {(r.Sessions || []).map(
                                  (s: Data, n: number) => (
                                    <div key={n}>
                                      <strong>
                                        {s.Date || s.Week || `${n + 1}회차`}
                                      </strong>
                                      <span>
                                        {s.Status ||
                                          s.Label ||
                                          s.Attendance ||
                                          "상태 확인"}
                                      </span>
                                      {(s.Slots || []).map(
                                        (slot: Data, j: number) => (
                                          <small key={j}>
                                            {slot.Date || `${slot.Index}교시`}{" "}
                                            {slot.Status || slot.Result}
                                          </small>
                                        ),
                                      )}
                                    </div>
                                  ),
                                )}
                              </div>
                            )}
                          </div>
                        ))
                      ) : (
                        <Empty title="출석 정보가 없어요" />
                      )}
                    </section>
                  )}
                  {view === "academic" && data && (
                    <section className="card">
                      <div className="section-title">
                        <h2>학사일정</h2>
                        <span className="muted">{data.Year}</span>
                      </div>
                      {data.Events?.length ? (
                        data.Events.map((e: Data, i: number) => (
                          <div className="row" key={i}>
                            <div className="calendar-date">
                              <strong>{e.Month}월</strong>
                              <small>{e.Date}</small>
                            </div>
                            <div className="row-main">
                              <strong>{e.Title}</strong>
                              {e.Note && <small>{e.Note}</small>}
                            </div>
                          </div>
                        ))
                      ) : (
                        <Empty title="학사일정이 없어요" />
                      )}
                    </section>
                  )}
                  {view === "search" && data && (
                    <section className="card">
                      <div className="section-title">
                        <h2>“{search}” 검색 결과</h2>
                      </div>
                      <Problems items={data.Errors} />
                      {[
                        "Notices",
                        "Assignments",
                        "Lectures",
                        "Courses",
                        "Academics",
                      ].flatMap((kind) =>
                        (data[kind] || []).map((r: Data, i: number) => (
                          <div className="row" key={`${kind}-${i}`}>
                            <div className="row-main">
                              <small>
                                {r.CourseName || r.Course?.name || kind}
                              </small>
                              <strong>
                                {r.Notice?.Title ||
                                  r.Assignment?.Title ||
                                  r.Lecture?.Title ||
                                  r.Course?.name ||
                                  r.Title}
                              </strong>
                            </div>
                            {["Notices", "Assignments"].includes(kind) && (
                              <button
                                className="icon-button"
                                aria-label="검색 결과 상세"
                                onClick={() =>
                                  showDetail(
                                    kind === "Notices"
                                      ? "notice"
                                      : "assignment",
                                    r.ID,
                                  )
                                }
                              >
                                <Icon name="arrow" />
                              </button>
                            )}
                          </div>
                        )),
                      )}
                      {![
                        "Notices",
                        "Assignments",
                        "Lectures",
                        "Courses",
                        "Academics",
                      ].some((k) => data[k]?.length) && (
                        <Empty title="검색 결과가 없어요" />
                      )}
                    </section>
                  )}
                </>
              )}
              {view === "monitor" && (
                <>
                  <div className="monitor-banner">
                    <span className="kind green">
                      <Icon name="activity" />
                    </span>
                    <div>
                      <h2>새 소식이 생기면, 에이전트가 이어서</h2>
                      <p>
                        현재 학기의 새 공지·과제·강의를 5분 간격으로 확인합니다.
                        <br />
                        처음 발견한 기존 항목은 기준선으로 저장합니다.
                      </p>
                    </div>
                    {canMessage ? (
                      <button
                        className="primary"
                        disabled={busy}
                        onClick={() =>
                          action(async () => {
                            await requestMonitoring(
                              term || selectedTerm?.Term?.value || "현재",
                            );
                            setNotice(
                              "채팅에 모니터링 설정을 요청했습니다. 구독 수가 반영되는지 확인해 주세요.",
                            );
                          })
                        }
                      >
                        모니터링 요청
                      </button>
                    ) : (
                      <button
                        onClick={() => setNotice('ChatGPT에서 KLAP을 연결한 뒤 “KLAP의 새 공지·과제·강의를 감지하면 알려줘”라고 요청해 주세요.')}
                      >모니터링 설정 안내</button>
                    )}
                  </div>
                  <div className="stats">
                    <Stat
                      label="활성 구독"
                      value={state?.events?.active_subscriptions || 0}
                      icon="bell"
                    />
                    <Stat
                      label="전달 대기"
                      value={state?.events?.pending_deliveries || 0}
                      icon="activity"
                    />
                    <Stat
                      label="최근 확인"
                      value={formatDate(state?.events?.last_scan)}
                      icon="refresh"
                    />
                  </div>
                  {state?.events?.last_error && (
                    <p className="alert">{state.events.last_error}</p>
                  )}
                  <section className="card">
                    <div className="section-title">
                      <h2>이벤트 전달 기록</h2>
                    </div>
                    {state?.events?.recent?.length ? (
                      eventHistory.items.map((r: Data, i: number) => (
                        <div className="row" key={i}>
                          <span className="kind neutral">
                            <Icon name="bell" />
                          </span>
                          <div className="row-main">
                            <small>{r.name}</small>
                            <strong>{r.title}</strong>
                            <small>{formatDate(r.at)}</small>
                          </div>
                          <span className="badge neutral">
                            {statusName[r.status] || r.status}
                          </span>
                        </div>
                      ))
                    ) : (
                      <Empty
                        title="아직 전달한 새 소식이 없어요"
                        description="구독 후 새로 등록되는 항목이 여기에 표시됩니다."
                      />
                    )}
                    {eventHistory.pages > 1 && (
                      <nav className="pagination" aria-label="이벤트 기록 페이지">
                        <button disabled={eventHistory.page === 1} onClick={() => setEventPage(eventHistory.page - 1)}>이전</button>
                        <span aria-live="polite">{eventHistory.page} / {eventHistory.pages}</span>
                        <button disabled={eventHistory.page === eventHistory.pages} onClick={() => setEventPage(eventHistory.page + 1)}>다음</button>
                      </nav>
                    )}
                  </section>
                </>
              )}
              {view === "settings" && (
                <Settings
                  connected={connected}
                  state={state}
                  busy={busy}
                  action={action}
                  refresh={refreshState}
                  sync={sync}
                  setSync={setSync}
                />
              )}
            </>
          )}
          <footer>
            <AssetCredits />
            <span>
              KLAP · 나의 학습 공간
            </span>
            {updated && <span>{formatDate(updated)} 조회</span>}
          </footer>
        </main>
      </div>
      {detail && (
        <div className="modal-backdrop" onClick={closeDetail}>
          <section
            ref={detailElement}
            role="dialog"
            aria-modal="true"
            aria-label="상세 내용"
            className="detail card"
            onClick={(e) => e.stopPropagation()}
          >
            <button
              className="icon-button modal-close"
              aria-label="상세 닫기"
              onClick={closeDetail}
            >
              <Icon name="close" />
            </button>
            {detailBusy ? (
              <p role="status">상세 내용을 불러오고 있어요…</p>
            ) : detail.error ? (
              <p role="alert" className="alert">
                {detail.error}
              </p>
            ) : (
              <>
                <small className="eyebrow">{detail.CourseName}</small>
                <h2>{detail.Detail?.Title}</h2>
                <p className="muted">
                  {formatDate(
                    detail.Detail?.DueAt || detail.Detail?.Registered,
                  )}
                </p>
                <div className="body-text">
                  {detail.Detail?.ContentText || "본문이 비어 있습니다."}
                </div>
                {detail.Detail?.SubmittedText && (
                  <>
                    <h3>제출 내용</h3>
                    <div className="body-text">
                      {detail.Detail.SubmittedText}
                    </div>
                  </>
                )}
                {safeURL(detail.DetailURL) && (
                  <button
                    onClick={() =>
                      openURL(safeURL(detail.DetailURL)!).catch((e) =>
                        setError(e.message),
                      )
                    }
                  >
                    KLAS에서 보기 <Icon name="arrow" />
                  </button>
                )}
              </>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
function Stat({
  label,
  value,
  note,
  icon,
}: {
  label: string;
  value: React.ReactNode;
  note?: string;
  icon: string;
}) {
  return (
    <div className="stat card">
      <span>
        {label}
        <Icon name={icon} />
      </span>
      <strong className={typeof value === "number" ? "" : "stat-text"}>
        {value}
      </strong>
      {note && <small>{note}</small>}
    </div>
  );
}
function JobList({
  jobs,
  busy,
  cancel,
}: {
  jobs: Data[];
  busy: boolean;
  cancel: (id: string) => void;
}) {
  return (
    <section className="card job-list">
      <div className="section-title">
        <h2>수강 작업</h2>
        <span className="muted">최근 요청</span>
      </div>
      {jobs.length ? (
        jobs.slice(0, 12).map((j) => (
          <div className="job" key={j.id}>
            <div className="job-heading">
              <span
                className={`badge ${j.status === "completed" ? "green" : "neutral"}`}
              >
                {statusName[j.status] || j.status}
              </span>
              <strong>{j.progress.title || `${j.items.length}개 강의`}</strong>
              <small>{formatDate(j.created_at)}</small>
              {["queued", "running"].includes(j.status) && (
                <button
                  disabled={busy || j.cancel_requested}
                  onClick={() => cancel(j.id)}
                >
                  {j.cancel_requested ? "취소 요청됨" : "취소"}
                </button>
              )}
            </div>
            <progress
              max="100"
              value={
                j.status === "completed"
                  ? 100
                  : Math.max(0, Math.min(100, j.progress.percent || 0))
              }
            />
            <p className="muted">
              {j.items.filter((i: Data) => i.status === "completed").length} /{" "}
              {j.items.length}개 완료
            </p>
            {j.error && <p className="alert">{j.error}</p>}
            {j.items
              .filter((i: Data) => i.error)
              .map((i: Data, n: number) => (
                <p className="alert" key={n}>
                  {i.error}
                </p>
              ))}
          </div>
        ))
      ) : (
        <Empty
          title="요청한 수강 작업이 없어요"
          description="수강할 강의를 선택하고 ‘선택 수강’을 눌러 주세요."
        />
      )}
    </section>
  );
}
function Timetable({ entries }: { entries: TimetableEntry[] }) {
  const t = timetable(entries);
  return (
    <section className="card">
      <div className="section-title">
        <h2>주간 시간표</h2>
        <span className="muted">요일 × 교시</span>
      </div>
      {entries.length ? (
        <>
          <div className="timetable-scroll">
            <table className="timetable">
              <thead>
                <tr>
                  <th>교시</th>
                  {Array.from({ length: t.days }, (_, i) => (
                    <th key={i}>
                      {["월", "화", "수", "목", "금", "토", "일"][i]}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {Array.from(
                  { length: t.end - t.start + 1 },
                  (_, i) => i + t.start,
                ).map((p) => (
                  <tr key={p}>
                    <th>
                      {p}
                      <small>교시</small>
                    </th>
                    {Array.from({ length: t.days }, (_, i) => i + 1).map(
                      (day) => (
                        <td key={day}>
                          {(t.grid.get(`${day}:${p}`) || []).map((e, n) => (
                            <div
                              className={`class-block color-${Math.abs([...e.SubjectID].reduce((a, c) => a + c.charCodeAt(0), 0)) % 4}`}
                              key={`${e.SubjectID}-${n}`}
                            >
                              <strong>{e.SubjectName}</strong>
                              <small>{e.Room || e.Professor}</small>
                            </div>
                          ))}
                        </td>
                      ),
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {t.online.length > 0 && (
            <div className="online-classes">
              <h3>온라인 수업</h3>
              {t.online.map((e, i) => (
                <span className="chip" key={i}>
                  {e.SubjectName}
                </span>
              ))}
            </div>
          )}
          {t.unscheduled.length > 0 && (
            <p className="alert">
              시간을 배치할 수 없는 수업:{" "}
              {t.unscheduled.map((e) => e.SubjectName).join(", ")}
            </p>
          )}
        </>
      ) : (
        <Empty title="등록된 시간표가 없어요" />
      )}
    </section>
  );
}
function Settings({
  connected,
  state,
  busy,
  action,
  refresh,
  sync,
  setSync,
}: {
  connected: boolean;
  state?: Data;
  busy: boolean;
  action: (f: () => Promise<void>) => Promise<void>;
  refresh: () => Promise<void>;
  sync?: Data;
  setSync: (d: Data) => void;
}) {
  const [id, setID] = useState(""),
    [password, setPassword] = useState(""),
    [decisions, setDecisions] = useState<Data>({}),
    [pair, setPair] = useState<Data>();
  return (
    <div className="settings-grid">
      <section className="card settings-card">
        <span className="eyebrow">SCHOOL ACCOUNT</span>
        <h2>KLAS 연결</h2>
        <p className="muted">
          학번과 비밀번호는 이 인스턴스의 보안 설정 화면에서만 입력합니다.
        </p>
        {embedded ? (
          <button
            className="primary"
            onClick={() =>
              action(() => openURL(state?.connection?.settings_url || ""))
            }
          >
            보안 설정 열기 <Icon name="arrow" />
          </button>
        ) : connected ? (
          <>
            <p className="success">KLAS 계정이 연결되어 있습니다.</p>
            <button
              disabled={busy}
              onClick={() =>
                action(async () => {
                  await ownerRequest("/api/klas/disconnect");
                  await refresh();
                })
              }
            >
              계정 연결 해제
            </button>
          </>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              action(async () => {
                try {
                  await ownerRequest("/api/klas/connect", {
                    student_id: id,
                    password,
                  });
                  await refresh();
                } finally {
                  setPassword("");
                }
              });
            }}
          >
            <label>
              학번
              <input
                required
                autoComplete="username"
                value={id}
                onChange={(e) => setID(e.target.value)}
              />
            </label>
            <label>
              KLAS 비밀번호
              <input
                required
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
            <button className="primary" disabled={busy || !id || !password}>
              {busy ? "연결 확인 중…" : "KLAS 연결"}
            </button>
          </form>
        )}
      </section>
      <section className="card settings-card">
        <span className="eyebrow">INSTANCE</span>
        <h2>나의 KLAP</h2>
        <dl>
          <dt>실행 방식</dt>
          <dd>
            {{ server: "개인 서버", mac: "Mac 네이티브", hybrid: "서버 + Mac" }[
              state?.connection?.mode as string
            ] || "확인 중"}
          </dd>
          <dt>MCP 연결</dt>
          <dd>{state?.connection?.settings_url?.replace(/\/$/, "")}/mcp</dd>
          <dt>학교 정보 처리</dt>
          <dd>공통 KLAP-core</dd>
        </dl>
        {!embedded && (
          <button
            onClick={() =>
              action(async () => {
                await ownerRequest("/logout");
                location.assign("/login");
              })
            }
          >
            소유자 로그아웃
          </button>
        )}
      </section>
      {state?.connection?.apple_sync && (
        <section className="card settings-card wide">
          <span className="eyebrow">APPLE SYNC</span>
          <h2>캘린더와 미리알림</h2>
          <p className="muted">
            과제·강의는 미리알림, 시간표·학사일정은 캘린더에 동기화합니다.
          </p>
          <button
            className="primary"
            disabled={busy || !connected}
            onClick={() =>
              action(async () => {
                const r = await call("klap_sync_apple", {
                  decisions: Object.fromEntries(
                    Object.entries(decisions).filter(
                      ([, choice]) => choice === "keep" || choice === "apply",
                    ),
                  ),
                });
                setSync(r.data);
                setDecisions({});
              })
            }
          >
            Apple에 동기화
          </button>
          {sync && (
            <>
              <div className="sync-summary">
                {["assignments", "lectures", "academic", "timetable"].map(
                  (k) => (
                    <span key={k}>
                      {
                        (
                          {
                            assignments: "과제",
                            lectures: "강의",
                            academic: "학사일정",
                            timetable: "시간표",
                          } as Data
                        )[k]
                      }
                      : {sync[k]?.Result?.created || 0}개 추가 ·{" "}
                      {sync[k]?.Result?.updated || 0}개 갱신
                    </span>
                  ),
                )}
              </div>
              {(sync.errors || []).map((e: string, i: number) => (
                <p className="alert" key={i}>
                  {e}
                </p>
              ))}
              {(sync.conflicts || []).map((c: Data) => (
                <div className="row" key={c.Key}>
                  <div className="row-main">
                    <strong>{c.Title}</strong>
                    <small>{c.Summary}</small>
                  </div>
                  <select
                    aria-label={`${c.Title} 충돌 해결`}
                    value={decisions[c.Key] || ""}
                    onChange={(e) =>
                      setDecisions((v) => ({ ...v, [c.Key]: e.target.value }))
                    }
                  >
                    <option value="">선택 필요</option>
                    <option value="keep">현재 내용 유지</option>
                    <option value="apply">학교 변경 반영</option>
                  </select>
                </div>
              ))}
            </>
          )}
        </section>
      )}
      {state?.connection?.mode === "hybrid" && !embedded && (
        <section className="card settings-card wide">
          <span className="eyebrow">MAC HELPER</span>
          <h2>내 Mac 연결</h2>
          <p className="muted">
            Mac helper가 서버에 연결되어 있을 때 Apple 동기화를 실행할 수
            있습니다.
          </p>
          <button
            disabled={busy}
            onClick={() =>
              action(async () =>
                setPair(await ownerRequest("/api/helper/pair")),
              )
            }
          >
            연결 코드 만들기
          </button>
          {pair && (
            <div className="pair">
              <strong>5분 안에 Mac 설치 프로그램에 입력하세요.</strong>
              <code>{pair.code}</code>
              <p>이 코드는 한 번만 사용할 수 있습니다.</p>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
createRoot(document.getElementById("root")!).render(<App />);
