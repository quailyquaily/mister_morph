package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/internal/todo"
)

type fieldSet struct {
	Any    bool
	Values map[int]bool
}

type Expression struct {
	minute fieldSet
	hour   fieldSet
	dom    fieldSet
	month  fieldSet
	dow    fieldSet
}

func ParseExpression(raw string) (Expression, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) != 5 {
		return Expression{}, fmt.Errorf("cron expression must have 5 fields")
	}
	minute, err := parseField(parts[0], 0, 59, false)
	if err != nil {
		return Expression{}, fmt.Errorf("invalid minute field: %w", err)
	}
	hour, err := parseField(parts[1], 0, 23, false)
	if err != nil {
		return Expression{}, fmt.Errorf("invalid hour field: %w", err)
	}
	dom, err := parseField(parts[2], 1, 31, false)
	if err != nil {
		return Expression{}, fmt.Errorf("invalid day-of-month field: %w", err)
	}
	month, err := parseField(parts[3], 1, 12, false)
	if err != nil {
		return Expression{}, fmt.Errorf("invalid month field: %w", err)
	}
	dow, err := parseField(parts[4], 0, 7, true)
	if err != nil {
		return Expression{}, fmt.Errorf("invalid day-of-week field: %w", err)
	}
	return Expression{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func (e Expression) Matches(t time.Time) bool {
	if !e.minute.matches(t.Minute()) || !e.hour.matches(t.Hour()) || !e.month.matches(int(t.Month())) {
		return false
	}
	domMatch := e.dom.matches(t.Day())
	dowMatch := e.dow.matches(int(t.Weekday()))
	switch {
	case e.dom.Any && e.dow.Any:
		return true
	case e.dom.Any:
		return dowMatch
	case e.dow.Any:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

func parseField(raw string, minValue int, maxValue int, sundayAlias bool) (fieldSet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fieldSet{}, fmt.Errorf("empty field")
	}
	out := fieldSet{Values: map[int]bool{}}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			return fieldSet{}, fmt.Errorf("empty list item")
		}
		base := token
		step := 1
		if strings.Contains(token, "/") {
			parts := strings.Split(token, "/")
			if len(parts) != 2 {
				return fieldSet{}, fmt.Errorf("invalid step syntax %q", token)
			}
			base = strings.TrimSpace(parts[0])
			parsedStep, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || parsedStep <= 0 {
				return fieldSet{}, fmt.Errorf("invalid step %q", token)
			}
			step = parsedStep
		}
		start, end, any, err := parseFieldBase(base, minValue, maxValue)
		if err != nil {
			return fieldSet{}, err
		}
		if any && step == 1 {
			out.Any = true
		}
		for v := start; v <= end; v += step {
			normalized := v
			if sundayAlias && normalized == 7 {
				normalized = 0
			}
			out.Values[normalized] = true
		}
	}
	if out.Any {
		return out, nil
	}
	return out, nil
}

func parseFieldBase(raw string, minValue int, maxValue int) (int, int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "*" {
		return minValue, maxValue, true, nil
	}
	if strings.Contains(raw, "-") {
		parts := strings.Split(raw, "-")
		if len(parts) != 2 {
			return 0, 0, false, fmt.Errorf("invalid range %q", raw)
		}
		start, err := parseFieldNumber(parts[0], minValue, maxValue)
		if err != nil {
			return 0, 0, false, err
		}
		end, err := parseFieldNumber(parts[1], minValue, maxValue)
		if err != nil {
			return 0, 0, false, err
		}
		if start > end {
			return 0, 0, false, fmt.Errorf("range start greater than end %q", raw)
		}
		return start, end, false, nil
	}
	value, err := parseFieldNumber(raw, minValue, maxValue)
	if err != nil {
		return 0, 0, false, err
	}
	return value, value, false, nil
}

func parseFieldNumber(raw string, minValue int, maxValue int) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty number")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("only numeric cron fields are supported")
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("value %d outside %d-%d", value, minValue, maxValue)
	}
	return value, nil
}

func (s fieldSet) matches(value int) bool {
	if s.Any {
		return true
	}
	return s.Values[value]
}

func DueTasks(file File, now time.Time) ([]DueTask, error) {
	now = now.UTC()
	out := make([]DueTask, 0, len(file.Tasks))
	seen := map[string]bool{}
	for _, task := range file.Tasks {
		if seen[strings.TrimSpace(task.ID)] {
			return nil, fmt.Errorf("duplicate task id: %s", strings.TrimSpace(task.ID))
		}
		seen[strings.TrimSpace(task.ID)] = true
		if !TaskEnabled(task) {
			continue
		}
		if err := ValidateTask(task); err != nil {
			return nil, err
		}
		due, scheduledAt, err := IsDue(task, now)
		if err != nil {
			return nil, err
		}
		if due {
			out = append(out, DueTask{Task: task, ScheduledAtUTC: scheduledAt})
		}
	}
	return out, nil
}

