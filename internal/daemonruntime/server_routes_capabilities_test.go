package daemonruntime

import (
	"context"
	"reflect"
	"testing"
)

// Keep the public route wiring grouped by endpoint domain. This composite
// literal is a compile-time contract for callers.
var _ = RoutesOptions{
	TaskTopic: TaskTopicRoutes{
		Submit: func(context.Context, SubmitTaskRequest) (SubmitTaskResponse, error) {
			return SubmitTaskResponse{}, nil
		},
	},
	Approvals: ApprovalRoutes{
		List: func(context.Context, ApprovalListRequest) (ApprovalListResponse, error) {
			return ApprovalListResponse{}, nil
		},
		Get: func(context.Context, string) (ApprovalInfo, bool, error) {
			return ApprovalInfo{}, false, nil
		},
	},
	Workspace: WorkspaceRoutes{
		Get: func(context.Context, string) (WorkspaceResolution, error) {
			return WorkspaceResolution{Source: "none"}, nil
		},
	},
}

func TestRoutesOptionsKeepsGroupedRouteCapabilities(t *testing.T) {
	t.Parallel()

	routesType := reflect.TypeOf(RoutesOptions{})
	for _, fieldName := range []string{
		"TaskReader",
		"TopicReader",
		"TopicDeleter",
		"Submit",
		"Stop",
		"ApprovalList",
		"ApprovalApprove",
		"ApprovalDeny",
		"WorkspaceGet",
		"WorkspacePut",
		"WorkspaceDelete",
		"WorkspaceOpen",
		"WorkspaceTree",
		"WorkspaceBrowse",
		"WorkspaceCreateDir",
		"TopicMetadata",
	} {
		if _, ok := routesType.FieldByName(fieldName); ok {
			t.Errorf("RoutesOptions must not expose grouped callback field %q", fieldName)
		}
	}
}
