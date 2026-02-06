package maepcmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/quailyquaily/mistermorph/internal/pathutil"
	"github.com/quailyquaily/mistermorph/internal/statepaths"
	"github.com/quailyquaily/mistermorph/maep"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "maep",
		Short: "Manage MAEP identity, contacts, and P2P exchange",
	}
	cmd.PersistentFlags().String("dir", "", "MAEP state directory (defaults to file_state_dir/maep)")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newIDCmd())
	cmd.AddCommand(newCardCmd())
	cmd.AddCommand(newContactsCmd())
	cmd.AddCommand(newInboxCmd())
	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newHelloCmd())
	cmd.AddCommand(newPingCmd())
	cmd.AddCommand(newCapabilitiesCmd())
	cmd.AddCommand(newPushCmd())
	return cmd
}

func newInitCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local MAEP identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, created, err := svc.EnsureIdentity(cmd.Context(), time.Now().UTC())
			if err != nil {
				return err
			}
			fingerprint, _ := maep.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"created":     created,
				"node_uuid":   identity.NodeUUID,
				"peer_id":     identity.PeerID,
				"node_id":     identity.NodeID,
				"fingerprint": fingerprint,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}

			state := "existing"
			if created {
				state = "created"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "state: %s\nnode_uuid: %s\npeer_id: %s\nnode_id: %s\nfingerprint: %s\n", state, identity.NodeUUID, identity.PeerID, identity.NodeID, fingerprint)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newIDCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "id",
		Short: "Show local MAEP identity",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			identity, ok, err := svc.GetIdentity(cmd.Context())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("identity not found; run `mistermorph maep init`")
			}

			fingerprint, _ := maep.FingerprintGrouped(identity.IdentityPubEd25519)
			view := map[string]any{
				"node_uuid":            identity.NodeUUID,
				"peer_id":              identity.PeerID,
				"node_id":              identity.NodeID,
				"identity_pub_ed25519": identity.IdentityPubEd25519,
				"fingerprint":          fingerprint,
				"created_at":           identity.CreatedAt,
				"updated_at":           identity.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "node_uuid: %s\npeer_id: %s\nnode_id: %s\nidentity_pub_ed25519: %s\nfingerprint: %s\ncreated_at: %s\nupdated_at: %s\n", identity.NodeUUID, identity.PeerID, identity.NodeID, identity.IdentityPubEd25519, fingerprint, identity.CreatedAt.UTC().Format(time.RFC3339), identity.UpdatedAt.UTC().Format(time.RFC3339))
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Manage MAEP contact cards",
	}
	cmd.AddCommand(newCardExportCmd())
	return cmd
}

func newCardExportCmd() *cobra.Command {
	var addresses []string
	var outPath string
	var minProtocol int
	var maxProtocol int
	var expiresIn time.Duration

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a signed contact card",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			if len(addresses) == 0 {
				return fmt.Errorf("at least one --address is required")
			}
			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().UTC().Add(expiresIn)
				expiresAt = &t
			}
			_, raw, err := svc.ExportContactCard(cmd.Context(), addresses, minProtocol, maxProtocol, time.Now().UTC(), expiresAt)
			if err != nil {
				return err
			}
			if strings.TrimSpace(outPath) == "" {
				_, _ = cmd.OutOrStdout().Write(raw)
				return nil
			}
			path := pathutil.ExpandHomePath(strings.TrimSpace(outPath))
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "written: %s\n", path)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Contact multiaddr (repeatable, must end with /p2p/<peer_id>)")
	cmd.Flags().StringVar(&outPath, "out", "", "Output file path (default stdout)")
	cmd.Flags().IntVar(&minProtocol, "min-protocol", 1, "Minimum supported protocol version")
	cmd.Flags().IntVar(&maxProtocol, "max-protocol", 1, "Maximum supported protocol version")
	cmd.Flags().DurationVar(&expiresIn, "expires-in", 0, "Relative expiration duration, e.g. 720h (0 disables)")

	return cmd
}

func newContactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contacts",
		Short: "Manage MAEP contacts",
	}
	cmd.AddCommand(newContactsListCmd())
	cmd.AddCommand(newContactsImportCmd())
	cmd.AddCommand(newContactsShowCmd())
	cmd.AddCommand(newContactsVerifyCmd())
	return cmd
}

func newContactsListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List contacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contacts, err := svc.ListContacts(cmd.Context())
			if err != nil {
				return err
			}
			sort.Slice(contacts, func(i, j int) bool {
				if contacts[i].UpdatedAt.Equal(contacts[j].UpdatedAt) {
					return contacts[i].PeerID < contacts[j].PeerID
				}
				return contacts[i].UpdatedAt.After(contacts[j].UpdatedAt)
			})

			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), contacts)
			}
			if len(contacts) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no contacts")
				return nil
			}
			for _, c := range contacts {
				display := strings.TrimSpace(c.DisplayName)
				if display == "" {
					display = "-"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", c.PeerID, c.TrustState, c.NodeUUID, display)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsImportCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "import <contact_card.json|->",
		Short: "Import a contact card",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := readInputFile(args[0])
			if err != nil {
				return err
			}
			svc := serviceFromCmd(cmd)
			result, err := svc.ImportContactCard(cmd.Context(), raw, displayName, time.Now().UTC())
			if err != nil {
				symbol := maep.SymbolOf(err)
				if symbol != "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "symbol: %s\n", symbol)
				}
				return err
			}
			status := "updated"
			if result.Created {
				status = "created"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: %s\npeer_id: %s\nnode_uuid: %s\ntrust_state: %s\n", status, result.Contact.PeerID, result.Contact.NodeUUID, result.Contact.TrustState)
			return nil
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "Display name override for this contact")
	return cmd
}

func newContactsShowCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "show <peer_id>",
		Short: "Show contact detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, ok, err := svc.GetContactByPeerID(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("contact not found: %s", args[0])
			}
			fingerprint, _ := maep.FingerprintGrouped(contact.IdentityPubEd25519)
			view := map[string]any{
				"peer_id":                contact.PeerID,
				"node_uuid":              contact.NodeUUID,
				"node_id":                contact.NodeID,
				"display_name":           contact.DisplayName,
				"identity_pub_ed25519":   contact.IdentityPubEd25519,
				"fingerprint":            fingerprint,
				"addresses":              contact.Addresses,
				"trust_state":            contact.TrustState,
				"min_supported_protocol": contact.MinSupportedProtocol,
				"max_supported_protocol": contact.MaxSupportedProtocol,
				"issued_at":              contact.IssuedAt,
				"expires_at":             contact.ExpiresAt,
				"updated_at":             contact.UpdatedAt,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\nnode_uuid: %s\nnode_id: %s\ndisplay_name: %s\ntrust_state: %s\nfingerprint: %s\n", contact.PeerID, contact.NodeUUID, contact.NodeID, contact.DisplayName, contact.TrustState, fingerprint)
			for _, addr := range contact.Addresses {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newContactsVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <peer_id>",
		Short: "Mark contact as verified after out-of-band fingerprint check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			contact, err := svc.MarkContactVerified(cmd.Context(), args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntrust_state: %s\n", contact.PeerID, contact.TrustState)
			return nil
		},
	}
	return cmd
}

func newInboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "Query received agent.data.push messages",
	}
	cmd.AddCommand(newInboxListCmd())
	return cmd
}

