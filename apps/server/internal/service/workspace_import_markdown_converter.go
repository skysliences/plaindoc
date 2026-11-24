package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// WorkspaceImportMarkdownConverter preserves the Markdown formatting rules for imported files.
type WorkspaceImportMarkdownConverter struct {
	scripts converterScriptPaths
}

func NewWorkspaceImportMarkdownConverter(webDistDir, ssrWorkerEntry string) WorkspaceImportMarkdownConverter {
	return WorkspaceImportMarkdownConverter{scripts: converterScriptPaths{webDistDir, ssrWorkerEntry}}
}

type workspaceHTMLToMarkdownRequest struct {
	HTML string `json:"html"`
}

type workspaceHTMLToMarkdownResult struct {
	Markdown string `json:"markdown"`
}

func (c WorkspaceImportMarkdownConverter) Convert(ctx context.Context, rawHTML string) (string, error) {
	normalizedHTML := strings.TrimSpace(rawHTML)
	if normalizedHTML == "" {
		return "", nil
	}

	scriptPath, err := c.scripts.resolve("convert-html-to-markdown.cjs", "convert_html_to_markdown.mjs")
	if err != nil {
		return "", err
	}
	if _, err := exec.LookPath("node"); err != nil {
		return "", errors.New("node runtime is required for html to markdown conversion")
	}

	inputPayload, err := json.Marshal(workspaceHTMLToMarkdownRequest{
		HTML: normalizedHTML,
	})
	if err != nil {
		return "", err
	}

	command := exec.CommandContext(ctx, "node", scriptPath)
	command.Stdin = bytes.NewReader(inputPayload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("html to markdown failed: %s", message)
	}

	var output workspaceHTMLToMarkdownResult
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return "", fmt.Errorf("decode html to markdown output failed: %w", err)
	}
	return strings.TrimSpace(output.Markdown), nil
}
