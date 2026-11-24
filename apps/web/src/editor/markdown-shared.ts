import type { Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import rehypeRaw from "rehype-raw";
import rehypeSanitize from "rehype-sanitize";
import type { Root } from "mdast";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import type { MermaidRenderResult } from "./mermaid-shared";
import type { PreviewThemeTemplate } from "../preview-themes";
import { buildMarkdownComponents } from "./markdown-components";
import { PREVIEW_HTML_SANITIZE_SCHEMA } from "./markdown-sanitize";
import { remarkBlockAnchorPlugin } from "./markdown-utils";
import { remarkReferenceFootnotePlugin } from "./remark-reference-footnotes";
import { remarkStyledSpanContainerPlugin } from "./remark-styled-span-container";
import type { PreviewLinkRenderMode, TocItem } from "./types";

interface CreateMarkdownComponentsInput {
  activePreviewTheme: PreviewThemeTemplate;
  tocItems: TocItem[];
  onTocNavigate: (item: TocItem) => void;
  requestOrigin?: string;
  preRenderedMermaidMap?: ReadonlyMap<string, MermaidRenderResult>;
  includeMermaidSourcePayload?: boolean;
}

interface CreateSharedRemarkPluginsOptions {
  includeBlockAnchors?: boolean;
}

interface MarkdownNode {
  type: string;
  value?: string;
  children?: MarkdownNode[];
}

// 正文中的单次回车也渲染为换行，保留 Markdown 已解析的段落和代码结构。
function remarkParagraphLineBreaks() {
  function preserveLineBreaks(node: MarkdownNode, inParagraph = false): void {
    if (!node.children) return;
    if (node.type !== "paragraph" && !inParagraph) {
      node.children.forEach((child) => preserveLineBreaks(child));
      return;
    }

    node.children = node.children.flatMap((child): MarkdownNode[] => {
      if (child.type !== "text" || child.value === undefined) {
        preserveLineBreaks(child, true);
        return [child];
      }
      const lines = child.value.split(/\r\n?|\n/);
      if (lines.length === 1) return [child];

      const parts: MarkdownNode[] = [];
      for (const [index, value] of lines.entries()) {
        if (index > 0) parts.push({ type: "break" });
        if (value) parts.push({ type: "text", value });
      }
      return parts;
    });
  }

  return (tree: MarkdownNode) => preserveLineBreaks(tree);
}

// Preserve explicit spacing between top-level prose blocks in the editor and reader.
function remarkBlockSpacing() {
  return (tree: Root) => {
    tree.children.forEach((node, index) => {
      const previousEnd = tree.children[index - 1]?.position?.end.line;
      if (!previousEnd || !node.position || node.position.start.line <= previousEnd + 1) return;
      if (node.type !== "heading" && node.type !== "paragraph" && node.type !== "list") return;
      const data = node.data ??= {};
      (data.hProperties ??= {})["data-markdown-gap"] = node.type;
    });
  };
}

// createSharedRemarkPlugins 统一产出 markdown -> mdast 阶段插件链路。
export function createSharedRemarkPlugins(
  linkRenderMode: PreviewLinkRenderMode,
  options: CreateSharedRemarkPluginsOptions = {}
) {
  const referenceFootnotePlugin: [
    typeof remarkReferenceFootnotePlugin,
    { mode: PreviewLinkRenderMode }
  ] = [remarkReferenceFootnotePlugin, { mode: linkRenderMode }];
  const includeBlockAnchors = options.includeBlockAnchors ?? true;

  const plugins = [
    remarkGfm,
    remarkMath,
    remarkStyledSpanContainerPlugin,
    referenceFootnotePlugin,
    remarkParagraphLineBreaks,
    remarkBlockSpacing
  ];
  return includeBlockAnchors ? [...plugins, remarkBlockAnchorPlugin] : plugins;
}

// createSharedRehypePlugins 统一产出 hast -> react 阶段插件链路。
export function createSharedRehypePlugins() {
  const sanitizePlugin: [typeof rehypeSanitize, typeof PREVIEW_HTML_SANITIZE_SCHEMA] = [
    rehypeSanitize,
    PREVIEW_HTML_SANITIZE_SCHEMA
  ];

  return [rehypeRaw, sanitizePlugin, rehypeKatex];
}

// createSharedMarkdownComponents 统一产出 Markdown 渲染组件映射。
export function createSharedMarkdownComponents(input: CreateMarkdownComponentsInput): Components {
  return buildMarkdownComponents({
    activePreviewTheme: input.activePreviewTheme,
    tocItems: input.tocItems,
    onTocNavigate: input.onTocNavigate,
    requestOrigin: input.requestOrigin,
    preRenderedMermaidMap: input.preRenderedMermaidMap,
    includeMermaidSourcePayload: input.includeMermaidSourcePayload
  });
}
