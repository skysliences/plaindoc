import { ImageOff } from "lucide-react";
import { useState } from "react";
import { PREVIEW_BODY_ID } from "../editor/constants";

export function WorkspaceImagePane({ title, url, copyDenied }: {
  title: string;
  url: string | null;
  copyDenied: boolean;
}) {
  const [hasError, setHasError] = useState(false);

  return (
    <section
      id={PREVIEW_BODY_ID}
      className="pane workspace-image-pane"
      aria-label="图片预览"
      data-document-copy-denied={copyDenied ? "1" : undefined}
    >
      {url && !hasError ? (
        <img src={url} alt={title} decoding="async" onError={() => setHasError(true)} />
      ) : (
        <p className="workspace-image-pane__error" role="status">
          <ImageOff size={20} aria-hidden="true" />
          无法加载图片
        </p>
      )}
    </section>
  );
}
