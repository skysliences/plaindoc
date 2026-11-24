package handler

import "testing"

func TestDetectWorkspaceImportTypePDF(t *testing.T) {
	cases := map[string]string{
		"report.pdf":        importDetectedTypePDF,
		"REPORT.PDF":        importDetectedTypePDF,
		"上传/文档.v2.pdf":     importDetectedTypePDF,
		"report.docx":       importDetectedTypeDOCX,
		"report.txt":        importDetectedTypeText,
		"report.unknownbin": importDetectedTypeUnsupported,
	}
	for name, want := range cases {
		if got := detectWorkspaceImportType(name); got != want {
			t.Fatalf("detectWorkspaceImportType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWorkspaceImportPDFReadable(t *testing.T) {
	valid := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")
	if !workspaceImportPDFReadable(valid) {
		t.Fatal("合法 PDF 头部应判定为可读")
	}

	// 规范允许头部前有少量垃圾字节，但要限制在 1024 字节以内。
	padded := append(make([]byte, 512), valid...)
	if !workspaceImportPDFReadable(padded) {
		t.Fatal("头部前的少量填充字节应被容忍")
	}

	invalid := [][]byte{
		nil,
		[]byte("PK\x03\x04zip-content"),
		append(make([]byte, 2048), valid...),
	}
	for _, content := range invalid {
		if workspaceImportPDFReadable(content) {
			t.Fatalf("非 PDF 内容不应判定为可读: %q", content[:min(len(content), 16)])
		}
	}
}
