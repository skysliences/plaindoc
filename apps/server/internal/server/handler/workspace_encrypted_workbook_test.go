package handler

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWorkspaceEncryptedWorkbook(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	plain, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	var protected bytes.Buffer
	err = f.Write(&protected, excelize.Options{Password: "fixture-password"})
	if err != nil {
		t.Fatal(err)
	}
	if !workspaceEncryptedWorkbook(protected.Bytes()) || workspaceEncryptedWorkbook(plain.Bytes()) || workspaceEncryptedWorkbook(bytes.Repeat([]byte{0xd0}, 512)) {
		t.Fatal("incorrect encrypted workbook recognition")
	}
}
