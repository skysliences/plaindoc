(() => {
  const state = document.getElementById("plaindoc-reader-state") ||
    document.getElementById("plaindoc-reader-initial-state");
  if (!state) return;

  const contentSelector = "[data-reader-hook='article-shell'], .reader-shell";
  const readState = () => {
    try {
      return JSON.parse(state.textContent);
    } catch {
      return null;
    }
  };

  function reportCopy(payload) {
    if (!payload?.trackDocumentActivity || payload.copyDenied || !payload.document?.id) return;
    const { document: currentDocument, share } = payload;
    const path = share?.enabled
      ? "/api/shares/" + encodeURIComponent(share.spaceId) + "/" + encodeURIComponent(share.documentRouteKey)
      : "/api/docs/" + encodeURIComponent(currentDocument.id);
    fetch(path + "/copy-events", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      keepalive: true
    }).catch(() => {});
  }

  document.addEventListener("copy", (event) => {
    const content = document.querySelector(contentSelector);
    const selection = window.getSelection();
    if (!content || !selection || selection.isCollapsed || !selection.containsNode(content, true)) return;
    const payload = readState();
    if (payload?.copyDenied) {
      event.preventDefault();
      event.stopImmediatePropagation();
    } else {
      reportCopy(payload);
    }
  }, true);

  document.addEventListener("plaindoc:clipboard-copy", (event) => {
    if (event.target instanceof Element && event.target.closest(contentSelector)) {
      reportCopy(readState());
    }
  });
})();
