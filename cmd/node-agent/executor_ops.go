package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/VenkatGGG/Browser-use/internal/cdp"
)

func (e *browserExecutor) OpenURL(ctx context.Context, targetURL string) error {
	url := strings.TrimSpace(targetURL)
	if url == "" {
		return errors.New("url is required")
	}

	return e.withLockedClient(ctx, func(runCtx context.Context, client *cdp.Client) error {
		if err := client.Navigate(runCtx, url); err != nil {
			return err
		}
		if e.renderDelay > 0 {
			select {
			case <-runCtx.Done():
				return runCtx.Err()
			case <-time.After(e.renderDelay):
			}
		}
		return nil
	})
}

func (e *browserExecutor) RunAction(ctx context.Context, action executeAction) error {
	return e.withLockedClient(ctx, func(runCtx context.Context, client *cdp.Client) error {
		_, err := e.applyAction(runCtx, client, action)
		return err
	})
}

func (e *browserExecutor) SnapshotBase64(ctx context.Context) (string, error) {
	var screenshot string
	err := e.withLockedClient(ctx, func(runCtx context.Context, client *cdp.Client) error {
		shot, err := client.CaptureScreenshot(runCtx)
		if err != nil {
			return err
		}
		screenshot = shot
		return nil
	})
	if err != nil {
		return "", err
	}
	return screenshot, nil
}

func (e *browserExecutor) SnapshotPNG(ctx context.Context) ([]byte, error) {
	encoded, err := e.SnapshotBase64(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(encoded) == "" {
		return nil, errors.New("empty screenshot data")
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func (e *browserExecutor) withLockedClient(ctx context.Context, fn func(context.Context, *cdp.Client) error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, e.executeTimeout)
	defer cancel()

	client, err := e.dialWithRetry(runCtx)
	if err != nil {
		return err
	}
	defer client.Close()

	return fn(runCtx, client)
}
