package nodeclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	nodev1 "github.com/VenkatGGG/Browser-use/internal/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	timeout time.Duration
}

func NewGRPCClient(timeout time.Duration) *GRPCClient {
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &GRPCClient{timeout: timeout}
}

func (c *GRPCClient) Execute(ctx context.Context, nodeAddress string, input ExecuteInput) (ExecuteOutput, error) {
	target := normalizeGRPCTarget(nodeAddress)
	if target == "" {
		return ExecuteOutput{}, fmt.Errorf("node address is required")
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, c.timeout)
	defer dialCancel()

	conn, err := grpc.DialContext(
		dialCtx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return ExecuteOutput{}, fmt.Errorf("dial node grpc %s: %w", target, err)
	}
	defer conn.Close()

	client := nodev1.NewNodeAgentClient(conn)

	actionsJSON := "[]"
	if len(input.Actions) > 0 {
		raw, err := json.Marshal(input.Actions)
		if err != nil {
			return ExecuteOutput{}, fmt.Errorf("marshal actions: %w", err)
		}
		actionsJSON = string(raw)
	}

	execCtx, execCancel := context.WithTimeout(ctx, c.timeout)
	defer execCancel()

	actResp, err := client.Act(execCtx, &nodev1.ActRequest{
		SessionId: input.TaskID,
		Action:    "execute_flow",
		Params: map[string]string{
			"url":          input.URL,
			"goal":         input.Goal,
			"actions_json": actionsJSON,
		},
	})
	if err != nil {
		return ExecuteOutput{}, fmt.Errorf("grpc execute_flow failed: %w", err)
	}
	if !actResp.GetOk() {
		msg := strings.TrimSpace(actResp.GetErrorMessage())
		if msg == "" {
			msg = "node execution failed"
		}
		return ExecuteOutput{}, errors.New(msg)
	}

	var out ExecuteOutput
	if payload := strings.TrimSpace(actResp.GetMetadataJson()); payload != "" {
		if err := json.Unmarshal([]byte(payload), &out); err != nil {
			return ExecuteOutput{}, fmt.Errorf("decode execute metadata: %w", err)
		}
	}

	// Backfill screenshot through Snapshot RPC when metadata omitted it.
	if strings.TrimSpace(out.ScreenshotBase64) == "" {
		snapResp, err := client.Snapshot(execCtx, &nodev1.SnapshotRequest{
			SessionId: input.TaskID,
			WithMarks: false,
		})
		if err == nil && len(snapResp.GetImageBytes()) > 0 {
			out.ScreenshotBase64 = base64.StdEncoding.EncodeToString(snapResp.GetImageBytes())
		}
	}

	return out, nil
}

func normalizeGRPCTarget(nodeAddress string) string {
	trimmed := strings.TrimSpace(nodeAddress)
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "grpc://")
	return strings.TrimSpace(trimmed)
}
