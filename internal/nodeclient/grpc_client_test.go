package nodeclient

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"testing"
	"time"

	nodev1 "github.com/VenkatGGG/Browser-use/internal/gen"
	"google.golang.org/grpc"
)

type fakeNodeAgentServer struct {
	nodev1.UnimplementedNodeAgentServer
	actMetadata string
	actOK       bool
	actError    string
	snapshotPNG []byte
}

func (f *fakeNodeAgentServer) Act(_ context.Context, req *nodev1.ActRequest) (*nodev1.ActResponse, error) {
	if req.GetAction() != "execute_flow" {
		return &nodev1.ActResponse{Ok: false, ErrorMessage: "unsupported action"}, nil
	}
	return &nodev1.ActResponse{
		Ok:           f.actOK || (f.actError == "" && !f.actOK && f.actMetadata != ""),
		ErrorMessage: f.actError,
		MetadataJson: f.actMetadata,
	}, nil
}

func (f *fakeNodeAgentServer) Snapshot(_ context.Context, _ *nodev1.SnapshotRequest) (*nodev1.SnapshotResponse, error) {
	return &nodev1.SnapshotResponse{
		ContentType: "image/png",
		ImageBytes:  f.snapshotPNG,
	}, nil
}

func startFakeGRPCNode(t *testing.T, server nodev1.NodeAgentServer) (addr string, stop func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	nodev1.RegisterNodeAgentServer(grpcServer, server)
	go func() {
		_ = grpcServer.Serve(listener)
	}()

	return listener.Addr().String(), func() {
		grpcServer.GracefulStop()
		_ = listener.Close()
	}
}

func TestGRPCClientExecuteParsesMetadata(t *testing.T) {
	nodeAddr, stop := startFakeGRPCNode(t, &fakeNodeAgentServer{
		actOK:       true,
		actMetadata: `{"page_title":"Example","final_url":"https://example.com","screenshot_base64":"abc123"}`,
	})
	defer stop()

	client := NewGRPCClient(5 * time.Second)
	out, err := client.Execute(context.Background(), nodeAddr, ExecuteInput{
		TaskID: "task_1",
		URL:    "https://example.com",
		Goal:   "open page",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.PageTitle != "Example" {
		t.Fatalf("expected title Example, got %q", out.PageTitle)
	}
	if out.FinalURL != "https://example.com" {
		t.Fatalf("expected final url, got %q", out.FinalURL)
	}
	if out.ScreenshotBase64 != "abc123" {
		t.Fatalf("expected screenshot from metadata, got %q", out.ScreenshotBase64)
	}
}

func TestGRPCClientExecuteFallsBackToSnapshot(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47}
	nodeAddr, stop := startFakeGRPCNode(t, &fakeNodeAgentServer{
		actOK:       true,
		actMetadata: `{"page_title":"Example","final_url":"https://example.com"}`,
		snapshotPNG: pngBytes,
	})
	defer stop()

	client := NewGRPCClient(5 * time.Second)
	out, err := client.Execute(context.Background(), nodeAddr, ExecuteInput{
		TaskID: "task_1",
		URL:    "https://example.com",
		Goal:   "open page",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out.ScreenshotBase64 == "" {
		t.Fatalf("expected screenshot fallback from snapshot rpc")
	}
	want := base64.StdEncoding.EncodeToString(pngBytes)
	if out.ScreenshotBase64 != want {
		t.Fatalf("expected screenshot %q, got %q", want, out.ScreenshotBase64)
	}
}

func TestGRPCClientExecuteReturnsExecutionErrorWithMetadata(t *testing.T) {
	nodeAddr, stop := startFakeGRPCNode(t, &fakeNodeAgentServer{
		actOK:    false,
		actError: "action 2 (click) failed: timeout waiting for selector \"button.buy\"",
		actMetadata: `{
			"page_title":"Buy Page",
			"final_url":"https://example.com/buy",
			"trace":[
				{"index":1,"action":{"type":"wait_for","selector":"input[name=\"q\"]"},"status":"succeeded","duration_ms":120},
				{"index":2,"action":{"type":"click","selector":"button.buy"},"status":"failed","error":"timeout waiting for selector \"button.buy\"","duration_ms":800}
			]
		}`,
	})
	defer stop()

	client := NewGRPCClient(5 * time.Second)
	out, err := client.Execute(context.Background(), nodeAddr, ExecuteInput{
		TaskID: "task_1",
		URL:    "https://example.com/buy",
		Goal:   "buy",
	})
	if err == nil {
		t.Fatalf("expected execution error")
	}
	execErr := &ExecutionError{}
	if ok := errors.As(err, &execErr); !ok {
		t.Fatalf("expected ExecutionError, got %T (%v)", err, err)
	}
	if out.PageTitle != "Buy Page" {
		t.Fatalf("expected title from metadata, got %q", out.PageTitle)
	}
	if len(out.Trace) != 2 {
		t.Fatalf("expected 2 trace steps from metadata, got %d", len(out.Trace))
	}
	if out.Trace[1].Status != "failed" {
		t.Fatalf("expected second trace step failed, got %q", out.Trace[1].Status)
	}
}
