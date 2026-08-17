package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/agentpair"
)

func slackPairTarget(args, teamID string) (agentpair.Peer, error) {
	target := strings.TrimSpace(args)
	match := slackMentionPattern.FindStringSubmatch(target)
	if len(match) < 2 || match[0] != target {
		return agentpair.Peer{}, fmt.Errorf("usage: /pair @Agent")
	}
	teamID = strings.ToUpper(strings.TrimSpace(teamID))
	userID := strings.ToUpper(strings.TrimSpace(match[1]))
	if !strings.HasPrefix(teamID, "T") || !(strings.HasPrefix(userID, "U") || strings.HasPrefix(userID, "W")) {
		return agentpair.Peer{}, fmt.Errorf("pair target must be a Slack user")
	}
	stableID := "slack:" + teamID + ":" + userID
	return agentpair.Peer{
		ID: stableID,
		Contact: contacts.Contact{
			ContactID:   stableID,
			Kind:        contacts.KindAgent,
			Channel:     contacts.ChannelSlack,
			SlackTeamID: teamID,
			SlackUserID: userID,
		},
	}, nil
}

func slackInboundAgentPeer(teamID, userID, username, displayName, dmChannelID string) agentpair.Peer {
	teamID = strings.ToUpper(strings.TrimSpace(teamID))
	userID = strings.ToUpper(strings.TrimSpace(userID))
	stableID := "slack:" + teamID + ":" + userID
	nickname := strings.TrimSpace(displayName)
	if nickname == "" {
		nickname = strings.TrimSpace(username)
	}
	return agentpair.Peer{
		ID: stableID,
		Contact: contacts.Contact{
			ContactID:        stableID,
			Kind:             contacts.KindAgent,
			Channel:          contacts.ChannelSlack,
			ContactNickname:  nickname,
			SlackTeamID:      teamID,
			SlackUserID:      userID,
			SlackDMChannelID: strings.TrimSpace(dmChannelID),
		},
	}
}

func slackChatAuthorized(allowedTeams, allowedChannels map[string]bool, teamID, channelID, chatType string, fromAgent, pairedAgent bool) bool {
	teamAllowed := len(allowedTeams) == 0 || allowedTeams[strings.TrimSpace(teamID)]
	channelAllowed := len(allowedChannels) == 0 || allowedChannels[strings.TrimSpace(channelID)]
	if teamAllowed && channelAllowed {
		return true
	}
	return !isSlackGroupChat(chatType) && fromAgent && pairedAgent
}

func slackPairSendUserID(peer agentpair.Peer) (string, error) {
	parts := strings.Split(strings.TrimSpace(peer.ID), ":")
	if len(parts) != 3 || !strings.EqualFold(parts[0], "slack") {
		return "", fmt.Errorf("Slack pair target is invalid")
	}
	userID := strings.ToUpper(strings.TrimSpace(parts[2]))
	if !(strings.HasPrefix(userID, "U") || strings.HasPrefix(userID, "W")) {
		return "", fmt.Errorf("Slack pair target must be a user")
	}
	return userID, nil
}

func sendSlackPairReply(ctx context.Context, api *slackAPI, channelID string, status agentpair.Status, err error) {
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
	_ = api.postMessage(ctx, channelID, reply, "")
}