func IsDue(task Task, now time.Time) (bool, time.Time, error) {
	loc, err := taskLocation(task.TZ)
	if err != nil {
		return false, time.Time{}, err
	}
	atRaw, cronRaw, err := taskSchedule(task)
	if err != nil {
		return false, time.Time{}, err
	}
	if atRaw != "" {
		at, err := time.ParseInLocation(TimestampLayout, atRaw, loc)
		if err != nil {
			return false, time.Time{}, fmt.Errorf("invalid at: %s", atRaw)
		}
		return !at.After(now.In(loc)), at.UTC(), nil
	}
	expr, err := ParseExpression(cronRaw)
	if err != nil {
		return false, time.Time{}, err
	}
	localNow := now.In(loc)
	if !expr.Matches(localNow) {
		return false, time.Time{}, nil
	}
	return true, time.Date(localNow.Year(), localNow.Month(), localNow.Day(), localNow.Hour(), localNow.Minute(), 0, 0, loc).UTC(), nil
}

func ValidateFile(file File) error {
	if file.Version != 0 && file.Version != Version {
		return fmt.Errorf("unsupported cron file version: %d", file.Version)
	}
	seen := map[string]bool{}
	for _, task := range file.Tasks {
		if err := ValidateTask(task); err != nil {
			return err
		}
		id := strings.TrimSpace(task.ID)
		if seen[id] {
			return fmt.Errorf("duplicate task id: %s", id)
		}
		seen[id] = true
	}
	return nil
}

func ValidateTask(task Task) error {
	id := strings.TrimSpace(task.ID)
	if id == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.ContainsAny(id, " \t\r\n") {
		return fmt.Errorf("task id must not contain whitespace: %s", id)
	}
	content := strings.TrimSpace(task.Content)
	if content == "" {
		return fmt.Errorf("task content is required")
	}
	if _, err := todo.ExtractReferenceIDs(content); err != nil {
		return err
	}
	loc, err := taskLocation(task.TZ)
	if err != nil {
		return err
	}
	atRaw, cronRaw, err := taskSchedule(task)
	if err != nil {
		return fmt.Errorf("task %s must use exactly one of at or cron", id)
	}
	if atRaw != "" {
		if _, err := time.ParseInLocation(TimestampLayout, atRaw, loc); err != nil {
			return fmt.Errorf("invalid at for task %s: %s", id, atRaw)
		}
	} else if _, err := ParseExpression(cronRaw); err != nil {
		return fmt.Errorf("invalid cron for task %s: %w", id, err)
	}
	if err := validateBashEnvRefs(task.BashEnv); err != nil {
		return fmt.Errorf("task %s: %w", id, err)
	}
	return nil
}

func taskSchedule(task Task) (string, string, error) {
	atRaw := strings.TrimSpace(task.At)
	cronRaw := strings.TrimSpace(task.Cron)
	if (atRaw == "") == (cronRaw == "") {
		return "", "", fmt.Errorf("task must use exactly one of at or cron")
	}
	return atRaw, cronRaw, nil
}

func taskLocation(raw string) (*time.Location, error) {
	tz := strings.TrimSpace(raw)
	if tz == "" {
		return time.Local, nil
	}
	if loc, ok, err := fixedUTCOffsetLocation(tz); ok || err != nil {
		return loc, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %s", tz)
	}
	return loc, nil
}

func fixedUTCOffsetLocation(raw string) (*time.Location, bool, error) {
	tz := strings.TrimSpace(raw)
	upper := strings.ToUpper(tz)
	if upper == "UTC" {
		return time.UTC, true, nil
	}
	if !strings.HasPrefix(upper, "UTC+") && !strings.HasPrefix(upper, "UTC-") {
		return nil, false, nil
	}
	sign := 1
	if upper[3] == '-' {
		sign = -1
	}
	offsetRaw := strings.TrimSpace(upper[4:])
	hour, minute, err := parseUTCOffset(offsetRaw)
	if err != nil {
		return nil, true, fmt.Errorf("invalid timezone: %s", tz)
	}
	totalSeconds := hour*3600 + minute*60
	if totalSeconds > 14*3600 {
		return nil, true, fmt.Errorf("invalid timezone: %s", tz)
	}
	name := formatUTCOffsetName(sign, hour, minute)
	return time.FixedZone(name, sign*totalSeconds), true, nil
}

func parseUTCOffset(raw string) (int, int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, fmt.Errorf("empty UTC offset")
	}
	var hourRaw, minuteRaw string
	if strings.Contains(raw, ":") {
		parts := strings.Split(raw, ":")
		if len(parts) != 2 || len(parts[1]) != 2 {
			return 0, 0, fmt.Errorf("invalid UTC offset")
		}
		hourRaw = parts[0]
		minuteRaw = parts[1]
	} else if len(raw) > 2 {
		hourRaw = raw[:len(raw)-2]
		minuteRaw = raw[len(raw)-2:]
	} else {
		hourRaw = raw
		minuteRaw = "0"
	}
	hour, err := parseOffsetNumber(hourRaw)
	if err != nil {
		return 0, 0, err
	}
	minute, err := parseOffsetNumber(minuteRaw)
	if err != nil {
		return 0, 0, err
	}
	if minute > 59 {
		return 0, 0, fmt.Errorf("invalid UTC offset minute")
	}
	return hour, minute, nil
}

func parseOffsetNumber(raw string) (int, error) {
	if raw == "" {
		return 0, fmt.Errorf("empty UTC offset number")
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("UTC offset must be numeric")
		}
	}
	return strconv.Atoi(raw)
}

func formatUTCOffsetName(sign int, hour int, minute int) string {
	prefix := "+"
	if sign < 0 {
		prefix = "-"
	}
	if minute == 0 {
		return fmt.Sprintf("UTC%s%d", prefix, hour)
	}
	return fmt.Sprintf("UTC%s%d:%02d", prefix, hour, minute)
}
