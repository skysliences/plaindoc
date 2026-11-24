import { render, screen } from "@testing-library/react";
import ReactMarkdown from "react-markdown";
import { describe, expect, it, vi } from "vitest";
import { DEFAULT_PREVIEW_THEME_TEMPLATE } from "../preview-themes";
import {
  createSharedMarkdownComponents,
  createSharedRehypePlugins,
  createSharedRemarkPlugins
} from "./markdown-shared";

describe("createSharedRemarkPlugins", () => {
  it("preserves single line breaks in preview paragraphs and keeps Markdown structure", () => {
    const { container } = render(
      <ReactMarkdown remarkPlugins={createSharedRemarkPlugins("link")} rehypePlugins={createSharedRehypePlugins()}>
        {[
          "1\n**2**\n3",
          "4  \n5\n6",
          "- 第一项\n- 第二项",
          "```text\n代码第一行\n代码第二行\n```"
        ].join("\n\n")}
      </ReactMarkdown>
    );

    const paragraphs = container.querySelectorAll("p");
    expect(paragraphs).toHaveLength(2);
    expect(paragraphs[0].querySelectorAll("br")).toHaveLength(2);
    expect(paragraphs[0].querySelector("strong")).toHaveTextContent("2");
    expect(paragraphs[1].querySelectorAll("br")).toHaveLength(2);
    expect(Array.from(container.querySelectorAll("ul > li"), (item) => item.textContent)).toEqual([
      "第一项",
      "第二项"
    ]);
    expect(container.querySelector("pre > code")?.textContent).toBe("代码第一行\n代码第二行\n");
    expect(container.querySelector("pre br")).toBeNull();
  });
});

describe("createSharedMarkdownComponents", () => {
  it("opens external links in a new window and adds nofollow rel tokens", () => {
    const components = createSharedMarkdownComponents({
      activePreviewTheme: DEFAULT_PREVIEW_THEME_TEMPLATE,
      tocItems: [],
      onTocNavigate: vi.fn(),
      requestOrigin: "https://docs.plaindoc.example"
    });

    render(
      <ReactMarkdown
        remarkPlugins={createSharedRemarkPlugins("link")}
        rehypePlugins={createSharedRehypePlugins()}
        components={components}
      >
        {[
          "[外链](https://example.com/docs)",
          "[相对内链](/guide/getting-started)",
          "[同源绝对内链](https://docs.plaindoc.example/guide)",
          "[页内锚点](#toc)"
        ].join("\n\n")}
      </ReactMarkdown>
    );

    const externalLink = screen.getByRole("link", { name: "外链" });
    const relativeInternalLink = screen.getByRole("link", { name: "相对内链" });
    const sameOriginLink = screen.getByRole("link", { name: "同源绝对内链" });
    const hashLink = screen.getByRole("link", { name: "页内锚点" });

    expect(externalLink).toHaveAttribute("target", "_blank");
    expect(externalLink).toHaveAttribute("rel", "noopener noreferrer nofollow");
    expect(relativeInternalLink).not.toHaveAttribute("target");
    expect(relativeInternalLink).not.toHaveAttribute("rel");
    expect(sameOriginLink).not.toHaveAttribute("target");
    expect(sameOriginLink).not.toHaveAttribute("rel");
    expect(hashLink).not.toHaveAttribute("target");
    expect(hashLink).not.toHaveAttribute("rel");
  });

  it("does not render reader-side mermaid source payload in editor preview by default", () => {
    const components = createSharedMarkdownComponents({
      activePreviewTheme: DEFAULT_PREVIEW_THEME_TEMPLATE,
      tocItems: [],
      onTocNavigate: vi.fn()
    });

    const { container } = render(
      <ReactMarkdown
        remarkPlugins={createSharedRemarkPlugins("link")}
        rehypePlugins={createSharedRehypePlugins()}
        components={components}
      >
        {"```mermaid\ngraph TD\nA-->B\n```"}
      </ReactMarkdown>
    );

    expect(container.querySelector("[data-reader-hook='mermaid']")).toBeTruthy();
    expect(container.querySelector("[data-reader-mermaid-source='1']")).toBeNull();
  });
});
