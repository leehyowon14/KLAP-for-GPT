package klap

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/events"
	"github.com/leehyowon14/KLAP-for-GPT/internal/jobs"
)

type Service struct {
	Core       *core.Service
	Apple      bool
	AppleReady func() bool
	syncMu     sync.Mutex
}
type Query struct {
	Intent         string            `json:"intent,omitempty"`
	CourseIDs      []string          `json:"course_ids,omitempty"`
	IncludeExpired *bool             `json:"include_expired,omitempty"`
	Period         *core.QueryPeriod `json:"period,omitempty"`
	IDs            []string          `json:"ids,omitempty"`
	Sections       []string          `json:"sections,omitempty"`
	Cursor         string            `json:"cursor,omitempty"`
	Term           string            `json:"term,omitempty"`
	CourseID       string            `json:"course_id,omitempty"`
	ID             string            `json:"id,omitempty"`
	Query          string            `json:"query,omitempty"`
	Days           int               `json:"days,omitempty"`
	Year           string            `json:"year,omitempty"`
}

func (s *Service) user(ctx context.Context) (core.UserOption, error) {
	rows, e := s.Core.Users(ctx)
	if e != nil {
		return core.UserOption{}, e
	}
	for _, r := range rows {
		if r.Current {
			return core.UserOption{StudentID: r.User.StudentID}, nil
		}
	}
	if len(rows) == 1 {
		return core.UserOption{StudentID: rows[0].User.StudentID}, nil
	}
	return core.UserOption{}, errors.New("KLAS 계정을 먼저 연결해 주세요.")
}
func (s *Service) Account(ctx context.Context) (string, error) {
	user, e := s.user(ctx)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(user.StudentID))), nil
}
func (s *Service) Content(ctx context.Context, name string, args events.Arguments) ([]core.ContentItem, error) {
	user, e := s.user(ctx)
	if e != nil {
		return nil, e
	}
	return core.Content(ctx, s.Core, name, args.Term, args.CourseID, user)
}
func (s *Service) Attend(ctx context.Context, id, expected string, progress func(jobs.Progress)) (bool, error) {
	user, e := s.user(ctx)
	if e != nil {
		return false, e
	}
	account := fmt.Sprintf("%x", sha256.Sum256([]byte(user.StudentID)))
	if account != expected {
		return false, errors.New("수강을 요청한 계정이 변경되었습니다.")
	}
	result, e := s.Core.AttendLecture(ctx, id, core.LectureAttendOptions{User: user, RequireEligible: true, OnProgress: func(row core.LectureRow, p core.LectureProgress) {
		progress(jobs.Progress{ID: row.ID, Title: row.Lecture.Title, Percent: p.Progress, Achieved: p.TotalTime, Required: p.PTime})
	}})
	return result.Progress.Completed, e
}
func (s *Service) Read(ctx context.Context, kind string, q Query) (any, error) {
	user, e := s.user(ctx)
	if e != nil {
		return nil, e
	}
	service, e := s.Core.ForTerm(q.Term)
	if e != nil {
		return nil, e
	}
	if kind == "get_items" {
		return service.GetItems(ctx, user, q.IDs, q.Sections)
	}
	if q.Intent != "" {
		intent := core.AgentQuery{Intent: q.Intent, CourseIDs: q.CourseIDs, IncludeExpired: q.IncludeExpired, Period: q.Period}
		switch kind {
		case "lectures":
			return service.QueryLectures(ctx, user, intent)
		case "assignments":
			return service.QueryAssignments(ctx, user, intent)
		case "notices":
			return service.QueryNotices(ctx, user, intent)
		}
	}
	switch kind {
	case "terms":
		return service.TermList(ctx, core.TermListOptions{User: user})
	case "courses":
		return service.CourseList(ctx, core.CourseListOptions{User: user, Refresh: true})
	case "assignments":
		return service.AssignmentList(ctx, core.AssignmentListOptions{User: user, CourseFilter: q.CourseID, Refresh: true})
	case "assignment_detail":
		return service.AssignmentDetail(ctx, q.ID, user)
	case "notices":
		return service.NoticeList(ctx, core.NoticeListOptions{User: user, CourseFilter: q.CourseID, Refresh: true})
	case "notice_detail":
		return service.NoticeDetail(ctx, q.ID, user)
	case "lectures":
		rows, e := service.LectureList(ctx, core.LectureListOptions{User: user, CourseFilter: q.CourseID, Refresh: true})
		if e != nil {
			return nil, e
		}
		result := []map[string]any{}
		for _, row := range rows {
			reason := ""
			if e := core.ValidateLectureAttendance(row, time.Now()); e != nil {
				reason = e.Error()
			}
			result = append(result, map[string]any{"row": row, "reason": reason})
		}
		return result, nil
	case "timetable":
		return service.Timetable(ctx, core.TimetableOptions{User: user, Refresh: true})
	case "academic":
		return service.AcademicList(ctx, core.AcademicListOptions{Year: q.Year, Refresh: true})
	case "due":
		if q.Days < 0 || q.Days > 90 {
			return nil, errors.New("days must be between 0 and 90")
		}
		return service.Due(ctx, core.DueOptions{User: user, Days: q.Days, Refresh: true})
	case "search":
		if len(q.Query) == 0 || len(q.Query) > 200 {
			return nil, errors.New("query must contain 1–200 characters")
		}
		return service.Search(ctx, core.SearchOptions{User: user, Query: q.Query, Refresh: true})
	case "attendance":
		value, e := service.AttendanceList(ctx, core.AttendanceListOptions{User: user, Refresh: true})
		if e != nil {
			return nil, e
		}
		rows := []map[string]any{}
		for _, r := range value.Rows {
			message := ""
			if r.Err != nil {
				message = r.Err.Error()
			}
			rows = append(rows, map[string]any{"Index": r.Index, "Course": r.Course, "Sessions": r.Sessions, "Error": message})
		}
		return map[string]any{"Term": value.Term, "Rows": rows}, nil
	default:
		return nil, errors.New("unsupported query")
	}
}
func (s *Service) Connect(ctx context.Context, id, password string) error {
	if current, e := s.user(ctx); e == nil && current.StudentID != id {
		return errors.New("현재 계정의 연결을 해제한 뒤 다른 계정을 등록해 주세요.")
	}
	return s.Core.Authenticate(ctx, id, password)
}
func (s *Service) Disconnect(ctx context.Context) error {
	user, e := s.user(ctx)
	if e != nil {
		return e
	}
	return s.Core.RemoveUser(ctx, user.StudentID)
}
func (s *Service) Sync(ctx context.Context, decisions map[string]core.SyncDecision) (any, error) {
	if !s.Apple || (s.AppleReady != nil && !s.AppleReady()) {
		return nil, errors.New("Apple 동기화를 지원하는 Mac에 연결해 주세요.")
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	for _, d := range decisions {
		if d != "keep" && d != "apply" {
			return nil, errors.New("sync decisions must be keep or apply")
		}
	}
	user, e := s.user(ctx)
	if e != nil {
		return nil, e
	}
	v := s.Core.SyncDashboard(ctx, core.DashboardSyncOptions{User: user, Decisions: decisions})
	problems := []string{}
	for _, e := range []error{v.AssignmentError, v.LectureError, v.AcademicError, v.TimetableError} {
		if e != nil {
			problems = append(problems, e.Error())
		}
	}
	conflicts := append(v.Assignments.Conflicts, v.Lectures.Conflicts...)
	conflicts = append(conflicts, v.Academic.Conflicts...)
	conflicts = append(conflicts, v.Timetable.Conflicts...)
	return map[string]any{"errors": problems, "conflicts": conflicts, "assignments": v.Assignments, "lectures": v.Lectures, "academic": v.Academic, "timetable": v.Timetable}, nil
}
