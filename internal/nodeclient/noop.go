package nodeclient

import "context"

type NoopClient struct{}

func (NoopClient) Execute(_ context.Context, _ string, input ExecuteInput) (ExecuteOutput, error) {
	return ExecuteOutput{
		PageTitle:        "noop",
		FinalURL:         input.URL,
		ScreenshotBase64: "",
	}, nil
}
