package guard

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Guard struct {
	cfg        Config
	redactor   *Redactor
	audit      AuditSink
	approvals  ApprovalStore
	lookupHost func(string) ([]string, error) // nil => net.LookupHost
}

func New(cfg Config, audit AuditSink, approvals ApprovalStore) *Guard {
	if cfg.Redaction.Enabled || len(cfg.Redaction.Patterns) > 0 {
		// ok
	}
	return &Guard{
		cfg:       cfg,
		redactor:  NewRedactor(cfg.Redaction),
		audit:     audit,
		approvals: approvals,
	}
}

// SetLookupHost overrides the default DNS resolver (for testing).
func (g *Guard) SetLookupHost(fn func(string) ([]string, error)) {
	if g != nil {
		g.lookupHost = fn
	}
}

func (g *Guard) Enabled() bool { return g != nil && g.cfg.Enabled }

func (g *Guard) NetworkPolicyForURLFetch() (NetworkPolicy, bool) {
	if g == nil || !g.cfg.Enabled {
		return NetworkPolicy{}, false
	}
	p := g.cfg.Network.URLFetch
	return NetworkPolicy{
		AllowedURLPrefixes: append([]string{}, p.AllowedURLPrefixes...),
		DenyPrivateIPs:     p.DenyPrivateIPs,
		ResolveDNS:         p.ResolveDNS,
		FollowRedirects:    p.FollowRedirects,
		AllowProxy:         p.AllowProxy,
		LookupHost:         g.lookupHost,
	}, true
}

func (g *Guard) Evaluate(ctx context.Context, meta Meta, a Action) (Result, error) {
	if g == nil || !g.cfg.Enabled {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}, nil
	}
	if meta.Time.IsZero() {
		meta.Time = time.Now().UTC()
	}

	res := Result{RiskLevel: RiskLow, Decision: DecisionAllow}

	switch a.Type {
	case ActionToolCallPre:
		res = g.evalToolCallPre(ctx, a)
	case ActionToolCallPost:
		res = g.evalToolCallPost(a)
	case ActionOutputPublish:
		res = g.evalOutputPublish(a)
	default:
		res = Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}

	g.emitAudit(ctx, meta, a, res, "", "", "")
	return res, nil
}

func (g *Guard) RequestApproval(ctx context.Context, meta Meta, a Action, pre Result, actionSummaryRedacted string, resumeState []byte) (string, error) {
	if g == nil || !g.cfg.Enabled {
		return "", fmt.Errorf("guard is disabled")
	}
	if g.approvals == nil || !g.cfg.Approvals.Enabled {
		return "", fmt.Errorf("approvals are not enabled")
	}
	if meta.Time.IsZero() {
		meta.Time = time.Now().UTC()
	}

	h, err := ActionHash(a)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	// M1: hard-coded TTL. If you need configurable expiry/SLAs, add it later with a clear threat model.
	expiresAt := now.Add(5 * time.Minute)

	rec := ApprovalRecord{
		RunID:                 meta.RunID,
		CreatedAt:             now,
		ExpiresAt:             expiresAt,
		Status:                ApprovalPending,
		ActionType:            a.Type,
		ToolName:              strings.TrimSpace(a.ToolName),
		ActionHash:            h,
		RiskLevel:             pre.RiskLevel,
		Decision:              pre.Decision,
		Reasons:               append([]string{}, pre.Reasons...),
		ActionSummaryRedacted: strings.TrimSpace(actionSummaryRedacted),
		ResumeState:           resumeState,
	}
	id, err := g.approvals.Create(ctx, rec)
	if err != nil {
		return "", err
	}

	g.emitAudit(ctx, meta, a, pre, id, string(ApprovalPending), "")
	return id, nil
}

func (g *Guard) GetApproval(ctx context.Context, id string) (ApprovalRecord, bool, error) {
	if g == nil || g.approvals == nil {
		return ApprovalRecord{}, false, nil
	}
	return g.approvals.Get(ctx, id)
}

func (g *Guard) ResolveApproval(ctx context.Context, id string, status ApprovalStatus, actor, comment string) error {
	if g == nil || g.approvals == nil {
		return fmt.Errorf("approvals not configured")
	}
	if err := g.approvals.Resolve(ctx, id, status, actor, comment); err != nil {
		return err
	}
	// Emit a follow-up audit event for the resolution (safe/redacted by construction).
	if rec, ok, err := g.approvals.Get(ctx, id); err == nil && ok {
		g.emitApprovalResolutionAudit(ctx, rec)
	}
	return nil
}

func (g *Guard) Close() error {
	if g == nil {
		return nil
	}
	if g.audit != nil {
		return g.audit.Close()
	}
	return nil
}

