package dashboard

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormBool accepts the legacy select values without treating the string "0" as true.
type FormBool bool

func (b *FormBool) UnmarshalJSON(raw []byte) error {
	switch string(raw) {
	case "true", "1", `"1"`, `"true"`:
		*b = true
	case "false", "0", `"0"`, `"false"`, "null":
		*b = false
	default:
		return errors.New("expected boolean or 0/1")
	}
	return nil
}

type CronInput struct {
	Year      string `json:"year"`
	Month     string `json:"month"`
	DayOfWeek string `json:"day_of_week"`
	Day       string `json:"day"`
	Hour      string `json:"hour"`
	Minute    string `json:"minute"`
	Second    string `json:"second"`
}

func (c *CronInput) normalize() error {
	fields := []*string{&c.Year, &c.Month, &c.DayOfWeek, &c.Day, &c.Hour, &c.Minute, &c.Second}
	bounds := [][2]int{{1970, 9999}, {1, 12}, {0, 6}, {1, 31}, {0, 23}, {0, 59}, {0, 59}}
	for i, p := range fields {
		if *p == "" {
			*p = "*"
		}
		limit := 4
		if i == 2 {
			limit = 30
		}
		aliases := ""
		if i == 1 {
			aliases = "jan feb mar apr may jun jul aug sep oct nov dec"
		}
		if i == 2 {
			aliases = "mon tue wed thu fri sat sun"
		}
		if len(*p) > limit || !validCron(*p, bounds[i][0], bounds[i][1], aliases) {
			return fail(422, "invalid_cron")
		}
	}
	return nil
}

// The pinned agent uses APScheduler's Monday=0 and rejects Quartz syntax.
// Bounds also respect the existing database columns (4 chars; weekdays 30).
func validCron(expr string, min, max int, names string) bool {
	if expr == "" || strings.TrimSpace(expr) != expr {
		return false
	}
	number := func(s string) (int, error) {
		for i, n := range strings.Fields(names) {
			if strings.EqualFold(n, s) {
				return i + min, nil
			}
		}
		return strconv.Atoi(s)
	}
	for _, part := range strings.Split(expr, ",") {
		bits := strings.Split(part, "/")
		if len(bits) > 2 || bits[0] == "" {
			return false
		}
		step := 1
		var err error
		if len(bits) == 2 {
			step, err = strconv.Atoi(bits[1])
			if err != nil || step < 1 {
				return false
			}
		}
		lo, hi := min, max
		if bits[0] != "*" {
			r := strings.Split(bits[0], "-")
			if len(r) > 2 {
				return false
			}
			lo, err = number(r[0])
			if err != nil {
				return false
			}
			hi = lo
			if len(r) == 2 {
				hi, err = number(r[1])
				if err != nil {
					return false
				}
			} else if len(bits) == 2 {
				hi = max
			}
		}
		if lo < min || hi > max || lo > hi || (len(bits) == 2 && step > hi-lo) {
			return false
		}
	}
	return true
}
func (c CronInput) args() []any {
	return []any{c.Year, c.Month, c.DayOfWeek, c.Day, c.Hour, c.Minute, c.Second}
}
func editableStatus(s string) bool {
	return s == "active" || s == "inactive" || s == "manual" || s == "error"
}

type ScheduleInput struct {
	CronInput
	ID        int64    `json:"schedule_id"`
	GroupID   int64    `json:"group_id"`
	Name      string   `json:"name"`
	DeviceID  int64    `json:"target_device"`
	ErrorStop FormBool `json:"is_error_stop"`
	Status    string   `json:"status"`
	Creator   string   `json:"creator"`
	Modifier  string   `json:"modifier"`
}

func (in *ScheduleInput) normalize(create bool) error {
	in.Name = strings.TrimSpace(in.Name)
	if !validName(in.Name) || !validID(in.GroupID, true) || !validID(in.DeviceID, false) || !editableStatus(in.Status) || (create && in.ID != 0) || (!create && !validID(in.ID, false)) {
		return fail(422, "invalid_request")
	}
	return in.CronInput.normalize()
}

