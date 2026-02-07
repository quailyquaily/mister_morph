package contactscmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/quailyquaily/mistermorph/contacts"
	"github.com/quailyquaily/mistermorph/internal/contactsruntime"
	"github.com/quailyquaily/mistermorph/internal/llmconfig"
	"github.com/quailyquaily/mistermorph/internal/llmutil"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/llm"
	"github.com/quailyquaily/mistermorph/maep"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "Manage business-layer contacts and proactive sharing",
	}
	cmd.PersistentFlags().String("dir", "", "Contacts state directory (defaults to file_state_dir/contacts)")

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newUpsertCmd())
	cmd.AddCommand(newSetStatusCmd())
	cmd.AddCommand(newCandidateCmd())
	cmd.AddCommand(newProactiveCmd())
	cmd.AddCommand(newAuditCmd())
	cmd.AddCommand(newSyncMAEPCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var status string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			records, err := svc.ListContacts(cmd.Context(), parseStatus(status))
			if err != nil {
				return err
			}
			sort.Slice(records, func(i, j int) bool {
				if records[i].Status != records[j].Status {
					return records[i].Status < records[j].Status
				}
				return strings.TrimSpace(records[i].ContactID) < strings.TrimSpace(records[j].ContactID)
			})
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), records)
			}
			if len(records) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no contacts")
				return nil
			}
			for _, item := range records {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%s\t%s\t%s\t%s\tdepth=%.1f\tretain=%.3f\n",
					item.ContactID,
					item.Status,
					item.Kind,
					item.PeerID,
					item.TrustState,
					item.ContactNickname,
					item.UnderstandingDepth,
					item.RetainScore,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "all", "Filter by status: all|active|inactive")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newUpsertCmd() *cobra.Command {
	var kind string
	var status string
	var peerID string
	var nodeID string
	var subjectID string
	var contactNickname string
	var personaBrief string
	var displayName string
	var telegramUsername string
	var telegramNickname string
	var trustState string
	var depth float64
	var reciprocity float64
	var addresses []string
	var topicWeights []string
	var personaTraits []string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "upsert [contact_id]",
		Short: "Create or update one contact",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topicMap, err := parseFloatAssignments(topicWeights)
			if err != nil {
				return err
			}
			traitMap, err := parseFloatAssignments(personaTraits)
			if err != nil {
				return err
			}
			contactID := ""
			if len(args) > 0 {
				contactID = strings.TrimSpace(args[0])
			}
			telegramID := telegramContactID(telegramUsername)
			kindValue := contacts.Kind(strings.TrimSpace(strings.ToLower(kind)))
			nickname := strings.TrimSpace(contactNickname)
			if nickname == "" {
				nickname = strings.TrimSpace(displayName)
			}
			if nickname == "" {
				nickname = strings.TrimSpace(telegramNickname)
			}
			if kindValue == contacts.KindHuman && strings.TrimSpace(subjectID) == "" && telegramID != "" {
				subjectID = telegramID
			}
			if kindValue == contacts.KindHuman && contactID == "" && telegramID != "" {
				contactID = telegramID
			}
			svc := serviceFromCmd(cmd)
			updated, err := svc.UpsertContact(cmd.Context(), contacts.Contact{
				ContactID:          contactID,
				Kind:               kindValue,
				Status:             parseStatus(status),
				ContactNickname:    nickname,
				PersonaBrief:       strings.TrimSpace(personaBrief),
				PersonaTraits:      traitMap,
				SubjectID:          strings.TrimSpace(subjectID),
				NodeID:             strings.TrimSpace(nodeID),
				PeerID:             strings.TrimSpace(peerID),
				Addresses:          normalizeList(addresses),
				TrustState:         strings.TrimSpace(strings.ToLower(trustState)),
				UnderstandingDepth: depth,
				TopicWeights:       topicMap,
				ReciprocityNorm:    reciprocity,
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), updated)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "contact_id: %s\nstatus: %s\nkind: %s\npeer_id: %s\ncontact_nickname: %s\n", updated.ContactID, updated.Status, updated.Kind, updated.PeerID, updated.ContactNickname)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "agent", "Contact kind: agent|human")
	cmd.Flags().StringVar(&status, "status", "active", "Contact status: active|inactive")
	cmd.Flags().StringVar(&peerID, "peer-id", "", "MAEP peer_id for agent contact")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "MAEP node_id for agent contact")
	cmd.Flags().StringVar(&subjectID, "subject-id", "", "Subject id for human contact")
	cmd.Flags().StringVar(&contactNickname, "contact-nickname", "", "Contact nickname")
	cmd.Flags().StringVar(&personaBrief, "persona-brief", "", "Personality summary for this contact")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Legacy alias of --contact-nickname")
	cmd.Flags().StringVar(&telegramUsername, "telegram-username", "", "Telegram username for human contact (maps to tg:@<username>)")
	cmd.Flags().StringVar(&telegramNickname, "telegram-nickname", "", "Telegram nickname (fallback for contact_nickname)")
	cmd.Flags().StringVar(&trustState, "trust-state", "verified", "Trust state (verified recommended)")
	cmd.Flags().Float64Var(&depth, "understanding-depth", 30, "Understanding depth [0,100]")
	cmd.Flags().Float64Var(&reciprocity, "reciprocity", 0.5, "Reciprocity score [0,1]")
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Dial address for MAEP peer (repeatable)")
	cmd.Flags().StringArrayVar(&topicWeights, "topic-weight", nil, "Topic affinity (repeatable): topic=score")
	cmd.Flags().StringArrayVar(&personaTraits, "persona-trait", nil, "Persona trait weight (repeatable): trait=score")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newSetStatusCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "set-status <contact_id> <status>",
		Short: "Move a contact between active/inactive",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			contactID := strings.TrimSpace(args[0])
			if contactID == "" {
				return fmt.Errorf("contact_id is required")
			}
			status := parseStatus(args[1])
			if status != contacts.StatusActive && status != contacts.StatusInactive {
				return fmt.Errorf("invalid status %q (want active|inactive)", args[1])
			}
			svc := serviceFromCmd(cmd)
			updated, err := svc.SetContactStatus(cmd.Context(), contactID, status)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), updated)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "contact_id: %s\nstatus: %s\n", updated.ContactID, updated.Status)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCandidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candidate",
		Short: "Manage share candidates",
	}
	cmd.AddCommand(newCandidateAddCmd())
	cmd.AddCommand(newCandidateListCmd())
	return cmd
}

