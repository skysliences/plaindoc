package view

import (
	"bytes"
	"hash/fnv"
	"html/template"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

const (
	spaceTitleCoverMaxLines = 4
	spaceTitleCoverWidth    = 388
)

type spaceTitleCoverColors struct {
	Background string
	Panel      string
	Accent     string
	Ink        string
}

var spaceTitleCoverPalettes = []spaceTitleCoverColors{
	{Background: "#ffc5b9", Panel: "#ffede5", Accent: "#df5946", Ink: "#3e2019"},
	{Background: "#f8dd8a", Panel: "#fff4ca", Accent: "#bf7b24", Ink: "#352511"},
	{Background: "#b4d9c1", Panel: "#e4f6db", Accent: "#3e8a66", Ink: "#1b3a2f"},
	{Background: "#c9c3ee", Panel: "#efecff", Accent: "#7460bd", Ink: "#30234c"},
}

var spaceTitleCoverTemplate = template.Must(template.New("space-title-cover").Parse(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 500 800" width="500" height="800" role="img" aria-labelledby="cover-title">
<title id="cover-title">{{ .Title }}</title>
<rect width="500" height="800" fill="{{ .Colors.Background }}"/>
<path d="M0 674C102 616 160 754 278 696S427 561 500 608V800H0Z" fill="{{ .Colors.Accent }}" opacity=".19"/>
<circle cx="500" cy="75" r="183" fill="{{ .Colors.Accent }}" opacity=".12"/>
<rect x="32" y="42" width="436" height="716" rx="32" fill="{{ .Colors.Panel }}"/>
<path d="M55 124h66M55 135h36" stroke="{{ .Colors.Accent }}" stroke-width="4" stroke-linecap="round"/>
<path d="M410 70v40m-20-20h40M375 115v22m-11-11h22" stroke="{{ .Colors.Accent }}" stroke-width="3" stroke-linecap="round" opacity=".75"/>
<path d="M60 696Q130 673 200 691T348 688" fill="none" stroke="{{ .Colors.Accent }}" stroke-width="4" stroke-linecap="round" opacity=".65"/>
<path d="M60 715h44" stroke="{{ .Colors.Accent }}" stroke-width="3" stroke-linecap="round" opacity=".38"/>
<text fill="{{ .Colors.Ink }}" font-family="system-ui, PingFang SC, Microsoft YaHei, Noto Sans CJK SC, sans-serif" font-size="{{ .FontSize }}" font-weight="850">{{ range .Lines }}<tspan x="56" y="{{ .Y }}">{{ .Text }}</tspan>{{ end }}</text>
</svg>`,
))

func RenderSpaceTitleCover(spaceID, title string) ([]byte, error) {
	title = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) || char == '\ufffe' || char == '\uffff' {
			return ' '
		}
		return char
	}, title)
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		title = "未命名空间"
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(spaceID))
	colors := spaceTitleCoverPalettes[hash.Sum32()%uint32(len(spaceTitleCoverPalettes))]

	// Keep ordinary titles to two lines before using a taller text block.
	preferredLines := 2
	titleWidth := spaceTitleCoverTextWidth(title)
	if titleWidth > 13 {
		preferredLines = 3
	}
	if titleWidth > 20 {
		preferredLines = spaceTitleCoverMaxLines
	}
	var fontSize int
	var lines []string
	for _, size := range []int{68, 58, 48, 40, 34} {
		fontSize = size
		lines = wrapSpaceTitleCover(title, float64(spaceTitleCoverWidth)/float64(size))
		if len(lines) <= preferredLines {
			break
		}
	}

	if len(lines) > spaceTitleCoverMaxLines {
		lines = lines[:spaceTitleCoverMaxLines]
		last := lines[len(lines)-1]
		budget := float64(spaceTitleCoverWidth) / float64(fontSize)
		for spaceTitleCoverTextWidth(last)+spaceTitleCoverTextWidth("…") > budget {
			_, length := utf8.DecodeLastRuneInString(last)
			last = last[:len(last)-length]
		}
		lines[len(lines)-1] = strings.TrimSpace(last) + "…"
	}

	type coverLine struct {
		Text string
		Y    int
	}
	lineHeight := fontSize * 5 / 4
	startY := 400 - (len(lines)-1)*lineHeight/2 + fontSize/3
	coverLines := make([]coverLine, 0, len(lines))
	for index, line := range lines {
		coverLines = append(coverLines, coverLine{Text: line, Y: startY + index*lineHeight})
	}

	var result bytes.Buffer
	err := spaceTitleCoverTemplate.Execute(&result, struct {
		Title    string
		Colors   spaceTitleCoverColors
		FontSize int
		Lines    []coverLine
	}{title, colors, fontSize, coverLines})
	return result.Bytes(), err
}

func wrapSpaceTitleCover(title string, budget float64) []string {
	var lines []string
	for title != "" {
		used := 0.0
		end := len(title)
		lastSpace := 0
		for index, char := range title {
			if used+spaceTitleCoverRuneWidth(char) > budget {
				end = index
				if lastSpace > 0 {
					end = lastSpace
				}
				break
			}
			used += spaceTitleCoverRuneWidth(char)
			if char == ' ' {
				lastSpace = index
			}
		}
		lines = append(lines, strings.TrimSpace(title[:end]))
		title = strings.TrimSpace(title[end:])
	}
	return lines
}

func spaceTitleCoverTextWidth(text string) float64 {
	total := 0.0
	for _, char := range text {
		total += spaceTitleCoverRuneWidth(char)
	}
	return total
}

func spaceTitleCoverRuneWidth(char rune) float64 {
	switch {
	case unicode.IsMark(char):
		return 0
	case char == ' ':
		return 0.34
	case char == 'm' || char == 'w' || char == 'M' || char == 'W':
		return 0.96
	case char <= unicode.MaxASCII:
		return 0.72
	}
	switch width.LookupRune(char).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 1.04
	default:
		return 0.86
	}
}
