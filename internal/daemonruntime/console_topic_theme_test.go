package daemonruntime

import (
	"path/filepath"
	"testing"
)

func TestConsoleTopicThemePersistsAndDoesNotOverrideManualTitle(t *testing.T) {
	for _, mode := range []string{"generated", "edited", "edited-back", "deleted", "already-generated"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			opts := ConsoleFileStoreOptions{RootDir: root, JournalDir: filepath.Join(root, "journal"), Persist: true}
			store, err := NewConsoleFileStore(opts)
			if err != nil {
				t.Fatal(err)
			}
			topic, err := store.CreateTopic("seed")
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "edited", "edited-back":
				if err := store.SetTopicTitle(topic.ID, "mine"); err != nil {
					t.Fatal(err)
				}
				if mode == "edited-back" {
					if err := store.SetTopicTitle(topic.ID, "seed"); err != nil {
						t.Fatal(err)
					}
				}
			case "deleted":
				if _, err := store.DeleteTopic(topic.ID); err != nil {
					t.Fatal(err)
				}
			case "already-generated":
				if err := store.SetTopicTitleFromLLM(topic.ID, "seed", "first", "code"); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.SetTopicTitleFromLLM(topic.ID, "seed", "generated", "book-open"); err != nil && mode != "deleted" {
				t.Fatal(err)
			}
			reloaded, err := NewConsoleFileStore(opts)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := reloaded.GetTopic(topic.ID)
			if mode == "deleted" {
				if ok && got.DeletedAt == nil {
					t.Fatal("deleted topic restored")
				}
				return
			}
			if !ok {
				t.Fatal("topic missing")
			}
			switch mode {
			case "generated":
				if got.Title != "generated" || got.Icon != "book-open" || got.LLMTitleGeneratedAt == nil {
					t.Fatalf("topic = %+v", got)
				}
			case "already-generated":
				if got.Title != "first" || got.Icon != "code" {
					t.Fatalf("topic = %+v", got)
				}
			default:
				want := "mine"
				if mode == "edited-back" {
					want = "seed"
				}
				if got.Title != want || got.LLMTitleGeneratedAt != nil {
					t.Fatalf("manual title overwritten: %+v", got)
				}
			}
		})
	}
}
