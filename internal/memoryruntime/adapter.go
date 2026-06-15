package memoryruntime

import (
	"fmt"

	"github.com/quailyquaily/mistermorph/memory"
)

type InjectionAdapter interface {
	ResolveSubjectID() (string, error)
	ResolveRequestContext() (memory.RequestContext, error)
}

type RecordAdapter interface {
	BuildRecordRequest() (RecordRequest, error)
}

func (o *Orchestrator) PrepareInjectionWithAdapter(adapter InjectionAdapter, maxItems int) (string, error) {
	if adapter == nil {
		return "", fmt.Errorf("injection adapter is required")
	}
	subjectID, err := adapter.ResolveSubjectID()
	if err != nil {
		return "", err
	}
	reqCtx, err := adapter.ResolveRequestContext()
	if err != nil {
		return "", err
	}
	return o.PrepareInjection(PrepareInjectionRequest{
		SubjectID:      subjectID,
		RequestContext: reqCtx,
		MaxItems:       maxItems,
	})
}

func (o *Orchestrator) RecordWithAdapter(adapter RecordAdapter) error {
	if adapter == nil {
		return fmt.Errorf("record adapter is required")
	}
	req, err := adapter.BuildRecordRequest()
	if err != nil {
		return err
	}
	return o.Record(req)
}
