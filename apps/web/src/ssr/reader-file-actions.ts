import { getDataGateway } from "../data-access";

document.addEventListener("click", async (event) => {
  if (!(event.target instanceof Element)) return;
  const button = event.target.closest<HTMLButtonElement>("button[data-reader-delete-file], button[data-reader-delete-folder]");
  const folderId = button?.dataset.readerDeleteFolder;
  const nodeId = folderId ?? button?.dataset.readerDeleteFile;
  if (!button || button.disabled || !nodeId) return;
  const name = button.dataset.fileTitle;
  const message = folderId
    ? `确定删除文件夹「${name}」及其中的所有文件和子文件夹吗？删除后无法撤销。`
    : `确定删除「${name}」吗？删除后无法撤销。`;
  if (!window.confirm(message)) return;

  button.disabled = true;
  try {
    await getDataGateway().workspace.deleteNode(nodeId, { leafOnly: !folderId });
    const active = button.parentElement?.querySelector('[data-reader-active="1"]');
    if (active && button.dataset.spacePath) {
      window.location.assign(button.dataset.spacePath);
    } else {
      window.location.reload();
    }
  } catch (error) {
    window.alert(error instanceof Error ? error.message : "删除失败，请刷新页面后重试。");
    button.disabled = false;
  }
});
