package memory

type RequestContext string

const (
	ContextUnknown RequestContext = "unknown"
	ContextPublic  RequestContext = "public"
	ContextPrivate RequestContext = "private"
)

type Visibility int

const (
	PublicOK    Visibility = 0
	PrivateOnly Visibility = 1
)

var NamespacesAllowlist = []string{
	"profile",
	"preference",
	"fact",
	"project",
	"task_state",
}

func IsAllowedNamespace(ns string) bool {
	for _, a := range NamespacesAllowlist {
		if ns == a {
			return true
		}
	}
	return false
}

type Item struct {
	SubjectID  string
	Namespace  string
	Key        string
	Value      string
	Visibility Visibility
	Confidence *float64
	Source     *string
	CreatedAt  int64
	UpdatedAt  int64
}

type ReadOptions struct {
	Context RequestContext
	Limit   int
	Prefix  string
}

type PutOptions struct {
	Visibility *Visibility
	Confidence *float64
	Source     *string
}
