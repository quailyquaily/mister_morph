package daemonruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApprovalsRouteListUsesInjectedHandler(t *testing.T) {
	createdAt := time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC)
	var gotReq ApprovalListRequest
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken: "token",
		Approvals: ApprovalRoutes{
			List: func(_ context.Context, req ApprovalListRequest) (ApprovalListResponse, error) {
				gotReq = req
				return ApprovalListResponse{
					Items: []ApprovalInfo{
						{
							ApprovalRequestID: "apr_1",
							TaskID:            "task_1",
							Status:            "pending",
							ToolName:          "bash",
							ToolParams:        map[string]any{"cmd": "echo approval details"},
							CreatedAt:         createdAt,
						},
					},
					Limit: 7,
				}, nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/approvals?status=pending&limit=7", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotReq.Status != "pending" || gotReq.Limit != 7 {
		t.Fatalf("ApprovalList request = %+v, want status pending limit 7", gotReq)
	}
	var payload ApprovalListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ApprovalRequestID != "apr_1" {
		t.Fatalf("payload.Items = %+v, want apr_1", payload.Items)
	}
	if payload.Items[0].CreatedAt.IsZero() {
		t.Fatalf("payload item created_at was not encoded")
	}
	if payload.Items[0].ToolParams["cmd"] != "echo approval details" {
		t.Fatalf("payload item tool_params = %#v", payload.Items[0].ToolParams)
	}
}

func TestApprovalRouteGetUsesInjectedHandler(t *testing.T) {
	createdAt := time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC)
	var gotID string
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken: "token",
		Approvals: ApprovalRoutes{
			Get: func(_ context.Context, approvalID string) (ApprovalInfo, bool, error) {
				gotID = approvalID
				return ApprovalInfo{
					ApprovalRequestID: approvalID,
					TaskID:            "task_1",
					Status:            "denied",
					ToolName:          "bash",
					ToolParams:        map[string]any{"cmd": "echo approval details"},
					CreatedAt:         createdAt,
				}, true, nil
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/approvals/apr_1", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gotID != "apr_1" {
		t.Fatalf("approval id = %q, want apr_1", gotID)
	}
	var payload ApprovalInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Status != "denied" || payload.ToolParams["cmd"] != "echo approval details" {
		t.Fatalf("payload = %+v, want denied bash details", payload)
	}
}

func TestApprovalDecisionRoutesUseInjectedHandlers(t *testing.T) {
	var approveReq ApprovalDecisionRequest
	var denyReq ApprovalDecisionRequest
	mux := http.NewServeMux()
	RegisterRoutes(mux, RoutesOptions{
		AuthToken: "token", Approvals: ApprovalRoutes{Approve: func(_ context.Context, req ApprovalDecisionRequest) (ApprovalDecisionResponse, error) {
			approveReq = req
			return ApprovalDecisionResponse{
				ApprovalRequestID: req.ApprovalRequestID,
				TaskID:            "task_1",
				Status:            "approved",
				Resumed:           true,
			}, nil
		}, Deny: func(_ context.Context, req ApprovalDecisionRequest) (ApprovalDecisionResponse, error) {
			denyReq = req
			return ApprovalDecisionResponse{
				ApprovalRequestID: req.ApprovalRequestID,
				TaskID:            "task_1",
				Status:            "denied",
				Resumed:           false,
			}, nil
		}},
	})

	req := httptest.NewRequest(http.MethodPost, "/approvals/apr_1/approve", strings.NewReader(`{"actor":"console:user","note":"ok"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if approveReq.ApprovalRequestID != "apr_1" || approveReq.Actor != "console:user" || approveReq.Note != "ok" {
		t.Fatalf("approve request = %+v, want apr_1 console:user ok", approveReq)
	}
	var approvePayload ApprovalDecisionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &approvePayload); err != nil {
		t.Fatalf("json.Unmarshal(approve) error = %v", err)
	}
	if approvePayload.Status != "approved" || !approvePayload.Resumed {
		t.Fatalf("approve payload = %+v, want approved resumed", approvePayload)
	}

	req = httptest.NewRequest(http.MethodPost, "/approvals/apr_1/deny", strings.NewReader(`{"actor":"console:user","note":"no"}`))
	req.Header.Set("Authorization", "Bearer token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("deny status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if denyReq.ApprovalRequestID != "apr_1" || denyReq.Actor != "console:user" || denyReq.Note != "no" {
		t.Fatalf("deny request = %+v, want apr_1 console:user no", denyReq)
	}
	var denyPayload ApprovalDecisionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &denyPayload); err != nil {
		t.Fatalf("json.Unmarshal(deny) error = %v", err)
	}
	if denyPayload.Status != "denied" || denyPayload.Resumed {
		t.Fatalf("deny payload = %+v, want denied not resumed", denyPayload)
	}
}
