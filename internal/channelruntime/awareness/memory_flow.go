package awareness

import (
	"fmt"
	"time"

	"github.com/quailyquaily/mistermorph/internal/awarenessutil"
	"github.com/quailyquaily/mistermorph/memory"
)

const (
	awarenessMemorySubjectID = "awareness"
	awarenessMemorySessionID = "awareness"
)

func awarenessTaskRunID(behavior awarenessutil.Behavior, now time.Time) string {
	now = now.UTC()
	return fmt.Sprintf("%s:%s", behavior, now.Format("20060102T150405.000000000Z07:00"))
}

func awarenessMemoryParticipants() []memory.MemoryParticipant {
	return []memory.MemoryParticipant{{
		ID:       0,
		Nickname: "agent",
		Protocol: "",
	}}
}
