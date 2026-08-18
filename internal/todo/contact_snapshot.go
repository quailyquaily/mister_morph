package todo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	refid "github.com/quailyquaily/mistermorph/internal/entryutil/refid"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
)

type ContactSnapshot struct {
	Contacts     []ContactSnapshotItem `json:"contacts"`
	ReachableIDs []string              `json:"reachable_ids"`
}

type ContactSnapshotItem struct {
	ContactID    string   `json:"contact_id,omitempty"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases,omitempty"`
	Usernames    []string `json:"usernames,omitempty"`
	ReachableIDs []string `json:"reachable_ids"`
	PreferredID  string   `json:"preferred_id,omitempty"`
}

func LoadContactSnapshot(ctx context.Context, contactsDir string) (ContactSnapshot, error) {
	contactsDir = pathutil.ExpandHomePath(strings.TrimSpace(contactsDir))
	if contactsDir == "" {
		return ContactSnapshot{}, fmt.Errorf("contacts dir is not configured")
	}
	svc := contacts.NewService(contacts.NewFileStore(contactsDir))
	items, err := svc.ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return ContactSnapshot{}, err
	}
	out := ContactSnapshot{
		Contacts: make([]ContactSnapshotItem, 0, len(items)),
	}
	reachableAll := make([]string, 0, len(items)*2)
	for _, item := range items {
		reachable := contactReachableIDs(item)
		reachableAll = append(reachableAll, reachable...)
		name := chooseContactName(item)
		if name == "" {
			continue
		}
		aliases := dedupeSortedStrings([]string{
			strings.TrimSpace(item.ContactNickname),
			extractTelegramAlias(item.ContactID),
		})
		usernames := dedupeSortedStrings([]string{
			strings.TrimSpace(item.TGUsername),
			extractTelegramAlias(item.ContactID),
		})
		preferred := choosePreferredID(item, reachable)
		out.Contacts = append(out.Contacts, ContactSnapshotItem{
			ContactID:    strings.TrimSpace(item.ContactID),
			Name:         name,
			Aliases:      aliases,
			Usernames:    usernames,
			ReachableIDs: reachable,
			PreferredID:  preferred,
		})
	}
	sort.Slice(out.Contacts, func(i, j int) bool {
		if out.Contacts[i].Name == out.Contacts[j].Name {
			return out.Contacts[i].ContactID < out.Contacts[j].ContactID
		}
		return out.Contacts[i].Name < out.Contacts[j].Name
	})
	out.ReachableIDs = dedupeSortedStrings(reachableAll)
	return out, nil
}

func chooseContactName(item contacts.Contact) string {
	for _, raw := range []string{
		item.ContactNickname,
		extractTelegramAlias(item.ContactID),
		item.ContactID,
	} {
		val := strings.TrimSpace(raw)
		if val != "" {
			return val
		}
	}
	return ""
}

func choosePreferredID(item contacts.Contact, reachable []string) string {
	candidates := []string{strings.TrimSpace(item.ContactID)}
	if item.TGPrivateChatID > 0 {
		candidates = append(candidates, "tg:"+strconv.FormatInt(item.TGPrivateChatID, 10))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if !refid.IsValid(candidate) {
			continue
		}
		for _, id := range reachable {
			if id == candidate {
				return id
			}
		}
	}
	if len(reachable) == 0 {
		return ""
	}
	return reachable[0]
}

func contactReachableIDs(item contacts.Contact) []string {
	ids := make([]string, 0, 8)
	appendID := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if !refid.IsValid(v) {
			return
		}
		ids = append(ids, v)
	}

	appendID(item.ContactID)
	if item.TGPrivateChatID > 0 {
		appendID(fmt.Sprintf("tg:%d", item.TGPrivateChatID))
	}
	for _, groupID := range item.TGGroupChatIDs {
		if groupID != 0 {
			appendID(fmt.Sprintf("tg:%d", groupID))
		}
	}
	return dedupeSortedStrings(ids)
}

func extractTelegramAlias(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "tg:@") {
		return strings.TrimPrefix(raw, "tg:@")
	}
	if strings.HasPrefix(raw, "@") {
		return strings.TrimPrefix(raw, "@")
	}
	return ""
}
