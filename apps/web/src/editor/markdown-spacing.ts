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

// Only promote a sequence of numbered prose lines. Work from paragraph text
// nodes so examples inside code, tables, HTML and nested lists stay literal.
function chineseListEdits(source: string, root: Root, protectedEnd: number): SpacingEdit[] {
  const edits: SpacingEdit[] = [];
  let run: Array<SpacingEdit & { number: number }> = [];

  function finish() {
    if (run.length > 1 && run.every((item, i) => item.number === run[0].number + i)) {
      for (const { from, to, insert } of run) edits.push({ from, to, insert });
    }
    run = [];
  }

  for (const block of root.children) {
    const start = block.position?.start.offset;
    if (block.type !== "paragraph" || start === undefined || start < protectedEnd) {
      finish();
      continue;
    }

    if (!/^[1-9]\d{0,8}、/.test(source.slice(start, start + 12))) {
      finish();
      continue;
    }
    for (const child of block.children) {
      if (child.type !== "text") continue;
      const from = child.position?.start.offset;
      const to = child.position?.end.offset;
      if (from === undefined || to === undefined) continue;
      for (const match of source.slice(from, to).matchAll(/^([ ]{0,3})([1-9]\d{0,8})、[ \t]*/gm)) {
        const offset = from + match.index + match[1].length;
        const lineStart = source.lastIndexOf("\n", offset - 1) + 1;
        if (!/^ {0,3}$/.test(source.slice(lineStart, offset))) continue;
        run.push({ from: offset, to: from + match.index + match[0].length,
          number: Number(match[2]), insert: match[2] + ". " });
      }
    }
  }
  finish();
  return edits;
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
  edits.push(...chineseListEdits(source, root, protectedEnd));
  return edits.sort((a, b) => a.from - b.from);
}

export function formatMarkdownSpacing(view: EditorView) {
  if (view.state.readOnly) return false;
  const changes = markdownSpacingEdits(view.state.doc.toString());
  if (!changes.length) return false;
  view.dispatch({
    changes,
    annotations: [Transaction.userEvent.of("input.format"), isolateHistory.of("full")]
  });
  return true;
}