func newCandidateAddCmd() *cobra.Command {
	var itemID string
	var topic string
	var topics []string
	var text string
	var contentType string
	var sensitivity string
	var depthHint float64
	var sourceChatID int64
	var sourceChatType string
	var sourceRef string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add or update one proactive share candidate",
		RunE: func(cmd *cobra.Command, args []string) error {
			topic = strings.TrimSpace(topic)
			text = strings.TrimSpace(text)
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			if text == "" {
				return fmt.Errorf("--text is required")
			}
			contentType = strings.TrimSpace(contentType)
			if contentType == "" {
				contentType = "text/plain"
			}
			payload := base64.RawURLEncoding.EncodeToString([]byte(text))

			svc := serviceFromCmd(cmd)
			candidate, err := svc.AddCandidate(cmd.Context(), contacts.ShareCandidate{
				ItemID:           strings.TrimSpace(itemID),
				Topic:            topic,
				Topics:           normalizeList(topics),
				ContentType:      contentType,
				PayloadBase64:    payload,
				SensitivityLevel: strings.TrimSpace(strings.ToLower(sensitivity)),
				DepthHint:        depthHint,
				SourceChatID:     sourceChatID,
				SourceChatType:   strings.TrimSpace(strings.ToLower(sourceChatType)),
				SourceRef:        strings.TrimSpace(sourceRef),
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), candidate)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "item_id: %s\ntopic: %s\ncontent_type: %s\n", candidate.ItemID, candidate.Topic, candidate.ContentType)
			return nil
		},
	}
	cmd.Flags().StringVar(&itemID, "item-id", "", "Candidate id (auto-generated if empty)")
	cmd.Flags().StringVar(&topic, "topic", "", "Primary topic (required)")
	cmd.Flags().StringArrayVar(&topics, "topics", nil, "Additional topics (repeatable)")
	cmd.Flags().StringVar(&text, "text", "", "Payload text (required)")
	cmd.Flags().StringVar(&contentType, "content-type", "text/plain", "Content type")
	cmd.Flags().StringVar(&sensitivity, "sensitivity", "low", "Sensitivity level: low|medium|high")
	cmd.Flags().Float64Var(&depthHint, "depth-hint", 0.5, "Suggested depth [0,1]")
	cmd.Flags().Int64Var(&sourceChatID, "source-chat-id", 0, "Telegram source chat id related to this candidate")
	cmd.Flags().StringVar(&sourceChatType, "source-chat-type", "", "Telegram source chat type: private|group|supergroup")
	cmd.Flags().StringVar(&sourceRef, "source-ref", "", "Optional source reference")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCandidateListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List candidates",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			items, err := svc.ListCandidates(cmd.Context())
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), items)
			}
			if len(items) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no candidates")
				return nil
			}
			for _, item := range items {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ItemID, item.Topic, item.ContentType, item.CreatedAt.UTC().Format(time.RFC3339))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newProactiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proactive",
		Short: "Run proactive sharing workflows",
	}
	cmd.AddCommand(newProactiveTickCmd())
	return cmd
}

