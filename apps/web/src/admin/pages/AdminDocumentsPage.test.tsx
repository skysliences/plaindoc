import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminDocument, DataGateway } from "../../data-access";
import { AdminDocumentsPage } from "./AdminDocumentsPage";

const { confirmMock, promptMock, showToastMock } = vi.hoisted(() => ({
  confirmMock: vi.fn(),
  promptMock: vi.fn(),
  showToastMock: vi.fn()
}));

vi.mock("../components/AdminDialogs", async (importOriginal) => {
  return {
    ...(await importOriginal<typeof import("../components/AdminDialogs")>()),
    useAdminDialogs: () => ({
      confirm: confirmMock,
      prompt: promptMock,
      dialogs: null
    })
  };
});

vi.mock("../../components/ui/toast", () => {
  return {
    showToast: showToastMock
  };
});

const documentItem: AdminDocument = {
  documentId: "doc-word-1",
  documentRouteKey: "doc-word-1",
  nodeId: "node-word-1",
  format: "docx",
  title: "合同文档",
  spaceId: "space-1",
  spaceName: "销售空间",
  spaceOwnerUserId: "owner-1",
  spaceOwnerName: "张三",
  spaceOwnerEmail: "zhangsan@example.com",
  visibility: "public",
  status: "active",
  bannedReason: "",
  bannedAt: null,
  deletedAt: null,
  createdAt: "2026-03-07T00:00:00Z",
  updatedAt: "2026-03-07T00:00:00Z"
};

describe("AdminDocumentsPage", () => {
  beforeEach(() => {
    confirmMock.mockReset();
    promptMock.mockReset();
    showToastMock.mockReset();
  });

  it("renders document format badge and forwards format filter", async () => {
    const listDocuments = vi
      .fn()
      .mockResolvedValueOnce({
        items: [documentItem],
        pagination: { page: 1, pageSize: 20, total: 1 }
      })
      .mockResolvedValueOnce({
        items: [],
        pagination: { page: 1, pageSize: 20, total: 0 }
      });

    const dataGateway = {
      admin: {
        listDocuments
      }
    } as unknown as DataGateway;

    render(<AdminDocumentsPage dataGateway={dataGateway} />);

    const titleLink = await screen.findByText("合同文档");
    expect(titleLink).toBeInTheDocument();
    const row = titleLink.closest("tr");
    if (!row) {
      throw new Error("document row not found");
    }
    expect(within(row).getByText("Word")).toBeInTheDocument();

    const formatField = screen.getByText("格式").closest("label");
    if (!formatField) {
      throw new Error("format filter field not found");
    }

    const nativeSelect = formatField.querySelector("select");
    if (!(nativeSelect instanceof HTMLSelectElement)) {
      throw new Error("format native select not found");
    }
    fireEvent.change(nativeSelect, { target: { value: "xlsx" } });

    await waitFor(() => {
      expect(listDocuments).toHaveBeenLastCalledWith(
        expect.objectContaining({
          format: "xlsx"
        })
      );
    });
  });

  it("loads activity only for the opened document details and retries after failure", async () => {
    const getDocumentActivity = vi
      .fn()
      .mockRejectedValueOnce(new Error("请稍后再试"))
      .mockResolvedValueOnce({ openCount: 12, viewCount: 8, editCount: 3, downloadCount: 0 });
    const dataGateway = {
      admin: {
        listDocuments: vi.fn().mockResolvedValue({
          items: [documentItem],
          pagination: { page: 1, pageSize: 20, total: 1 }
        }),
        getDocumentActivity
      }
    } as unknown as DataGateway;

    render(<AdminDocumentsPage dataGateway={dataGateway} />);

    const detailButton = await screen.findByRole("button", { name: "详情" });
    expect(getDocumentActivity).not.toHaveBeenCalled();
    fireEvent.click(detailButton);

    const dialog = within(screen.getByRole("dialog", { name: "文档详情" }));
    expect(dialog.getByText(documentItem.title)).toBeInTheDocument();
    expect(dialog.getByRole("link", { name: "查看正文" })).toHaveAttribute("href", "/r/space-1/doc-word-1");
    expect(await dialog.findByRole("alert")).toHaveTextContent("请稍后再试");
    expect(getDocumentActivity).toHaveBeenCalledWith(documentItem.documentId);

    fireEvent.click(dialog.getByRole("button", { name: "重试" }));
    await dialog.findByText("打开次数");
    expect(dialog.getByText("打开次数").nextElementSibling).toHaveTextContent("12");
    expect(dialog.getByText("查看次数").nextElementSibling).toHaveTextContent("8");
    expect(dialog.getByText("编辑次数").nextElementSibling).toHaveTextContent("3");
    expect(dialog.getByText("下载次数").nextElementSibling).toHaveTextContent("0");
    expect(getDocumentActivity).toHaveBeenCalledTimes(2);

    fireEvent.click(dialog.getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(detailButton).toHaveFocus();
  });
});
