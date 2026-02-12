package task

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryServiceDerivesExtractedOutputsOnComplete(t *testing.T) {
	svc := NewInMemoryService()
	created, err := svc.Create(context.Background(), CreateInput{
		SessionID: "sess_1",
		URL:       "https://example.com",
		Goal:      "extract price",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := svc.Start(context.Background(), StartInput{
		TaskID:  created.ID,
		NodeID:  "node-1",
		Started: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("start task: %v", err)
	}

	completed, err := svc.Complete(context.Background(), CompleteInput{
		TaskID:    created.ID,
		NodeID:    "node-1",
		Completed: time.Now().UTC(),
		Trace: []StepTrace{
			{Index: 1, Status: "succeeded", OutputText: "Think and Grow Rich"},
			{Index: 2, Status: "succeeded", OutputText: "$10.99"},
			{Index: 3, Status: "succeeded", OutputText: "$10.99"},
		},
	})
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}

	if len(completed.ExtractedOutputs) != 2 {
		t.Fatalf("expected 2 extracted outputs, got %d", len(completed.ExtractedOutputs))
	}
	if completed.ExtractedOutputs[0] != "Think and Grow Rich" {
		t.Fatalf("unexpected extracted output 0: %q", completed.ExtractedOutputs[0])
	}
	if completed.ExtractedOutputs[1] != "$10.99" {
		t.Fatalf("unexpected extracted output 1: %q", completed.ExtractedOutputs[1])
	}
}

func TestInMemoryServiceClearsDerivedOutputsOnRetry(t *testing.T) {
	svc := NewInMemoryService()
	created, err := svc.Create(context.Background(), CreateInput{
		SessionID: "sess_1",
		URL:       "https://example.com",
		Goal:      "extract price",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := svc.Start(context.Background(), StartInput{
		TaskID:  created.ID,
		NodeID:  "node-1",
		Started: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("start task: %v", err)
	}

	if _, err := svc.Fail(context.Background(), FailInput{
		TaskID:    created.ID,
		NodeID:    "node-1",
		Completed: time.Now().UTC(),
		Error:     "failed",
		Trace: []StepTrace{
			{Index: 1, Status: "failed", OutputText: "$11.99"},
		},
	}); err != nil {
		t.Fatalf("fail task: %v", err)
	}

	retried, err := svc.Retry(context.Background(), RetryInput{
		TaskID:    created.ID,
		RetryAt:   time.Now().UTC().Add(time.Second),
		LastError: "retry",
	})
	if err != nil {
		t.Fatalf("retry task: %v", err)
	}
	if len(retried.ExtractedOutputs) != 0 {
		t.Fatalf("expected retry to clear extracted outputs, got %d", len(retried.ExtractedOutputs))
	}
}
