package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	nodev1 "github.com/VenkatGGG/Browser-use/internal/gen"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcNodeAgentServer struct {
	nodev1.UnimplementedNodeAgentServer
	executor *browserExecutor
}

func newGRPCNodeAgentServer(executor *browserExecutor) *grpcNodeAgentServer {
	return &grpcNodeAgentServer{executor: executor}
}

func (s *grpcNodeAgentServer) Register(context.Context, *nodev1.RegisterRequest) (*nodev1.RegisterResponse, error) {
	return &nodev1.RegisterResponse{Accepted: true}, nil
}

func (s *grpcNodeAgentServer) Heartbeat(context.Context, *nodev1.HeartbeatRequest) (*nodev1.HeartbeatResponse, error) {
	return &nodev1.HeartbeatResponse{Ok: true}, nil
}

func (s *grpcNodeAgentServer) OpenURL(ctx context.Context, req *nodev1.OpenURLRequest) (*nodev1.OpenURLResponse, error) {
	if err := s.executor.OpenURL(ctx, req.GetUrl()); err != nil {
		return &nodev1.OpenURLResponse{
			Ok:           false,
			ErrorMessage: err.Error(),
		}, nil
	}
	return &nodev1.OpenURLResponse{Ok: true}, nil
}

func (s *grpcNodeAgentServer) Act(ctx context.Context, req *nodev1.ActRequest) (*nodev1.ActResponse, error) {
	actionType := strings.ToLower(strings.TrimSpace(req.GetAction()))
	if actionType == "" {
		return &nodev1.ActResponse{Ok: false, ErrorMessage: "action is required"}, nil
	}

	if actionType == "execute_flow" {
		actions, err := parseActionsJSON(req.GetParams()["actions_json"])
		if err != nil {
			return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
		}

		result, err := s.executor.ExecuteWithActions(
			ctx,
			req.GetParams()["url"],
			req.GetParams()["goal"],
			actions,
			"",
		)
		if err != nil {
			var flowErr *executeFlowError
			if errors.As(err, &flowErr) {
				raw, marshalErr := json.Marshal(flowErr.result)
				if marshalErr != nil {
					return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
				}
				return &nodev1.ActResponse{
					Ok:           false,
					ErrorMessage: err.Error(),
					MetadataJson: string(raw),
				}, nil
			}
			return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
		}
		return &nodev1.ActResponse{
			Ok:           true,
			MetadataJson: string(raw),
		}, nil
	}

	action, err := toExecuteAction(actionType, req.GetParams())
	if err != nil {
		return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
	}
	if err := s.executor.RunAction(ctx, action); err != nil {
		return &nodev1.ActResponse{Ok: false, ErrorMessage: err.Error()}, nil
	}
	return &nodev1.ActResponse{Ok: true}, nil
}

func (s *grpcNodeAgentServer) Snapshot(ctx context.Context, _ *nodev1.SnapshotRequest) (*nodev1.SnapshotResponse, error) {
	png, err := s.executor.SnapshotPNG(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "snapshot failed: %v", err)
	}
	return &nodev1.SnapshotResponse{
		ContentType: "image/png",
		ImageBytes:  png,
	}, nil
}

func (s *grpcNodeAgentServer) Shutdown(context.Context, *nodev1.ShutdownRequest) (*nodev1.ShutdownResponse, error) {
	return &nodev1.ShutdownResponse{Ok: true}, nil
}

func parseActionsJSON(value string) ([]executeAction, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil
	}
	var actions []executeAction
	if err := json.Unmarshal([]byte(raw), &actions); err != nil {
		return nil, err
	}
	return actions, nil
}

func toExecuteAction(actionType string, params map[string]string) (executeAction, error) {
	normalized := strings.ToLower(strings.TrimSpace(actionType))
	if normalized == "" {
		return executeAction{}, status.Error(codes.InvalidArgument, "action is required")
	}

	action := executeAction{
		Type:     normalized,
		Selector: strings.TrimSpace(params["selector"]),
		Text:     params["text"],
	}

	if timeoutRaw := strings.TrimSpace(params["timeout_ms"]); timeoutRaw != "" {
		timeout, err := strconv.Atoi(timeoutRaw)
		if err != nil {
			return executeAction{}, status.Errorf(codes.InvalidArgument, "invalid timeout_ms: %s", timeoutRaw)
		}
		action.TimeoutMS = timeout
	}
	if delayRaw := strings.TrimSpace(params["delay_ms"]); delayRaw != "" {
		delay, err := strconv.Atoi(delayRaw)
		if err != nil {
			return executeAction{}, status.Errorf(codes.InvalidArgument, "invalid delay_ms: %s", delayRaw)
		}
		action.DelayMS = delay
	}
	if pixelsRaw := strings.TrimSpace(params["pixels"]); pixelsRaw != "" {
		pixels, err := strconv.Atoi(pixelsRaw)
		if err != nil {
			return executeAction{}, status.Errorf(codes.InvalidArgument, "invalid pixels: %s", pixelsRaw)
		}
		action.Pixels = pixels
	}

	return action, nil
}
