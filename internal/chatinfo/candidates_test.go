package chatinfo

import (
	"context"
	"reflect"
	"testing"

	"github.com/quailyquaily/mistermorph/contacts"
)

func TestActiveContactCandidateIDs(t *testing.T) {
	ctx := context.Background()
	contactsDir := t.TempDir()
	store := contacts.NewFileStore(contactsDir)
	for _, contact := range []contacts.Contact{
		{
			ContactID:       "tg:@alice",
			Kind:            contacts.KindHuman,
			Channel:         contacts.ChannelTelegram,
			TGPrivateChatID: 2001,
			TGGroupChatIDs:  []int64{-1001},
		},
		{
			ContactID:        "slack:T111:U222",
			Kind:             contacts.KindHuman,
			Channel:          contacts.ChannelSlack,
			SlackTeamID:      "T111",
			SlackDMChannelID: "D333",
			SlackChannelIDs:  []string{"C999", "G888"},
		},
		{
			ContactID:   "line_user:Ualice",
			Kind:        contacts.KindHuman,
			Channel:     contacts.ChannelLine,
			LineChatIDs: []string{"Cline"},
		},
		{
			ContactID:   "lark_user:ou_alice",
			Kind:        contacts.KindHuman,
			Channel:     contacts.ChannelLark,
			LarkChatIDs: []string{"oc_lark"},
		},
		{
			ContactID: "slack:T111:U444",
			Kind:      contacts.KindHuman,
			Channel:   contacts.ChannelSlack,
		},
	} {
		if err := store.PutContact(ctx, contact); err != nil {
			t.Fatalf("PutContact(%s) error = %v", contact.ContactID, err)
		}
	}

	got, err := ActiveContactCandidateIDs(ctx, contactsDir)
	if err != nil {
		t.Fatalf("ActiveContactCandidateIDs() error = %v", err)
	}
	want := []string{
		"lark:oc_lark",
		"line:Cline",
		"slack:T111:C999",
		"slack:T111:D333",
		"slack:T111:G888",
		"tg:-1001",
		"tg:2001",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestActiveContactCandidateIDsMissingFile(t *testing.T) {
	got, err := ActiveContactCandidateIDs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("ActiveContactCandidateIDs() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("candidates = %#v, want empty", got)
	}
}
