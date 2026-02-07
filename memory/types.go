package memory

import "time"

type RequestContext string

const (
	ContextUnknown RequestContext = "unknown"
	ContextPublic  RequestContext = "public"
	ContextPrivate RequestContext = "private"
)

type Frontmatter struct {
	CreatedAt       string   `yaml:"created_at"`
	UpdatedAt       string   `yaml:"updated_at"`
	Summary         string   `yaml:"summary"`
	Tasks           string   `yaml:"tasks"`
	FollowUps       string   `yaml:"follow_ups"`
	SessionID       string   `yaml:"session_id,omitempty"`
	Source          string   `yaml:"source,omitempty"`
	Channel         string   `yaml:"channel,omitempty"`
	Tags            []string `yaml:"tags,omitempty"`
	Usernames       []string `yaml:"usernames,omitempty"`
	SubjectID       string   `yaml:"subject_id,omitempty"`
	ContactID       string   `yaml:"contact_id,omitempty"`
	ContactNickname string   `yaml:"contact_nickname,omitempty"`
}

type Manager struct {
	Dir           string
	ShortTermDays int
	Now           func() time.Time
}

type KVItem struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type TaskItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

type PromoteDraft struct {
	GoalsProjects []KVItem `json:"goals_projects"`
	KeyFacts      []KVItem `json:"key_facts"`
}

type SessionDraft struct {
	Summary        string       `json:"summary"`
	SessionSummary []KVItem     `json:"session_summary"`
	TemporaryFacts []KVItem     `json:"temporary_facts"`
	Tasks          []TaskItem   `json:"tasks"`
	FollowUps      []TaskItem   `json:"follow_ups"`
	Promote        PromoteDraft `json:"promote"`
}

type ShortTermSummary struct {
	Date           string
	Summary        string
	RelPath        string
	TasksDone      int
	TasksTotal     int
	FollowUpsDone  int
	FollowUpsTotal int
}

// ShortTermContent is the parsed representation of a short-term session file.
type ShortTermContent struct {
	SessionSummary []KVItem
	TemporaryFacts []KVItem
	Tasks          []TaskItem
	FollowUps      []TaskItem
	RelatedLinks   []LinkItem
}

type LongTermContent struct {
	Goals []KVItem
	Facts []KVItem
}

type LinkItem struct {
	Text   string
	Target string
}