func (g *Guard) evalToolCallPre(_ context.Context, a Action) Result {
	name := strings.TrimSpace(strings.ToLower(a.ToolName))
	switch name {
	case "bash":
		if g.cfg.Bash.RequireApproval {
			return Result{
				RiskLevel: RiskHigh,
				Decision:  DecisionRequireApproval,
				Reasons:   []string{"bash_requires_approval"},
			}
		}
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	case "url_fetch":
		rawURL := ""
		if a.ToolParams != nil {
			if v, ok := a.ToolParams["url"].(string); ok {
				rawURL = strings.TrimSpace(v)
			}
		}
		if rawURL == "" {
			return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
		}
		// If the call uses auth_profile, the auth_profile's allow policy is the primary destination boundary.
		// Guard still audits, but does not add an extra allowlist layer by default.
		if a.ToolParams != nil {
			if v, ok := a.ToolParams["auth_profile"].(string); ok && strings.TrimSpace(v) != "" {
				return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
			}
		}

		p := g.cfg.Network.URLFetch
		if len(p.AllowedURLPrefixes) == 0 {
			return Result{
				RiskLevel: RiskHigh,
				Decision:  DecisionDeny,
				Reasons:   []string{"url_fetch_not_allowlisted"},
			}
		}

		u, err := url.Parse(rawURL)
		if err != nil {
			return Result{RiskLevel: RiskHigh, Decision: DecisionDeny, Reasons: []string{"invalid_url"}}
		}
		if p.DenyPrivateIPs {
			if err := ResolveAndCheckHost(u.Hostname(), p.ResolveDNS, g.lookupHost); err != nil {
				return Result{RiskLevel: RiskHigh, Decision: DecisionDeny, Reasons: []string{"private_ip"}}
			}
		}

		if !URLAllowedByPrefixes(rawURL, p.AllowedURLPrefixes) {
			return Result{RiskLevel: RiskHigh, Decision: DecisionDeny, Reasons: []string{"non_allowlisted_domain"}}
		}
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	default:
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
}

func (g *Guard) evalToolCallPost(a Action) Result {
	obs := a.Content
	if strings.TrimSpace(obs) == "" {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
	if g.redactor == nil {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
	red, changed := g.redactor.RedactString(obs)
	if !changed {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
	return Result{
		RiskLevel:       RiskHigh,
		Decision:        DecisionAllowWithRedact,
		Reasons:         []string{"sensitive_content_redacted"},
		RedactedContent: red,
	}
}

func (g *Guard) evalOutputPublish(a Action) Result {
	out := a.Content
	if strings.TrimSpace(out) == "" || g.redactor == nil {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
	red, changed := g.redactor.RedactString(out)
	if !changed {
		return Result{RiskLevel: RiskLow, Decision: DecisionAllow}
	}
	return Result{
		RiskLevel:       RiskHigh,
		Decision:        DecisionAllowWithRedact,
		Reasons:         []string{"sensitive_content_redacted"},
		RedactedContent: red,
	}
}

func (g *Guard) emitAudit(ctx context.Context, meta Meta, a Action, res Result, approvalID string, approvalStatus string, actor string) {
	if g == nil || g.audit == nil || !g.cfg.Enabled {
		return
	}
	if meta.Time.IsZero() {
		meta.Time = time.Now().UTC()
	}

	sum := summarizeActionRedacted(a)
	hash, _ := ActionHash(a)
	ev := AuditEvent{
		EventID:               newEventID(meta),
		RunID:                 meta.RunID,
		Timestamp:             meta.Time.UTC(),
		Step:                  meta.Step,
		ActionType:            a.Type,
		ToolName:              strings.TrimSpace(a.ToolName),
		ActionSummaryRedacted: sum,
		ActionHash:            hash,
		RiskLevel:             res.RiskLevel,
		Decision:              res.Decision,
		Reasons:               append([]string{}, res.Reasons...),
		ApprovalRequestID:     approvalID,
		ApprovalStatus:        approvalStatus,
		Actor:                 actor,
	}
	_ = g.audit.Emit(ctx, ev)
}

func (g *Guard) emitApprovalResolutionAudit(ctx context.Context, rec ApprovalRecord) {
	if g == nil || g.audit == nil || !g.cfg.Enabled {
		return
	}
	meta := Meta{RunID: strings.TrimSpace(rec.RunID), Step: -1, Time: time.Now().UTC()}
	ev := AuditEvent{
		EventID:               newEventID(meta),
		RunID:                 meta.RunID,
		Timestamp:             meta.Time.UTC(),
		Step:                  meta.Step,
		ActionType:            rec.ActionType,
		ToolName:              strings.TrimSpace(rec.ToolName),
		ActionSummaryRedacted: strings.TrimSpace(rec.ActionSummaryRedacted),
		ActionHash:            strings.TrimSpace(rec.ActionHash),
		RiskLevel:             rec.RiskLevel,
		Decision:              rec.Decision,
		Reasons:               append([]string{}, rec.Reasons...),
		ApprovalRequestID:     strings.TrimSpace(rec.ID),
		ApprovalStatus:        string(rec.Status),
		Actor:                 strings.TrimSpace(rec.Actor),
	}
	_ = g.audit.Emit(ctx, ev)
}

func summarizeActionRedacted(a Action) string {
	switch a.Type {
	case ActionToolCallPre, ActionToolCallPost:
		if strings.TrimSpace(a.ToolName) == "" {
			return string(a.Type)
		}
		if strings.EqualFold(a.ToolName, "url_fetch") {
			raw := ""
			if a.ToolParams != nil {
				if v, ok := a.ToolParams["url"].(string); ok {
					raw = strings.TrimSpace(v)
				}
			}
			if raw == "" {
				raw = strings.TrimSpace(a.URL)
			}
			return string(a.Type) + " tool=url_fetch url=" + redactURLQuery(raw)
		}
		return string(a.Type) + " tool=" + strings.TrimSpace(a.ToolName)
	case ActionOutputPublish:
		return "OutputPublish content=[redacted_summary]"
	default:
		return string(a.Type)
	}
}

func redactURLQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if isSensitiveKeyLike(k) {
			q.Set(k, "[redacted]")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

