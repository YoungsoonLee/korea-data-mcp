package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSyncCorps(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	day1 := []Corp{
		{CorpCode: "00126380", Name: "삼성전자", StockCode: "005930", ModifyDate: "2026-11-01"},
		{CorpCode: "00174518", Name: "에스비아이저축은행", ModifyDate: "2026-11-01"},
		{CorpCode: "01956751", Name: "래블업", ModifyDate: "2026-11-01"},
		{CorpCode: "00000001", Name: "사라질회사", StockCode: "000001", ModifyDate: "2026-11-01"},
		{CorpCode: "00000002", Name: "코드변경", StockCode: "000002", ModifyDate: "2026-11-01"},
		{CorpCode: "00000003", Name: "폐지될회사", StockCode: "000003", ModifyDate: "2026-11-01"},
		{CorpCode: "00000004", Name: "가", ModifyDate: "2026-11-01"},
		{CorpCode: "00000005", Name: "나", ModifyDate: "2026-11-01"},
		{CorpCode: "00000006", Name: "다", ModifyDate: "2026-11-01"},
		{CorpCode: "00000007", Name: "라", ModifyDate: "2026-11-01"},
	}
	events, err := s.SyncCorps(ctx, day1, "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Errorf("first sync recorded events: %v", events)
	}

	day2 := slices.Clone(day1)
	day2[1].Name = "교보에스비아이저축은행"
	day2[2].StockCode = "0147B0" // listed, with a letter in the stock code
	day2[4].StockCode = "000022"
	day2[5].StockCode = ""
	day2 = slices.Delete(day2, 3, 4) // 사라질회사 removed
	day2 = append(day2, Corp{CorpCode: "02062059", Name: "농업회사법인오라관광농원", ModifyDate: "2026-11-06"})
	day2 = append(day2, day2[0]) // duplicate rows are ignored
	events, err = s.SyncCorps(ctx, day2, "2026-11-09")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Event{}
	for _, e := range events {
		got[e.CorpCode+" "+e.Type] = e
	}
	want := map[string][2]string{
		"00174518 " + EventRenamed:          {"에스비아이저축은행", "교보에스비아이저축은행"},
		"01956751 " + EventListed:           {"", "0147B0"},
		"00000002 " + EventStockCodeChanged: {"000002", "000022"},
		"00000003 " + EventDelisted:         {"000003", ""},
		"00000001 " + EventRemoved:          {"사라질회사", ""},
		"02062059 " + EventAdded:            {"", "농업회사법인오라관광농원"},
	}
	if len(got) != len(want) {
		t.Errorf("events = %v, want %d", events, len(want))
	}
	for k, w := range want {
		e, ok := got[k]
		if !ok || e.OldValue != w[0] || e.NewValue != w[1] || e.SeenOn != "2026-11-09" {
			t.Errorf("event %s = %+v, want %v", k, e, w)
		}
	}

	// The same list again changes nothing; a removed company is not
	// reported removed twice.
	events, err = s.SyncCorps(ctx, day2, "2026-11-10")
	if err != nil || len(events) != 0 {
		t.Errorf("unchanged sync: %v, %v", events, err)
	}
	stored, err := s.Events(ctx, "2026-11-01")
	if err != nil || len(stored) != len(want) {
		t.Errorf("stored events = %d, %v", len(stored), err)
	}
	if n, _ := s.CorpCount(ctx); n != 11 {
		t.Errorf("corps = %d, want 11 (removed ones kept)", n)
	}

	// A company that comes back is added again.
	day3 := append(slices.Clone(day2), day1[3])
	events, err = s.SyncCorps(ctx, day3, "2026-11-11")
	if err != nil || len(events) != 1 || events[0].Type != EventAdded || events[0].CorpCode != "00000001" {
		t.Errorf("returning company: %v, %v", events, err)
	}

	// Listed companies in today's list only, never synced first.
	if _, err := s.AddReports(ctx, "00126380", "major_holders", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	codes, err := s.ListedCorps(ctx, "major_holders", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(codes, []string{"00000001", "00000002", "01956751", "00126380"}) {
		t.Errorf("listed corps = %v", codes)
	}
}

func TestSyncCorpsShrunk(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	var corps []Corp
	for i := range 20 {
		corps = append(corps, Corp{CorpCode: string(rune('A' + i)), Name: "x"})
	}
	if _, err := s.SyncCorps(ctx, corps, "2026-11-06"); err != nil {
		t.Fatal(err)
	}
	_, err := s.SyncCorps(ctx, corps[:15], "2026-11-09")
	if !errors.Is(err, ErrShrunk) {
		t.Fatalf("err = %v, want ErrShrunk", err)
	}
	if ev, _ := s.Events(ctx, ""); len(ev) != 0 {
		t.Errorf("events recorded from a truncated list: %v", ev)
	}
}

func TestAddReports(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	r := func(no string) Report {
		return Report{Kind: "major_holders", ReceiptNo: no, CorpCode: "00663669", Reporter: "폴라리스세원", FiledOn: "2026-11-06",
			Body: json.RawMessage(`{"receipt_no":"` + no + `"}`)}
	}
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	added, err := s.AddReports(ctx, "00663669", "major_holders", []Report{r("20260506000715"), r("20261106000465")}, now)
	if err != nil || added != 2 {
		t.Fatalf("added = %d, %v", added, err)
	}
	// Later, DART returns the correction but no longer the original: the
	// original stays.
	added, err = s.AddReports(ctx, "00663669", "major_holders", []Report{r("20261106000465"), r("20261109000001")}, now)
	if err != nil || added != 1 {
		t.Fatalf("added = %d, %v", added, err)
	}
	if n, _ := s.ReportCount(ctx, "00663669", "major_holders"); n != 3 {
		t.Errorf("stored = %d, want 3", n)
	}
	if n, _ := s.ReportCount(ctx, "", "insiders"); n != 0 {
		t.Errorf("insiders = %d", n)
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Now()
	id, err := s.StartRun(ctx, "daily", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, id, "partial", map[string]int{"corps": 3}, errors.New("quota"), now); err != nil {
		t.Fatal(err)
	}
	var status, stats, msg string
	if err := s.db.QueryRow(`SELECT status, stats, error FROM run WHERE id = ?`, id).Scan(&status, &stats, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "partial" || stats != `{"corps":3}` || msg != "quota" {
		t.Errorf("run = %s %s %s", status, stats, msg)
	}
}
