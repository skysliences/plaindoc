package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type converterScriptPaths struct {
	webDistDir     string
	ssrWorkerEntry string
}

func (p converterScriptPaths) resolve(bundleName, sourceName string) (string, error) {
	var candidates []string
	if p.ssrWorkerEntry != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(p.ssrWorkerEntry), bundleName))
	}
	if p.webDistDir != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(p.webDistDir), "dist-ssr", bundleName))
	}
	// Keep unbundled development and tests usable in a source checkout.
	if _, currentFile, _, ok := runtime.Caller(0); ok && filepath.IsAbs(currentFile) {
		candidates = append(candidates, filepath.Join(filepath.Dir(currentFile), "scripts", sourceName))
	}
	for _, scriptPath := range candidates {
		if info, err := os.Stat(scriptPath); err == nil && info.Mode().IsRegular() {
			return scriptPath, nil
		}
	}
	return "", fmt.Errorf("cannot find %s; build the web SSR assets", bundleName)
}
