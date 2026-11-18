package app

import (
	"licentivo-launch/internal/ai"
	"licentivo-launch/internal/storage"
	"testing"
	"time"
)

func TestAIScheduleCalendar(t *testing.T) {
	after := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		spec    AIScheduleSpec
		minutes int
		want    string
	}{
		{AIScheduleSpec{Frequency: "daily", TimeZone: "Asia/Shanghai", At: "09:00"}, 0, "2026-11-18T01:00:00Z"},
		{AIScheduleSpec{Frequency: "weekly", TimeZone: "Asia/Shanghai", At: "09:00", Weekdays: []int{1}}, 0, "2026-11-23T01:00:00Z"},
		{AIScheduleSpec{Frequency: "monthly", TimeZone: "UTC", At: "09:00", MonthDay: 31}, 0, "2026-12-31T09:00:00Z"},
		{AIScheduleSpec{Frequency: "once", OnceAt: "2026-12-01T09:00:00+08:00"}, 0, "2026-12-01T01:00:00Z"},
		{AIScheduleSpec{}, 15, "2026-11-17T02:15:00Z"},
	} {
		got, err := aiNextSchedule(test.spec, test.minutes, after)
		if err != nil || got.Format(time.RFC3339) != test.want {
			t.Fatal(test.spec, got, err)
		}
	}
	for _, spec := range []AIScheduleSpec{{Frequency: "weekly", TimeZone: "UTC", At: "09:00"}, {Frequency: "daily", TimeZone: "Invalid/Zone", At: "09:00"}, {Frequency: "monthly", TimeZone: "UTC", At: "09:00", MonthDay: 32}, {Frequency: "once", OnceAt: "2026-01-01T00:00:00Z"}} {
		if _, err := aiNextSchedule(spec, 0, after); err == nil {
			t.Fatal("invalid calendar accepted", spec)
		}
	}
}
func TestAIPlanEnforcesToolAndActionScope(t *testing.T) {
	s := &Service{}
	plan := AIPlan{Title: "Inspect", Goal: "Review data", Steps: []AIPlanStep{{Title: "Read overview", Tool: "query_system"}}}
	if err := s.aiValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	task := storage.AITask{Kind: "plan", Input: marshal(aiTurn{Plan: &plan})}
	call := ai.Call{}
	call.Function.Name = "generate_word"
	if s.aiToolAllowed(task, call) {
		t.Fatal("unplanned tool allowed")
	}
	call.Function.Name = "query_system"
	if !s.aiToolAllowed(task, call) {
		t.Fatal("planned tool rejected")
	}
	plan.Steps = []AIPlanStep{{Title: "Change product", Tool: "propose_change", Action: "save_product"}}
	task.Input = marshal(aiTurn{Plan: &plan})
	call.Function.Name = "propose_change"
	call.Function.Arguments = `{"action":"save_brand","arguments":{},"summary":"bad"}`
	if s.aiToolAllowed(task, call) {
		t.Fatal("unplanned action allowed")
	}
	task.Kind = "planning"
	call.Function.Name = "generate_excel"
	if s.aiToolAllowed(task, call) {
		t.Fatal("planning can execute")
	}
	plan.Steps[0].Tool = "execute_sql"
	if s.aiValidatePlan(plan) == nil {
		t.Fatal("arbitrary tool in plan")
	}
}