func newProactiveTickCmd() *cobra.Command {
	var send bool
	var maxTargets int
	var freshnessWindow time.Duration
	var maxLinkedHistoryItems int
	var humanEnabled bool
	var humanSendEnabled bool
	var humanPublicSendEnabled bool
	var outputJSON bool
	var maepDir string
	var telegramBotToken string
	var telegramBaseURL string

	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one proactive-sharing tick",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("human-enabled") {
				humanEnabled = viper.GetBool("contacts.human.enabled")
			}
			if !cmd.Flags().Changed("human-send-enabled") {
				humanSendEnabled = viper.GetBool("contacts.human.send.enabled")
			}
			if !cmd.Flags().Changed("human-public-send-enabled") {
				humanPublicSendEnabled = viper.GetBool("contacts.human.send.public_enabled")
			}

			svc := serviceFromCmd(cmd)

			var sender contacts.Sender
			cleanup := func() {}
			if send {
				s, closeFn, err := newProactiveSender(cmd, maepDir, telegramBotToken, telegramBaseURL, humanSendEnabled, humanPublicSendEnabled)
				if err != nil {
					return err
				}
				sender = s
				cleanup = closeFn
			}
			defer cleanup()

			var featureExtractor contacts.FeatureExtractor
			var preferenceExtractor contacts.PreferenceExtractor
			var nicknameGenerator contacts.NicknameGenerator
			client, extractorModel, err := newLLMClientForFeatures()
			if err != nil {
				return err
			}
			llmExtractor := contacts.NewLLMFeatureExtractor(client, extractorModel)
			featureExtractor = llmExtractor
			preferenceExtractor = llmExtractor
			nicknameGenerator = contacts.NewLLMNicknameGenerator(client, extractorModel)

			result, err := svc.RunTick(cmd.Context(), time.Now().UTC(), contacts.TickOptions{
				MaxTargets:            maxTargets,
				FreshnessWindow:       freshnessWindow,
				PushTopic:             "share.proactive.v1",
				Send:                  send,
				EnableHumanContacts:   humanEnabled,
				EnableHumanSend:       humanSendEnabled,
				EnableHumanPublicSend: humanPublicSendEnabled,
				MaxLinkedHistoryItems: maxLinkedHistoryItems,
				FeatureExtractor:      featureExtractor,
				PreferenceExtractor:   preferenceExtractor,
				NicknameGenerator:     nicknameGenerator,
				NicknameMinConfidence: 0.70,
			}, sender)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "tick_id: %s\nplanned: %d\nsent: %d\n", result.TickID, result.Planned, result.Sent)
			for _, d := range result.Decisions {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "decision: contact=%s peer=%s item=%s score=%.3f\n", d.ContactID, d.PeerID, d.ItemID, d.Score)
			}
			for _, o := range result.Outcomes {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "outcome: contact=%s peer=%s item=%s accepted=%v deduped=%v err=%s\n", o.ContactID, o.PeerID, o.ItemID, o.Accepted, o.Deduped, o.Error)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&send, "send", false, "Actually send (agent->MAEP, Telegram human->Telegram API). Default: dry-run only")
	cmd.Flags().IntVar(&maxTargets, "max-targets", contacts.DefaultMaxTargetsPerTick, "Max contacts selected per tick")
	cmd.Flags().DurationVar(&freshnessWindow, "freshness-window", contacts.DefaultFreshnessWindow, "Candidate freshness window")
	cmd.Flags().IntVar(&maxLinkedHistoryItems, "max-linked-history-items", 4, "Max linked history items allowed in one decision")
	cmd.Flags().BoolVar(&humanEnabled, "human-enabled", true, "Enable human contacts in proactive selection")
	cmd.Flags().BoolVar(&humanSendEnabled, "human-send-enabled", true, "Allow proactive --send to human contacts")
	cmd.Flags().BoolVar(&humanPublicSendEnabled, "human-public-send-enabled", false, "Allow proactive --send to public chats for human contacts")
	cmd.Flags().StringVar(&maepDir, "maep-dir", "", "MAEP state directory for send mode (defaults to file_state_dir/maep)")
	cmd.Flags().StringVar(&telegramBotToken, "telegram-bot-token", strings.TrimSpace(viper.GetString("telegram.bot_token")), "Telegram bot token for sending to human contacts")
	cmd.Flags().StringVar(&telegramBaseURL, "telegram-base-url", "https://api.telegram.org", "Telegram API base URL")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query proactive decision audit",
	}
	cmd.AddCommand(newAuditListCmd())
	return cmd
}