type TaskInput struct {
	CronInput
	ID                    string   `json:"detail_id"`
	ScheduleID            int64    `json:"schedule_id"`
	Name                  string   `json:"task_name"`
	ErrorStop             FormBool `json:"is_error_stop"`
	Sequence              int64    `json:"sequence"`
	Retry                 int64    `json:"retry_count"`
	Status                string   `json:"status"`
	Type                  string   `json:"task_type"`
	Command               *string  `json:"command"`
	Archive               *string  `json:"archive_type"`
	Source                *string  `json:"source_path"`
	MissingError          FormBool `json:"error_on_missing_source"`
	Destination           *string  `json:"destination_path"`
	DateFormat            *string  `json:"date_format"`
	TargetDateFormat      *string  `json:"target_date_format"`
	DestinationDateFormat *string  `json:"destination_date_format"`
	KeepDays              *int64   `json:"house_keep_days"`
	Recursive             bool     `json:"house_keep_recursive"`
	DeleteEmpty           bool     `json:"house_keep_delete_empty_dirs"`
	Creator               string   `json:"creator"`
	Modifier              string   `json:"modifier"`
}

func (in *TaskInput) normalize(create bool) error {
	in.Name = strings.TrimSpace(in.Name)
	if !validName(in.Name) || !validID(in.ScheduleID, false) || !editableStatus(in.Status) || in.Sequence < 0 || in.Sequence > 2147483647 || in.Retry < 0 || in.Retry > 100 || (create && in.ID != "") {
		return fail(422, "invalid_request")
	}
	if !create {
		if _, err := parseUUID(in.ID); err != nil {
			return err
		}
	}
	if err := in.CronInput.normalize(); err != nil {
		return err
	}
	for _, p := range []**string{&in.Command, &in.Archive, &in.Source, &in.Destination, &in.DateFormat, &in.TargetDateFormat, &in.DestinationDateFormat} {
		if *p == nil {
			continue
		}
		value := **p
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || len(value) > 16384 {
			return fail(422, "invalid_task")
		}
		if strings.TrimSpace(value) == "" || strings.EqualFold(strings.TrimSpace(value), "null") {
			*p = nil
		}
	}
	for _, p := range []*string{in.DateFormat, in.TargetDateFormat, in.DestinationDateFormat} {
		if p != nil && utf8.RuneCountInString(*p) > 50 {
			return fail(422, "invalid_task")
		}
	}
	switch in.Type {
	case "command":
		in.Archive = nil
		in.Source = nil
		in.Destination = nil
		in.KeepDays = nil
		if in.Command == nil {
			return fail(422, "invalid_task")
		}
	case "copy":
		in.Command = nil
		in.Archive = nil
		in.KeepDays = nil
		if in.Source == nil || in.Destination == nil {
			return fail(422, "invalid_task")
		}
	case "archive":
		in.Command = nil
		in.KeepDays = nil
		if in.Archive == nil || *in.Archive != "zip" {
			return fail(422, "invalid_archive_type")
		}
		if in.Source == nil || in.Destination == nil {
			return fail(422, "invalid_task")
		}
	case "housekeep":
		in.Command = nil
		in.Archive = nil
		in.Source = nil
		if in.Destination == nil || in.KeepDays == nil || *in.KeepDays < 0 || *in.KeepDays > 2147483647 {
			return fail(422, "invalid_task")
		}
	default:
		return fail(422, "invalid_task")
	}
	if in.Type != "housekeep" {
		in.Recursive = false
		in.DeleteEmpty = false
	}
	return nil
}

// Preserve explicit JSON null for optional task fields, but refuse JSON objects,
// arrays and numeric strings for IDs. Embedded clients send typed payloads.
var _ json.Unmarshaler = (*FormBool)(nil)
