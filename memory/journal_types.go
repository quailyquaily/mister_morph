package memory

type JournalCursor struct {
	File string `json:"file"`
	Line int64  `json:"line,omitempty"`
	Byte int64  `json:"byte,omitempty"`
}

type JournalRecord struct {
	Cursor JournalCursor `json:"cursor"`
	Event  MemoryEvent   `json:"event"`
}

type JournalCheckpoint struct {
	File      string `json:"file"`
	Line      int64  `json:"line,omitempty"`
	Byte      int64  `json:"byte,omitempty"`
	UpdatedAt string `json:"updated_at"`
}
