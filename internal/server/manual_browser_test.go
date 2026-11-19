package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/horrible-gh/TimeWeaver/internal/access"
	"github.com/horrible-gh/TimeWeaver/internal/webui"
)

func TestBrowserManualFlow(t *testing.T) {
	if os.Getenv("TIMEWEAVER_BROWSER_ACCESS") != "1" {
		t.Skip("requires browser and explicitly allowed disposable DB")
	}
	f := deviceFixtureFor(t)
	ctx := context.Background()
	admin := f.sessions["timeweaver"]
	schedule := createSchedule(t, f, 7, "manual-seven")
	private := createSchedule(t, f, 8, "manual-eight-private")
	makeTask := func(sid int64, name string) string {
		t.Helper()
		in := taskInput(sid)
		in.Name = name
		id, err := f.dashboard.SaveTask(ctx, admin.token, admin.csrf, in, true)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	task := makeTask(schedule, "manual-first")
	makeTask(schedule, "manual-second")
	privateTask := makeTask(private, "private-task-eight")
	processing, err := fixtureExec(t, f.db, `INSERT INTO manual_execution(detail_id,is_immediate,status,schedule_datetime,claim_token,claim_expires_at) VALUES(UNHEX(REPLACE(?,'-','')),1,'processing',UTC_TIMESTAMP(),REPEAT('a',64),UTC_TIMESTAMP()+INTERVAL 1 HOUR)`, task)
	if err != nil {
		t.Fatal(err)
	}
	processingID, err := processing.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixtureExec(t, f.db, `INSERT INTO manual_execution(detail_id,is_immediate,status,schedule_datetime) VALUES(UNHEX(REPLACE(?,'-','')),1,'wait',UTC_TIMESTAMP())`, privateTask); err != nil {
		t.Fatal(err)
	}
	historyTime := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	addHistory := func(sid int64, tid, group, message string) int64 {
		t.Helper()
		result, e := fixtureExec(t, f.db, `INSERT INTO execution_log(execution_grp_id,schedule_id,detail_id,start_time,end_time,result_code,result_message) VALUES(UNHEX(?),?,UNHEX(REPLACE(?,'-','')),?,?,0,?)`, group, sid, tid, historyTime, historyTime.Add(time.Second), message)
		if e != nil {
			t.Fatal(e)
		}
		id, e := result.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	historyID := addHistory(schedule, task, strings.Repeat("ab", 16), "visible-ok")
	addHistory(private, privateTask, strings.Repeat("cd", 16), "private-no")
	ui, err := webui.Bundled(f.settings.Context)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + ln.Addr().String()
	app, err := NewBrowserSurface(ctx, f.db, f.probe, f.settings, ui, slog.New(slog.NewTextHandler(io.Discard, nil)), access.HTTPOptions{Origin: origin})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- app.Serve(run, ln) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("manual browser server did not stop")
		}
	}()
	limited, stop := context.WithTimeout(ctx, 110*time.Second)
	defer stop()
	cmd := exec.CommandContext(limited, "python", filepath.Join("..", "..", "scripts", "test-browser-manual.py"))
	input, _ := json.Marshal(map[string]any{"origin": origin, "context": f.settings.Context, "password": deviceTestPassword, "schedule_id": schedule, "task_id": task, "private_task_id": privateTask, "processing_id": processingID, "history_id": historyID, "history_time": historyTime.Format(time.RFC3339)})
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("manual browser failed: %v\n%s", err, out)
	}
	t.Log(string(out))
}
