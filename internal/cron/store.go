package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/fsstore"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"gopkg.in/yaml.v3"
)

type SemanticResolver interface {
	MatchTaskIndex(ctx context.Context, query string, tasks []Task) (int, error)
}

type Store struct {
	Path      string
	Now       func() time.Time
	Semantics SemanticResolver
}

func NewStore(path string) *Store {
	return &Store{
		Path: pathutil.ExpandHomePath(strings.TrimSpace(path)),
		Now:  time.Now,
	}
}

func (s *Store) Read() (File, bool, error) {
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return File{}, false, fmt.Errorf("cron path is not configured")
	}
	text, exists, err := fsstore.ReadText(path)
	if err != nil {
		return File{}, false, err
	}
	if !exists || strings.TrimSpace(text) == "" {
		return File{Version: Version}, exists, nil
	}
	var file File
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		return File{}, exists, fmt.Errorf("parse cron.yaml: %w", err)
	}
	if file.Version == 0 {
		file.Version = Version
	}
	if file.Tasks == nil {
		file.Tasks = []Task{}
	}
	for i := range file.Tasks {
		file.Tasks[i].Title = normalizeTaskTitle(file.Tasks[i].Title)
	}
	if file.Version != Version {
		return File{}, exists, fmt.Errorf("unsupported cron file version: %d", file.Version)
	}
	return file, exists, nil
}

func (s *Store) Write(file File) error {
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return fmt.Errorf("cron path is not configured")
	}
	if file.Version == 0 {
		file.Version = Version
	}
	if file.Tasks == nil {
		file.Tasks = []Task{}
	}
	for i := range file.Tasks {
		file.Tasks[i].Title = normalizeTaskTitle(file.Tasks[i].Title)
	}
	if err := ValidateFile(file); err != nil {
		return err
	}
	raw, err := yaml.Marshal(file)
	if err != nil {
		return err
	}
	return fsstore.WriteTextAtomic(path, string(raw), fsstore.FileOptions{DirPerm: 0o700, FilePerm: 0o600})
}

func (s *Store) AddOnceWithChatID(title, content, at, tz, id, chatID string) (AddResult, error) {
	task := Task{
		ID:      normalizeTaskID(id),
		Title:   normalizeTaskTitle(title),
		At:      strings.TrimSpace(at),
		TZ:      strings.TrimSpace(tz),
		Content: strings.TrimSpace(content),
		ChatID:  strings.TrimSpace(chatID),
	}
	return s.addTask("add_once", task)
}

func (s *Store) AddRecurringWithChatID(title, content, expr, tz, id, chatID string) (AddResult, error) {
	task := Task{
		ID:      normalizeTaskID(id),
		Title:   normalizeTaskTitle(title),
		Cron:    strings.TrimSpace(expr),
		TZ:      strings.TrimSpace(tz),
		Content: strings.TrimSpace(content),
		ChatID:  strings.TrimSpace(chatID),
	}
	return s.addTask("add_recurring", task)
}

func (s *Store) addTask(action string, task Task) (AddResult, error) {
	file, _, err := s.Read()
	if err != nil {
		return AddResult{}, err
	}
	if err := ValidateTask(task); err != nil {
		return AddResult{}, err
	}
	for _, existing := range file.Tasks {
		if strings.TrimSpace(existing.ID) == strings.TrimSpace(task.ID) {
			return AddResult{}, fmt.Errorf("duplicate task id: %s", strings.TrimSpace(task.ID))
		}
	}
	file.Tasks = append(file.Tasks, task)
	if err := s.Write(file); err != nil {
		return AddResult{}, err
	}
	return AddResult{
		OK:        true,
		Action:    action,
		TaskCount: len(file.Tasks),
		Task:      &task,
	}, nil
}

func (s *Store) Delete(ctx context.Context, id, content string) (DeleteResult, error) {
	id = strings.TrimSpace(id)
	if id != "" {
		return s.DeleteByID(id)
	}
	query := strings.TrimSpace(content)
	if query == "" {
		return DeleteResult{}, fmt.Errorf("id or content is required")
	}
	file, _, err := s.Read()
	if err != nil {
		return DeleteResult{}, err
	}
	if len(file.Tasks) == 0 {
		return DeleteResult{}, fmt.Errorf("no matching cron task in cron.yaml")
	}
	if s.Semantics == nil {
		return DeleteResult{}, fmt.Errorf("cron semantic resolver is required")
	}
	idx, err := s.Semantics.MatchTaskIndex(ctx, query, file.Tasks)
	if err != nil {
		return DeleteResult{}, err
	}
	if idx < 0 || idx >= len(file.Tasks) {
		return DeleteResult{}, fmt.Errorf("no matching cron task in cron.yaml")
	}
	deleted := file.Tasks[idx]
	file.Tasks = append(append([]Task{}, file.Tasks[:idx]...), file.Tasks[idx+1:]...)
	if err := s.Write(file); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{OK: true, Action: "delete", TaskCount: len(file.Tasks), Deleted: &deleted}, nil
}

func (s *Store) DeleteByID(id string) (DeleteResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return DeleteResult{}, fmt.Errorf("id is required")
	}
	file, _, err := s.Read()
	if err != nil {
		return DeleteResult{}, err
	}
	idx := -1
	for i, task := range file.Tasks {
		if strings.TrimSpace(task.ID) == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return DeleteResult{}, fmt.Errorf("no matching cron task in cron.yaml")
	}
	deleted := file.Tasks[idx]
	file.Tasks = append(append([]Task{}, file.Tasks[:idx]...), file.Tasks[idx+1:]...)
	if err := s.Write(file); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{OK: true, Action: "delete", TaskCount: len(file.Tasks), Deleted: &deleted}, nil
}

func (s *Store) FindByID(id string) (Task, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Task{}, false, fmt.Errorf("id is required")
	}
	file, _, err := s.Read()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range file.Tasks {
		if strings.TrimSpace(task.ID) == id {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func (s *Store) Due(now time.Time) ([]DueTask, error) {
	file, _, err := s.Read()
	if err != nil {
		return nil, err
	}
	return DueTasks(file, now)
}

func (s *Store) DueLenient(now time.Time) ([]DueTask, []error, error) {
	file, _, err := s.Read()
	if err != nil {
		return nil, nil, err
	}
	now = now.UTC()
	out := make([]DueTask, 0, len(file.Tasks))
	var taskErrs []error
	seen := map[string]bool{}
	for _, task := range file.Tasks {
		id := strings.TrimSpace(task.ID)
		if seen[id] {
			taskErrs = append(taskErrs, fmt.Errorf("duplicate task id: %s", id))
			continue
		}
		seen[id] = true
		if !TaskEnabled(task) {
			continue
		}
		if err := ValidateTask(task); err != nil {
			taskErrs = append(taskErrs, err)
			continue
		}
		due, scheduledAt, err := IsDue(task, now)
		if err != nil {
			taskErrs = append(taskErrs, err)
			continue
		}
		if due {
			out = append(out, DueTask{Task: task, ScheduledAtUTC: scheduledAt})
		}
	}
	return out, taskErrs, nil
}

func normalizeTaskID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	return "cron-" + uuid.NewString()
}