func newInboxListCmd() *cobra.Command {
	var fromPeerID string
	var topic string
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List received messages from local inbox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc := serviceFromCmd(cmd)
			records, err := svc.ListInboxMessages(cmd.Context(), fromPeerID, topic, limit)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), records)
			}
			if len(records) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no inbox messages")
				return nil
			}
			for _, record := range records {
				payloadSummary := summarizePayload(record.ContentType, record.PayloadBase64)
				_, _ = fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%s\t%s\t%s\n",
					record.ReceivedAt.UTC().Format(time.RFC3339),
					record.FromPeerID,
					record.Topic,
					record.IdempotencyKey,
					payloadSummary,
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromPeerID, "from-peer-id", "", "Filter by sender peer_id")
	cmd.Flags().StringVar(&topic, "topic", "", "Filter by topic")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max number of records (<=0 means all)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newServeCmd() *cobra.Command {
	var listenAddrs []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run MAEP libp2p node and handle incoming RPC streams",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			svc := serviceFromCmd(cmd)
			logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
			node, err := maep.NewNode(runCtx, svc, maep.NodeOptions{
				ListenAddrs: listenAddrs,
				Logger:      logger,
				OnDataPush: func(event maep.DataPushEvent) {
					printDataPushEvent(cmd, event, outputJSON)
				},
			})
			if err != nil {
				return err
			}
			defer node.Close()

			if outputJSON {
				_ = writeJSON(cmd.OutOrStdout(), map[string]any{
					"status":    "ready",
					"peer_id":   node.PeerID(),
					"addresses": node.AddrStrings(),
				})
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "status: ready\npeer_id: %s\n", node.PeerID())
				for _, addr := range node.AddrStrings() {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "address: %s\n", addr)
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "waiting for incoming streams... (Ctrl+C to stop)")
			}

			<-runCtx.Done()
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&listenAddrs, "listen", nil, "Listen multiaddr (repeatable), default: /ip4/0.0.0.0/udp/0/quic-v1 and /ip4/0.0.0.0/tcp/0")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print status/events as JSON")
	return cmd
}

func newHelloCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "hello <peer_id>",
		Short: "Run explicit hello negotiation with a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.DialHello(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\nremote_min: %d\nremote_max: %d\nnegotiated: %d\n", result.RemotePeerID, result.RemoteMinProtocol, result.RemoteMaxProtocol, result.NegotiatedProtocol)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newPingCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "ping <peer_id>",
		Short: "Send agent.ping RPC to a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.Ping(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ok: %v\nts: %v\n", result["ok"], result["ts"])
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newCapabilitiesCmd() *cobra.Command {
	var addresses []string
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "capabilities <peer_id>",
		Short: "Fetch remote capabilities via agent.capabilities.get",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()
			result, err := node.GetCapabilities(cmd.Context(), args[0], addresses)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "protocol_min: %v\nprotocol_max: %v\n", result["protocol_min"], result["protocol_max"])
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "capabilities: %v\n", result["capabilities"])
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "allowed_methods: %v\n", result["allowed_methods"])
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newPushCmd() *cobra.Command {
	var addresses []string
	var topic string
	var text string
	var contentType string
	var idempotencyKey string
	var conversationID string
	var notify bool
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "push <peer_id>",
		Short: "Send agent.data.push to a contact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text = strings.TrimSpace(text)
			if text == "" {
				return fmt.Errorf("--text is required")
			}
			topic = strings.TrimSpace(topic)
			if topic == "" {
				return fmt.Errorf("--topic is required")
			}
			contentType = strings.TrimSpace(contentType)
			if contentType == "" {
				contentType = "application/json"
			}

			messageID := uuid.NewString()
			payloadBytes := []byte(text)
			if contentType == "application/json" {
				payload := map[string]any{
					"message_id": messageID,
					"text":       text,
					"sent_at":    time.Now().UTC().Format(time.RFC3339),
				}
				if strings.TrimSpace(conversationID) != "" {
					payload["conversation_id"] = strings.TrimSpace(conversationID)
				}
				raw, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				payloadBytes = raw
			}

			idempotencyKey = strings.TrimSpace(idempotencyKey)
			if idempotencyKey == "" {
				idempotencyKey = messageID
			}

			req := maep.DataPushRequest{
				Topic:          topic,
				ContentType:    contentType,
				PayloadBase64:  base64.RawURLEncoding.EncodeToString(payloadBytes),
				IdempotencyKey: idempotencyKey,
			}

			node, err := newDialNode(cmd)
			if err != nil {
				return err
			}
			defer node.Close()

			result, err := node.PushData(cmd.Context(), args[0], addresses, req, notify)
			if err != nil {
				return err
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"peer_id":         args[0],
					"topic":           req.Topic,
					"content_type":    req.ContentType,
					"idempotency_key": req.IdempotencyKey,
					"notification":    notify,
					"result":          result,
				})
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "peer_id: %s\ntopic: %s\ncontent_type: %s\nidempotency_key: %s\nnotification: %v\naccepted: %v\ndeduped: %v\n", args[0], req.Topic, req.ContentType, req.IdempotencyKey, notify, result.Accepted, result.Deduped)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&addresses, "address", nil, "Override dial address (repeatable)")
	cmd.Flags().StringVar(&topic, "topic", "chat.message", "Data topic")
	cmd.Flags().StringVar(&text, "text", "", "Text payload")
	cmd.Flags().StringVar(&contentType, "content-type", "application/json", "Content type (application/json|text/plain)")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key (default: generated message id)")
	cmd.Flags().StringVar(&conversationID, "conversation-id", "", "Conversation id for JSON payload")
	cmd.Flags().BoolVar(&notify, "notify", false, "Send as JSON-RPC notification (no response expected)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}

func newDialNode(cmd *cobra.Command) (*maep.Node, error) {
	svc := serviceFromCmd(cmd)
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	return maep.NewNode(cmd.Context(), svc, maep.NodeOptions{DialOnly: true, Logger: logger})
}

func printDataPushEvent(cmd *cobra.Command, event maep.DataPushEvent, outputJSON bool) {
	if outputJSON {
		payloadText := ""
		if strings.HasPrefix(strings.ToLower(event.ContentType), "text/") || strings.EqualFold(event.ContentType, "application/json") {
			payloadText = string(event.PayloadBytes)
		}
		_ = writeJSON(cmd.OutOrStdout(), map[string]any{
			"event":           "agent.data.push",
			"from_peer_id":    event.FromPeerID,
			"topic":           event.Topic,
			"content_type":    event.ContentType,
			"idempotency_key": event.IdempotencyKey,
			"deduped":         event.Deduped,
			"received_at":     event.ReceivedAt,
			"payload_text":    payloadText,
			"payload_base64":  event.PayloadBase64,
		})
		return
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "incoming: topic=%s from=%s deduped=%v idempotency_key=%s\n", event.Topic, event.FromPeerID, event.Deduped, event.IdempotencyKey)
	if strings.HasPrefix(strings.ToLower(event.ContentType), "application/json") {
		var obj any
		if err := json.Unmarshal(event.PayloadBytes, &obj); err == nil {
			pretty, _ := json.MarshalIndent(obj, "", "  ")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(json): %s\n", string(pretty))
			return
		}
	}
	if strings.HasPrefix(strings.ToLower(event.ContentType), "text/") {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(text): %s\n", string(event.PayloadBytes))
		return
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "payload(bytes): %d\n", len(event.PayloadBytes))
}

func summarizePayload(contentType string, payloadBase64 string) string {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(payloadBase64))
	if err != nil {
		return "<invalid-base64>"
	}
	lowerType := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(lowerType, "application/json") {
		var obj any
		if err := json.Unmarshal(data, &obj); err != nil {
			return "<invalid-json>"
		}
		text, err := json.Marshal(obj)
		if err != nil {
			return "<json-encode-error>"
		}
		return truncateString(string(text), 160)
	}
	if strings.HasPrefix(lowerType, "text/") {
		return truncateString(string(data), 160)
	}
	return fmt.Sprintf("<%d bytes>", len(data))
}

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}

func serviceFromCmd(cmd *cobra.Command) *maep.Service {
	dir, _ := cmd.Flags().GetString("dir")
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = statepaths.MAEPDir()
	} else {
		dir = pathutil.ExpandHomePath(dir)
	}
	store := maep.NewFileStore(dir)
	return maep.NewService(store)
}

func readInputFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
