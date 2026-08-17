package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
)

func telegramPairTarget(args string) (agentpair.Peer, error) {
	target := strings.TrimSpace(args)
	if !strings.HasPrefix(target, "@") || strings.Count(target, "@") != 1 || strings.ContainsAny(target, " \t\r\n:") || len(target) == 1 {
		return agentpair.Peer{}, fmt.Errorf("usage: /pair @Agent")
	}
	username := strings.TrimPrefix(target, "@")
	return agentpair.Peer{
		ID: "tg:@" + username,
		Contact: contacts.Contact{
			ContactID:  "tg:@" + username,
			Kind:       contacts.KindAgent,
			Channel:    contacts.ChannelTelegram,
			TGUsername: username,
		},
	}, nil
}

func telegramInboundAgentPeer(userID int64, username, displayName string, privateChatID int64) agentpair.Peer {
	stableID := "tg:" + strconv.FormatInt(userID, 10)
	username = strings.TrimSpace(strings.TrimPrefix(username, "@"))
	contactID := stableID
	if username != "" {
		contactID = "tg:@" + username
	}
	return agentpair.Peer{
		ID: stableID,
		Contact: contacts.Contact{
			ContactID:       contactID,
			Kind:            contacts.KindAgent,
			Channel:         contacts.ChannelTelegram,
			ContactNickname: strings.TrimSpace(displayName),
			TGUsername:      username,
			TGPrivateChatID: privateChatID,
		},
	}
}

func telegramChatAuthorized(allowedChatIDs map[int64]bool, chatID int64, isGroup, fromAgent, pairedAgent bool) bool {
	if len(allowedChatIDs) == 0 || allowedChatIDs[chatID] {
		return true
	}
	return !isGroup && fromAgent && pairedAgent
}

func telegramPairSendTarget(peer agentpair.Peer) (string, error) {
	refs := []string{peer.ID}
	if username := strings.TrimSpace(strings.TrimPrefix(peer.Contact.TGUsername, "@")); username != "" {
		refs = append(refs, "tg:@"+username)
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(strings.ToLower(ref), "tg:@") {
			username := strings.TrimSpace(ref[len("tg:"):])
			if len(username) > 1 {
				return username, nil
			}
		}
	}
	return "", fmt.Errorf("Telegram pair target requires a username")
}

func sendTelegramPairReply(ctx context.Context, api *telegramAPI, chatID, messageThreadID int64, status agentpair.Status, err error) {
	if api == nil {
		return
	}
	reply := "Pairing request sent. It expires in 5 minutes."
	if err != nil {
		reply = "Pairing failed: " + strings.TrimSpace(err.Error())
	} else {
		switch status {
		case agentpair.StatusCompleted:
			reply = "Agent pairing completed."
		case agentpair.StatusAlreadyPaired:
			reply = "This Agent is already paired."
		}
	}
	_ = api.sendMessageHTMLInThread(ctx, chatID, messageThreadID, reply, true)
}
