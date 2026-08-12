package daemonruntime

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (routes *routeRegistration) registerApprovalRoutes() {
	mux := routes.mux
	opts := routes.options.Approvals
	authToken := routes.authToken
	approvalList := opts.List
	approvalGet := opts.Get
	approvalApprove := opts.Approve
	approvalDeny := opts.Deny

	mux.HandleFunc("/approvals", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if approvalList == nil {
			http.Error(w, "approvals are unavailable", http.StatusServiceUnavailable)
			return
		}
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		if status == "" {
			status = string(TaskPending)
		}
		if !strings.EqualFold(status, string(TaskPending)) {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		limit := taskListDefaultLimit
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil || parsed <= 0 || parsed > taskListMaxLimit {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		resp, err := approvalList(r.Context(), ApprovalListRequest{
			Status: string(TaskPending),
			Limit:  limit,
		})
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		if resp.Limit <= 0 {
			resp.Limit = limit
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/approvals/", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(r, authToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			if approvalGet == nil {
				http.Error(w, "approvals are unavailable", http.StatusServiceUnavailable)
				return
			}
			approvalID, ok := parseApprovalPath(r.URL.Path)
			if !ok {
				http.NotFound(w, r)
				return
			}
			info, found, err := approvalGet(r.Context(), approvalID)
			if err != nil {
				if msg, ok := badRequestMessage(err); ok {
					http.Error(w, msg, http.StatusBadRequest)
					return
				}
				http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
				return
			}
			if !found {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(info)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		approvalID, action, ok := parseApprovalDecisionPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		handler := approvalApprove
		if action == "deny" {
			handler = approvalDeny
		}
		if handler == nil {
			http.Error(w, "approvals are unavailable", http.StatusServiceUnavailable)
			return
		}
		var req ApprovalDecisionRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
		}
		req.ApprovalRequestID = approvalID
		resp, err := handler(r.Context(), req)
		if err != nil {
			if msg, ok := badRequestMessage(err); ok {
				http.Error(w, msg, http.StatusBadRequest)
				return
			}
			http.Error(w, strings.TrimSpace(err.Error()), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

}