func newAuditListCmd() *cobra.Command {
	var tickID string
	var contactID string
	var action string
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List proactive audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			events, err := svc.ListAuditEvents(cmd.Context(), tickID, contactID, action, limit)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), events)
			}
			if len(events) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no audit events")
				return nil
			}
			for _, event := range events {
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%s\t%s\t%s\t%s\n",
					event.CreatedAt.UTC().Format(time.RFC3339),
					event.TickID,
					event.Action,
					event.ContactID,
					event.ItemID,
					event.Reason,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tickID, "tick-id", "", "Filter by tick id")
	cmd.Flags().StringVar(&contactID, "contact-id", "", "Filter by contact id")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action")
	cmd.Flags().IntVar(&limit, "limit", 100, "Max records (<=0 means all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newSyncMAEPCmd() *cobra.Command {
	var maepDir string
	var includeTOFU bool
	cmd := &cobra.Command{
		Use:   "sync-maep",
		Short: "Import MAEP contacts into contacts business store",
		RunE: func(cmd *cobra.Command, args []string) error {
			maepSvc := maepServiceFromDir(maepDir)
			maepContacts, err := maepSvc.ListContacts(cmd.Context())
			if err != nil {
				return err
			}
			svc := serviceFromCmd(cmd)
			existingContacts, err := svc.ListContacts(cmd.Context(), "")
			if err != nil {
				return err
			}
			existingByNodeID := map[string]string{}
			for _, item := range existingContacts {
				nodeID := strings.TrimSpace(item.NodeID)
				contactID := strings.TrimSpace(item.ContactID)
				if nodeID == "" || contactID == "" {
					continue
				}
				if _, ok := existingByNodeID[nodeID]; ok {
					continue
				}
				existingByNodeID[nodeID] = contactID
			}

			now := time.Now().UTC()
			imported := 0
			for _, item := range maepContacts {
				trust := strings.TrimSpace(strings.ToLower(string(item.TrustState)))
				if trust != "verified" && !(includeTOFU && trust == "tofu") {
					continue
				}
				nodeID := strings.TrimSpace(item.NodeID)
				targetContactID := resolveSyncMAEPTargetContactID(existingByNodeID, nodeID, item.PeerID)
				record := contacts.Contact{
					ContactID:          targetContactID,
					Kind:               contacts.KindAgent,
					Status:             contacts.StatusActive,
					ContactNickname:    strings.TrimSpace(item.DisplayName),
					NodeID:             nodeID,
					PeerID:             strings.TrimSpace(item.PeerID),
					Addresses:          normalizeList(item.Addresses),
					TrustState:         trust,
					UnderstandingDepth: 30,
					ReciprocityNorm:    0.5,
				}
				updated, err := svc.UpsertContact(cmd.Context(), record, now)
				if err != nil {
					return err
				}
				if nodeID != "" {
					existingByNodeID[nodeID] = strings.TrimSpace(updated.ContactID)
				}
				imported++
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "imported: %d\n", imported)
			return nil
		},
	}
	cmd.Flags().StringVar(&maepDir, "maep-dir", "", "MAEP state directory (defaults to file_state_dir/maep)")
	cmd.Flags().BoolVar(&includeTOFU, "include-tofu", false, "Include TOFU contacts (default only verified)")
	return cmd
}

