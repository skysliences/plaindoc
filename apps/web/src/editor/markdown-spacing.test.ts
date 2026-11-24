import { describe, expect, it } from "vitest";
import { markdownSpacingEdits } from "./markdown-spacing";

function format(text: string) {
  for (const e of markdownSpacingEdits(text).reverse()) {
    text = text.slice(0, e.from) + e.insert + text.slice(e.to);
  }
  return text;
}

describe("Markdown spacing", () => {
  it("separates headings, paragraphs and lists, and is idempotent", () => {
    const result = format("#   周报\n本周进展\n-   完成测试\n- 发布\n\n\n##   下周\n安排回访");
    expect(result).toBe("# 周报\n\n本周进展\n\n- 完成测试\n- 发布\n\n## 下周\n\n安排回访");
    expect(markdownSpacingEdits(result)).toEqual([]);
  });
  it("preserves nested list structure, task items and paragraph line breaks", () => {
    expect(format("- 父项\n  1. 子项\n  2. 子项\n- [x] 完成\n\n\n甲  \n乙"))
      .toBe("- 父项\n  1. 子项\n  2. 子项\n- [x] 完成\n\n甲  \n乙");
  });
  it("keeps literal text in code, math, tables and front matter", () => {
    const chunks = ["---\ntitle: sample\n\n\nnote: name\n---", "~~~md\n#   literal\n\n\n-   literal\n~~~",
      "$$\na+b\n\n\nc-d\n$$", "| A | B |\n|---|---|\n| 1 | 2 |", "    #   code\n\n\n    -   code"];
    for (const chunk of chunks) expect(format(chunk)).toBe(chunk);
  });
  it("retains CRLF, links, and ordered marker numbers", () => {
    expect(format("##  Links\r\n[site](https://example.com)\r\n\r\n4.   four"))
      .toBe("## Links\r\n\r\n[site](https://example.com)\r\n\r\n4. four");
  });
  it("promotes Chinese numbered paragraphs and soft lines to an ordered list", () => {
    const input = "1、模型更换与兼容。\r\n\r\n2、  模型下载与校验。\r\n3、**推理框架**与参数调整。";
    const expected = "1. 模型更换与兼容。\r\n\r\n2. 模型下载与校验。\r\n3. **推理框架**与参数调整。";
    expect(format(input)).toBe(expected);
    expect(format(expected)).toBe(expected);
    expect(format("9、权限\n\n10、计费\n\n11、监控")).toBe("9. 权限\n\n10. 计费\n\n11. 监控");
  });
  it("leaves isolated labels, numbering gaps, and literal examples alone", () => {
    for (const input of ["1、单独一项", "1、甲\n\n3、丙", "1、甲\n\n其他段落\n\n2、乙",
      "~~~md\n1、甲\n2、乙\n~~~", "> 1、甲\n> 2、乙", "`1、甲\n2、乙`",
      "---\ntitle: demo\n1、甲\n2、乙\n---"]) {
      expect(format(input)).toBe(input);
    }
  });
});
