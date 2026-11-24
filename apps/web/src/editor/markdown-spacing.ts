import { isolateHistory } from "@codemirror/commands";
import { Transaction } from "@codemirror/state";
import type { EditorView } from "@codemirror/view";
import type { List, Root } from "mdast";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import remarkParse from "remark-parse";
import { unified } from "unified";

const parser = unified().use(remarkParse).use(remarkGfm).use(remarkMath);
const spacedBlocks = new Set(["heading", "paragraph", "list"]);

interface SpacingEdit {
  from: number;
  to: number;
  insert: string;
}

export function markdownSpacingEdits(source: string): SpacingEdit[] {
  const root = parser.parse(source);
  const newline = source.includes("\r\n") ? "\r\n" : "\n";
  const edits: SpacingEdit[] = [];
  // Front matter is interpreted as Markdown by this reader. Leave its contents intact.
  const frontMatter = /^---[ \t]*\r?\n[\s\S]*?^(?:---|\.\.\.)[ \t]*(?=\r?$)/m;
  const metadata = source.startsWith("---") ? frontMatter.exec(source) : null;
  const protectedEnd = metadata?.index === 0 ? metadata[0].length : 0;

  function replace(from: number, to: number, insert: string) {
    if (from >= protectedEnd && source.slice(from, to) !== insert) edits.push({ from, to, insert });
  }

  function formatList(list: List) {
    let previousEnd: number | undefined;
    for (const item of list.children) {
      const start = item.position?.start.offset;
      const end = item.position?.end.offset;
      if (start === undefined || end === undefined) continue;
      if (previousEnd !== undefined && /^[ \t]*(?:\r?\n[ \t]*){3,}$/.test(source.slice(previousEnd, start))) {
        // Preserve indentation of the following marker, including nested lists.
        const gap = source.slice(previousEnd, start);
        replace(previousEnd, start, newline + newline + gap.slice(gap.lastIndexOf("\n") + 1));
      }
      if (item.position?.start.line === item.position?.end.line && item.children[0]?.type === "paragraph") {
        const marker = /^(?:[-+*]|\d{1,9}[.)])([ \t]+)/.exec(source.slice(start, end));
        if (marker) replace(start + marker[0].length - marker[1].length, start + marker[0].length, " ");
      }
      for (const child of item.children) if (child.type === "list") formatList(child);
      previousEnd = end;
    }
  }

  let previous: Root["children"][number] | undefined;
  for (const node of root.children) {
    const start = node.position?.start.offset;
    const end = previous?.position?.end.offset;
    if (start === undefined) continue;
    if (previous && end !== undefined && (spacedBlocks.has(previous.type) || spacedBlocks.has(node.type))) {
      const gap = source.slice(end, start);
      // Keep the next block's leading indentation. Change only inter-block blank lines.
      const boundary = /^[ \t]*(?:\r?\n[ \t]*)*\r?\n([ \t]*)$/.exec(gap);
      if (boundary) replace(end, start, newline + newline + boundary[1]);
    }
    if (node.type === "heading") {
      const marker = /^(#{1,6})([ \t]+)/.exec(source.slice(start));
      if (marker) replace(start + marker[1].length, start + marker[0].length, " ");
    }
    if (node.type === "list") formatList(node);
    previous = node;
  }
  return edits;
}

export function formatMarkdownSpacing(view: EditorView) {
  if (view.state.readOnly) return;
  const changes = markdownSpacingEdits(view.state.doc.toString());
  if (changes.length) view.dispatch({
    changes,
    annotations: [Transaction.userEvent.of("input.format"), isolateHistory.of("full")]
  });
}
