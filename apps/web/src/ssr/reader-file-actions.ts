import { getDataGateway } from "../data-access";

document.addEventListener("click", async (event) => {
  if (!(event.target instanceof Element)) return;
  const button = event.target.closest<HTMLButtonElement>("button[data-reader-delete-file]");
  const nodeId = button?.dataset.readerDeleteFile;
  if (!button || button.disabled || !nodeId) return;
  if (!window.confirm(`确定删除「${button.dataset.fileTitle}」吗？删除后无法撤销。`)) return;

  button.disabled = true;
  try {
    await getDataGateway().workspace.deleteNode(nodeId, { leafOnly: true });
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
