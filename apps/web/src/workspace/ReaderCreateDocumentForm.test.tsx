import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { TreeNode, WorkspaceGateway } from "../data-access";
import { ReaderCreateDocumentForm } from "./ReaderCreateDocumentForm";

const parentOrigin = "https://docs.example.com";
const folder: TreeNode = {
  id: "folder-1",
  spaceId: "space-1",
  parentId: null,
  type: "folder",
  title: "项目",
  sort: 0,
  children: []
};

function setup() {
  const createNode = vi.fn<WorkspaceGateway["createNode"]>();
  const getTree = vi.fn<WorkspaceGateway["getTree"]>().mockResolvedValue([folder]);
  const importDocuments = vi.fn<WorkspaceGateway["importDocuments"]>();
  const onCancel = vi.fn();
  const onCreated = vi.fn();
  const onDocumentImported = vi.fn();
  const onFolderCreated = vi.fn();
  const onFolderImported = vi.fn();
  render(
    <ReaderCreateDocumentForm
      spaceId="space-1"
      parentOrigin={parentOrigin}
      gateway={{ createNode, getTree, importDocuments }}
      onCancel={onCancel}
      onCreated={onCreated}
      onDocumentImported={onDocumentImported}
      onFolderCreated={onFolderCreated}
      onFolderImported={onFolderImported}
    />
  );
  return { createNode, importDocuments, onCancel, onCreated, onDocumentImported, onFolderCreated, onFolderImported };
}

describe("ReaderCreateDocumentForm", () => {
  it("creates an empty folder in the selected location and reports its node ID", async () => {
    const { createNode, onFolderCreated, onCreated } = setup();
    createNode.mockResolvedValue({ nodeId: "folder-2" });
    fireEvent.click(screen.getByRole("button", { name: "新建文件夹" }));
    expect(screen.queryByLabelText("文档标识（可选）")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("文件夹名称"), { target: { value: "  项目资料  " } });
    await screen.findByRole("option", { name: "项目" });
    fireEvent.change(screen.getByLabelText("保存位置"), { target: { value: folder.id } });
    fireEvent.click(screen.getByRole("button", { name: "创建文件夹" }));

    await waitFor(() => expect(onFolderCreated).toHaveBeenCalledWith("folder-2"));
    expect(createNode).toHaveBeenCalledWith({
      spaceId: "space-1",
      parentId: "folder-1",
      type: "folder",
      title: "项目资料"
    });
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("keeps the root directory as the default for a document", async () => {
    const { createNode, onCreated } = setup();
    createNode.mockResolvedValue({ nodeId: "node-2", docId: "doc-2" });
    expect(screen.getByLabelText("保存位置")).toHaveValue("");
    fireEvent.change(screen.getByLabelText("文档标题"), { target: { value: "项目计划" } });
    fireEvent.change(screen.getByLabelText("文档标识（可选）"), { target: { value: "project-plan" } });
    fireEvent.click(screen.getByRole("button", { name: "创建文档" }));

    await waitFor(() => expect(onCreated).toHaveBeenCalledWith("doc-2", "project-plan"));
    expect(createNode).toHaveBeenCalledWith({
      spaceId: "space-1",
      parentId: null,
      type: "doc",
      title: "项目计划",
      documentIdentifier: "project-plan"
    });
  });

  it.each(["docx", "xlsx"])("imports a .%s file in the selected folder and opens it for reading", async (format) => {
    const { importDocuments, onCreated, onDocumentImported } = setup();
    const name = `项目.${format}`;
    importDocuments.mockResolvedValue({
      totalCount: 1,
      successCount: 1,
      failedCount: 0,
      items: [{
        sourceName: name,
        sourcePath: name,
        detectedType: format,
        status: "success",
        stage: "done",
        createdDocumentId: "office-doc"
      }],
      failureItems: [],
      createdNodes: []
    });
    fireEvent.click(screen.getByRole("button", { name: "上传文件" }));
    const picker = screen.getByLabelText("本地文件");
    const submit = screen.getByRole("button", { name: "导入" });
    expect(picker).toHaveAttribute("accept", ".docx,.xlsx");
    expect(submit).toBeDisabled();
    const file = new File(["fixture"], name);
    fireEvent.change(picker, { target: { files: [file] } });
    expect(screen.getByText(name)).toBeVisible();
    await screen.findByRole("option", { name: "项目" });
    fireEvent.change(screen.getByLabelText("保存位置"), { target: { value: folder.id } });
    fireEvent.click(submit);

    await waitFor(() => expect(onDocumentImported).toHaveBeenCalledWith("office-doc"));
    expect(importDocuments).toHaveBeenCalledWith({
      spaceId: "space-1",
      targetNodeId: folder.id,
      files: [file],
      autoExtractTitle: false
    });
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("shows the single-file import failure without navigating away", async () => {
    const { importDocuments, onDocumentImported } = setup();
    const failure = {
      sourceName: "项目.docx",
      sourcePath: "项目.docx",
      detectedType: "docx",
      status: "failed" as const,
      stage: "converting",
      errorMessage: "无法读取文件内容"
    };
    importDocuments.mockResolvedValue({
      totalCount: 1,
      successCount: 0,
      failedCount: 1,
      items: [failure],
      failureItems: [failure],
      createdNodes: []
    });
    fireEvent.click(screen.getByRole("button", { name: "上传文件" }));
    fireEvent.change(screen.getByLabelText("本地文件"), {
      target: { files: [new File(["fixture"], failure.sourceName)] }
    });
    fireEvent.click(screen.getByRole("button", { name: "导入" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(failure.errorMessage);
    expect(onDocumentImported).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "导入" })).toBeEnabled();
  });

  it("rejects untrusted close requests and ignores close while a folder is being created", async () => {
    const { createNode, onCancel, onFolderCreated } = setup();
    let finishCreation: (value: { nodeId: string }) => void = () => {};
    createNode.mockImplementation(() => new Promise((resolve) => { finishCreation = resolve; }));
    const requestClose = (origin: string) => {
      window.dispatchEvent(new MessageEvent("message", {
        origin,
        source: window,
        data: { type: "plaindoc:reader-create-close-request" }
      }));
    };
    requestClose("https://other.example.com");
    expect(onCancel).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "新建文件夹" }));
    fireEvent.change(screen.getByLabelText("文件夹名称"), { target: { value: "项目资料" } });
    fireEvent.click(screen.getByRole("button", { name: "创建文件夹" }));
    requestClose(parentOrigin);
    expect(onCancel).not.toHaveBeenCalled();
    expect(createNode).toHaveBeenCalledOnce();

    finishCreation({ nodeId: "folder-2" });
    await waitFor(() => expect(onFolderCreated).toHaveBeenCalledWith("folder-2"));
  });
});
