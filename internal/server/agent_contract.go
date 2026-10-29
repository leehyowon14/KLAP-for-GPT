package server

import (
	"encoding/json"
	"errors"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/klap"
	"strings"
)

func isAgentTool(name string) bool {
	switch strings.TrimPrefix(name, "klap_") {
	case "lectures", "assignments", "notices", "get_items", "due":
		return true
	}
	return false
}
func agentDescription(kind string) string {
	switch kind {
	case "due":
		return "Select all items at next_deadline or in_period. Includes future lectures. Kinds: assignment, lecture, academic. Uses end/deadline, never lecture opening time. Continue with cursor alone."
	case "lectures":
		return "Select lectures by intent: next_opening returns all at earliest future start; available_now; due_in_period; unfinished requires include_expired; completed; overview. Completion is independent of expiry. No count limit. Continue with cursor alone."
	case "assignments":
		return "Select fresh assignments by pending, submitted, overdue, due_in_period or overview. Inspect partial/unknown status. Continue with cursor alone."
	default:
		return "Select notice summaries by recent (last seven days), in_period or overview. Get selected bodies via klap_get_items. Continue with cursor alone."
	}
}
func arrayStrings() map[string]any {
	return map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "minItems": 1}
}
func agentSchema(kind string) map[string]any {
	props := map[string]any{"cursor": stringField("Opaque continuation; send cursor alone.")}
	required := []string{"intent"}
	if kind == "get_items" {
		props["ids"] = arrayStrings()
		props["sections"] = map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"completion", "content", "learning", "submission", "attachments"}}, "minItems": 1}
		required = []string{"ids", "sections"}
	} else {
		props["term"] = stringField("Term YYYY,S or YYYY-S; omitted uses selected term.")
		props["course_ids"] = arrayStrings()
		intents := []string{"recent", "in_period", "overview"}
		if kind == "lectures" {
			intents = []string{"next_opening", "available_now", "due_in_period", "unfinished", "completed", "overview"}
			props["include_expired"] = map[string]any{"type": "boolean", "description": "Required only with unfinished; true includes expired lectures."}
		}
		if kind == "assignments" {
			intents = []string{"pending", "submitted", "overdue", "due_in_period", "overview"}
		}
		if kind == "due" {
			intents = []string{"next_deadline", "in_period"}
			props["kinds"] = arrayStrings()
		}
		props["intent"] = map[string]any{"type": "string", "enum": intents}
		props["period"] = schema(map[string]any{"name": map[string]any{"type": "string", "enum": []string{"today", "this_week", "next_week", "next_7_days", "custom"}}, "from": stringField("Custom inclusive ISO datetime or KST date."), "until": stringField("Custom exclusive ISO datetime or KST date.")}, "name")
	}
	result := schema(props)
	result["anyOf"] = []any{map[string]any{"required": required}, map[string]any{"required": []string{"cursor"}}}
	return result
}
func validateAgentArgs(kind string, args []byte, q klap.Query) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return err
	}
	if q.Cursor != "" {
		if len(fields) != 1 {
			return errors.New("cursor must be supplied alone")
		}
		return nil
	}
	allowed := map[string]bool{"term": true, "course_ids": true, "intent": true, "period": true}
	if kind == "due" {
		allowed["kinds"] = true
	}
	if kind == "lectures" {
		allowed["include_expired"] = true
	}
	if kind == "get_items" {
		allowed = map[string]bool{"ids": true, "sections": true}
	}
	for k, v := range fields {
		if !allowed[k] || string(v) == "null" {
			return errors.New("unsupported or null query argument: " + k)
		}
	}
	if kind == "get_items" {
		return core.ValidateDetailRequest(q.IDs, q.Sections)
	}
	if q.Intent == "" {
		return errors.New("intent required")
	}
	return nil
}
