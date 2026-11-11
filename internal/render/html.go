package render

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var mdConverter = converter.NewConverter(converter.WithPlugins(
	base.NewBasePlugin(),
	commonmark.NewCommonmarkPlugin(
		commonmark.WithLinkEmptyHrefBehavior(commonmark.LinkBehaviorSkip),
		commonmark.WithLinkEmptyContentBehavior(commonmark.LinkBehaviorSkip),
	),
	table.NewTablePlugin(table.WithSkipEmptyRows(true)),
))

// htmlToMarkdown strips the noise email HTML is made of (hidden preheaders,
// tracking pixels, layout tables, conditional comments) and converts what
// remains to markdown, which reads well as plain text.
func htmlToMarkdown(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return ""
	}
	clean(doc)
	md, err := mdConverter.ConvertNode(doc)
	if err != nil {
		return ""
	}
	return tidy(string(md))
}

func clean(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		switch c.Type {
		case html.CommentNode, html.DoctypeNode:
			n.RemoveChild(c)
			continue
		case html.TextNode:
			c.Data = invisible.Replace(c.Data)
			continue
		case html.ElementNode:
		default:
			continue
		}
		if dropped(c) {
			n.RemoveChild(c)
			continue
		}
		if c.DataAtom == atom.Img {
			if alt := strings.TrimSpace(invisible.Replace(attr(c, "alt"))); alt != "" && !isPixel(c) {
				n.InsertBefore(&html.Node{Type: html.TextNode, Data: alt}, c)
			}
			n.RemoveChild(c)
			continue
		}
		clean(c)
		switch c.DataAtom {
		case atom.A:
			if !allowedLink(attr(c, "href")) {
				setAttr(c, "href", "")
			}
			if strings.TrimSpace(textContent(c)) == "" {
				n.RemoveChild(c)
			}
		case atom.Table:
			if !isDataTable(c) {
				unwrapTable(c)
			}
		case atom.Center:
			toDiv(c)
		}
	}
}

// invisible removes characters senders use to pad preheaders and defeat
// spam filters; nbsp becomes a plain space so markdown doesn't escape it.
var invisible = strings.NewReplacer(
	"\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "", "\ufeff", "",
	"\u034f", "", "\u00ad", "", "\u00a0", " ",
)

var droppedAtoms = map[atom.Atom]bool{
	atom.Head: true, atom.Style: true, atom.Script: true, atom.Title: true,
	atom.Meta: true, atom.Link: true, atom.Noscript: true, atom.Template: true,
	atom.Svg: true, atom.Object: true, atom.Iframe: true,
}

func dropped(n *html.Node) bool {
	if droppedAtoms[n.DataAtom] {
		return true
	}
	if hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true" {
		return true
	}
	for decl := range strings.SplitSeq(attr(n, "style"), ";") {
		prop, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		val = strings.ToLower(strings.TrimSpace(strings.Replace(val, "!important", "", 1)))
		switch {
		case prop == "display" && val == "none",
			prop == "visibility" && val == "hidden",
			prop == "mso-hide" && val == "all",
			prop == "max-height" && (val == "0" || val == "0px"),
			prop == "opacity" && val == "0":
			return true
		}
	}
	return false
}

func isPixel(n *html.Node) bool {
	small := func(v string) bool { v = strings.TrimSuffix(v, "px"); return v == "0" || v == "1" }
	return small(attr(n, "width")) || small(attr(n, "height"))
}

func allowedLink(href string) bool {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

var blockAtoms = map[atom.Atom]bool{
	atom.Table: true, atom.Div: true, atom.P: true, atom.Ul: true, atom.Ol: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true,
	atom.H6: true, atom.Blockquote: true, atom.Hr: true, atom.Center: true,
}

// isDataTable tells a real table (a receipt's line items) from the nested
// tables email builders use for layout: it needs at least two rows with
// two filled cells, and cells holding only inline content.
func isDataTable(t *html.Node) bool {
	if attr(t, "role") == "presentation" {
		return false
	}
	filledRows := 0
	ok := true
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil && ok; c = c.NextSibling {
			switch c.DataAtom {
			case atom.Thead, atom.Tbody, atom.Tfoot:
				walk(c)
			case atom.Tr:
				filled := 0
				for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.DataAtom != atom.Td && cell.DataAtom != atom.Th {
						continue
					}
					if hasBlock(cell) {
						ok = false
						return
					}
					if strings.TrimSpace(textContent(cell)) != "" {
						filled++
					}
				}
				if filled >= 2 {
					filledRows++
				}
			}
		}
	}
	walk(t)
	return ok && filledRows >= 2
}

func hasBlock(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if blockAtoms[c.DataAtom] || hasBlock(c) {
			return true
		}
	}
	return false
}

// unwrapTable turns a layout table into plain blocks, one per cell, leaving
// nested data tables alone.
func unwrapTable(t *html.Node) {
	toDiv(t)
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.DataAtom {
			case atom.Table:
				continue
			case atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Td, atom.Th, atom.Caption:
				toDiv(c)
			}
			walk(c)
		}
	}
	walk(t)
}

func toDiv(n *html.Node) {
	n.DataAtom, n.Data, n.Attr = atom.Div, "div", nil
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

func setAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, key) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

var blankRuns = regexp.MustCompile(`\n{3,}`)

func tidy(md string) string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
