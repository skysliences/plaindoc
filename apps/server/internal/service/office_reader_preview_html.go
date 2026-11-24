package service

import (
	"errors"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxOfficePreviewImageDataBytes = 384 << 10

// sanitizeOfficePreviewHTML filters the renderer output before placing it in the
// document origin. Office links can contain arbitrary schemes in the input file.
func sanitizeOfficePreviewHTML(raw string) (string, error) {
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(raw), root)
	if err != nil {
		return "", err
	}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	imageBudget := maxOfficePreviewImageDataBytes
	for child := root.FirstChild; child != nil; {
		next := child.NextSibling
		sanitizeOfficePreviewNode(child, &imageBudget)
		child = next
	}

	var output strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		if err := html.Render(&output, node); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(output.String()) == "" {
		return "", errors.New("office preview rendered empty content")
	}
	return output.String(), nil
}

func sanitizeOfficePreviewNode(node *html.Node, imageBudget *int) {
	if node.Type == html.TextNode {
		return
	}
	if node.Type != html.ElementNode || !isOfficePreviewTag(node) {
		node.Parent.RemoveChild(node)
		return
	}
	attributes := node.Attr[:0]
	for _, attribute := range node.Attr {
		if allowOfficePreviewAttribute(node, attribute, imageBudget) {
			attributes = append(attributes, attribute)
		}
	}
	node.Attr = attributes
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		sanitizeOfficePreviewNode(child, imageBudget)
		child = next
	}
}

func isOfficePreviewTag(node *html.Node) bool {
	if node.Namespace == "svg" {
		return node.Data == "svg" || node.Data == "path"
	}
	if node.Namespace != "" {
		return false
	}
	switch node.Data {
	case "a", "b", "blockquote", "br", "button", "caption", "code", "col", "colgroup",
		"dd", "del", "div", "dl", "dt", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr",
		"i", "img", "ins", "li", "ol", "p", "pre", "s", "section", "span", "strong", "sub",
		"sup", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "u", "ul":
		return true
	default:
		return false
	}
}

func allowOfficePreviewAttribute(node *html.Node, attribute html.Attribute, imageBudget *int) bool {
	if attribute.Namespace != "" {
		return false
	}
	switch attribute.Key {
	case "class", "id", "title", "role", "hidden", "width", "height", "colspan", "rowspan", "scope", "alt":
		return true
	case "aria-hidden", "aria-label", "aria-pressed", "aria-selected",
		"data-office-xlsx-reader", "data-office-sheet-tab", "data-office-sheet-panel",
		"data-office-xlsx-fullscreen-toggle", "data-cell-ref":
		return true
	case "type":
		return node.Data == "button" && attribute.Val == "button"
	case "viewBox", "fill", "d", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin":
		return node.Namespace == "svg" && !strings.ContainsAny(attribute.Val, "()\\")
	case "href":
		return node.Data == "a" && isOfficePreviewURL(attribute.Val, true)
	case "src":
		return node.Data == "img" && allowOfficePreviewImage(attribute.Val, imageBudget)
	case "style":
		value := strings.ToLower(attribute.Val)
		return len(value) <= 768 && !strings.ContainsAny(value, "<>()@\\")
	default:
		return false
	}
}

func allowOfficePreviewImage(raw string, imageBudget *int) bool {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return isOfficePreviewURL(value, false)
	}
	header, data, found := strings.Cut(value, ",")
	if !found || data == "" || len(value) > *imageBudget {
		return false
	}
	switch strings.ToLower(header) {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/gif;base64", "data:image/webp;base64", "data:image/bmp;base64":
		*imageBudget -= len(value)
		return true
	default:
		return false
	}
}

func isOfficePreviewURL(raw string, allowContactLinks bool) bool {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsRune(value, '\\') || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "", "http", "https":
		return true
	case "mailto", "tel":
		return allowContactLinks
	default:
		return false
	}
}
