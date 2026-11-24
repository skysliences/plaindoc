import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FolderUploadPicker } from "./FolderUploadPicker";

function folderFile(path: string): File {
  const file = new File(["contents"], path.slice(path.lastIndexOf("/") + 1));
  Object.defineProperty(file, "webkitRelativePath", { value: path });
  return file;
}

describe("FolderUploadPicker", () => {
  it("leaves hidden files and their directory subtree out of the upload selection", () => {
    const onSelect = vi.fn();
    render(<FolderUploadPicker onSelect={onSelect} />);
    const visible = folderFile("project/guide.v2.md");

    fireEvent.change(screen.getByLabelText("本地文件夹"), {
      target: {
        files: [folderFile("project/._README.md"), visible, folderFile("project/nested/.git/objects/readme.md")]
      }
    });

    expect(onSelect).toHaveBeenCalledWith({
      files: [visible],
      folderManifest: { paths: ["project/guide.v2.md"], directories: ["project", "project/nested"] }
    });
  });

  it("keeps a visible folder when all of its files are hidden", () => {
    const onSelect = vi.fn();
    render(<FolderUploadPicker onSelect={onSelect} />);

    fireEvent.change(screen.getByLabelText("本地文件夹"), {
      target: { files: [folderFile("project/.DS_Store")] }
    });

    expect(onSelect).toHaveBeenCalledWith({
      files: [],
      folderManifest: { paths: [], directories: ["project"] }
    });
  });
});
