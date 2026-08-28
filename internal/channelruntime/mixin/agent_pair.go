package mixin

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
	"github.com/quailyquaily/mistermorph/internal/mixinapi"
)

func mixinPairTarget(ctx context.Context, service *contacts.Service, args string) (agentpair.Peer, error) {
	identityNumber := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(args), "@"))
	if identityNumber == "" || strings.ContainsAny(identityNumber, " \t\r\n:@()") || strings.Count(strings.TrimSpace(args), "@") != 1 {
		return agentpair.Peer{}, fmt.Errorf("usage: /pair @Agent")
	}
	if service == nil {
		return agentpair.Peer{}, fmt.Errorf("contacts service is unavailable")
	}
	items, err := service.ListContacts(ctx, contacts.StatusActive)
	if err != nil {
		return agentpair.Peer{}, err
	}
	var match *contacts.Contact
	for index := range items {
		contact := items[index]
		if contact.Kind != contacts.KindAgent || !strings.EqualFold(strings.TrimSpace(contact.Channel), contacts.ChannelMixin) || strings.TrimSpace(contact.MixinIdentityNumber) != identityNumber {
			continue
		}
		if match != nil {
			return agentpair.Peer{}, fmt.Errorf("Mixin identity number matches multiple Agent contacts")
		}
		match = &contact
	}
	if match == nil || strings.TrimSpace(match.MixinUserID) == "" {
		return agentpair.Peer{}, fmt.Errorf("pair target must be an existing Mixin Agent contact")
	}
	return agentpair.Peer{ID: "mixin:@" + identityNumber, Contact: *match}, nil
}

func mixinInboundAgentPeer(user mixinapi.User, conversationID string) agentpair.Peer {
	userID := strings.TrimSpace(user.UserID)
	contactID := "mixin:" + userID
	chatIDs := []string(nil)
	if conversationID = strings.TrimSpace(conversationID); conversationID != "" {
		chatIDs = []string{conversationID}
	}
	kind := contacts.KindHuman
	if strings.TrimSpace(user.AppID) != "" {
		kind = contacts.KindAgent
	}
	return agentpair.Peer{
		ID: "mixin:" + userID,
		Contact: contacts.Contact{
			ContactID: contactID, Kind: kind, Channel: contacts.ChannelMixin,
			ContactNickname: strings.TrimSpace(user.FullName), MixinUserID: userID,
			MixinIdentityNumber: strings.TrimSpace(user.IdentityNumber), MixinChatIDs: chatIDs,
		},
	}
}

func mixinPairSendUserID(peer agentpair.Peer) (string, error) {
	if userID := strings.TrimSpace(peer.Contact.MixinUserID); userID != "" {
		return userID, nil
	}
	if id := strings.TrimSpace(peer.ID); strings.HasPrefix(strings.ToLower(id), "mixin:") && !strings.HasPrefix(strings.ToLower(id), "mixin:@") {
		return strings.TrimSpace(id[len("mixin:"):]), nil
	}
	return "", fmt.Errorf("Mixin pair target requires a user UUID")
}

func mixinConversationAuthorized(allowed map[string]bool, conversationID string, isGroup, fromAgent, pairedAgent bool) bool {
	if len(allowed) == 0 || allowed[strings.TrimSpace(conversationID)] {
		return true
	}
	return !isGroup && fromAgent && pairedAgent
}

func mixinPairReplyText(status agentpair.Status, err error) string {
	if err != nil {
		return "Pairing failed: " + strings.TrimSpace(err.Error())
	}
	switch status {
	case agentpair.StatusCompleted:
		return "Agent pairing completed."
	case agentpair.StatusAlreadyPaired:
		return "This Agent is already paired."
	default:
		return "Pairing request sent. It expires in 5 minutes."
	}
}