func newProactiveSender(
	cmd *cobra.Command,
	maepDir string,
	telegramBotToken string,
	telegramBaseURL string,
	allowHumanSend bool,
	allowHumanPublicSend bool,
) (contacts.Sender, func(), error) {
	token := strings.TrimSpace(telegramBotToken)
	if token == "" {
		token = strings.TrimSpace(viper.GetString("telegram.bot_token"))
	}
	sender, err := contactsruntime.NewRoutingSender(cmd.Context(), contactsruntime.SenderOptions{
		MAEPDir:              maepDir,
		TelegramBotToken:     token,
		TelegramBaseURL:      telegramBaseURL,
		AllowHumanSend:       allowHumanSend,
		AllowHumanPublicSend: allowHumanPublicSend,
	})
	if err != nil {
		return nil, nil, err
	}
	return sender, func() { _ = sender.Close() }, nil
}

func newLLMClientForFeatures() (llm.Client, string, error) {
	provider := strings.TrimSpace(llmutil.ProviderFromViper())
	endpoint := strings.TrimSpace(llmutil.EndpointFromViper())
	apiKey := strings.TrimSpace(llmutil.APIKeyFromViper())
	model := strings.TrimSpace(llmutil.ModelFromViper())
	if model == "" {
		return nil, "", fmt.Errorf("empty llm model")
	}
	timeout := viper.GetDuration("llm.request_timeout")
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client, err := llmutil.ClientFromConfig(llmconfig.ClientConfig{
		Provider:       provider,
		Endpoint:       endpoint,
		APIKey:         apiKey,
		Model:          model,
		RequestTimeout: timeout,
	})
	if err != nil {
		return nil, "", err
	}
	return client, model, nil
}

func serviceFromCmd(cmd *cobra.Command) *contacts.Service {
	dir, _ := cmd.Flags().GetString("dir")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = statepaths.ContactsDir()
	} else {
		dir = pathutil.ExpandHomePath(dir)
	}
	return contacts.NewService(contacts.NewFileStore(dir))
}

func maepServiceFromDir(dir string) *maep.Service {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = statepaths.MAEPDir()
	} else {
		dir = pathutil.ExpandHomePath(dir)
	}
	return maep.NewService(maep.NewFileStore(dir))
}

func parseStatus(raw string) contacts.Status {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "active":
		return contacts.StatusActive
	case "inactive":
		return contacts.StatusInactive
	default:
		return ""
	}
}

func chooseContactID(nodeID string, peerID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID != "" {
		return nodeID
	}
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return ""
	}
	return "maep:" + peerID
}

func resolveSyncMAEPTargetContactID(existingByNodeID map[string]string, nodeID string, peerID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID != "" && existingByNodeID != nil {
		if existing, ok := existingByNodeID[nodeID]; ok {
			existing = strings.TrimSpace(existing)
			if existing != "" {
				return existing
			}
		}
	}
	return chooseContactID(nodeID, peerID)
}

func telegramContactID(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	username = strings.TrimPrefix(username, "@")
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return "tg:@" + username
}

func normalizeList(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		item := strings.TrimSpace(raw)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func parseFloatAssignments(values []string) (map[string]float64, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := map[string]float64{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		idx := strings.Index(value, "=")
		if idx <= 0 || idx == len(value)-1 {
			return nil, fmt.Errorf("invalid assignment %q (want key=value)", raw)
		}
		key := strings.TrimSpace(value[:idx])
		number := strings.TrimSpace(value[idx+1:])
		f, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number in %q: %w", raw, err)
		}
		out[key] = f
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
