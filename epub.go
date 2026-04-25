package main

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"path"
	"strings"

	"golang.org/x/net/html"
)

type Book struct {
	Title    string
	Author   string
	Chapters []Chapter
}

type Chapter struct {
	Title string
	Text  string
}

type container struct {
	RootFiles []rootFile `xml:"rootfiles>rootfile"`
}

type rootFile struct {
	FullPath  string `xml:"full-path,attr"`
	MediaType string `xml:"media-type,attr"`
}

type opfPackage struct {
	Metadata opfMetadata `xml:"metadata"`
	Manifest opfManifest `xml:"manifest"`
	Spine    opfSpine    `xml:"spine"`
}

type opfMetadata struct {
	Title   string `xml:"title"`
	Creator string `xml:"creator"`
}

type opfManifest struct {
	Items []opfItem `xml:"item"`
}

type opfItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}

type opfSpine struct {
	ItemRefs []opfItemRef `xml:"itemref"`
}

type opfItemRef struct {
	IDRef string `xml:"idref,attr"`
}

func ParseEPUB(filePath string) (*Book, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("open epub: %w", err)
	}
	defer r.Close()

	files := make(map[string]*zip.File)
	for _, f := range r.File {
		files[f.Name] = f
	}

	opfPath, err := findOPFPath(files)
	if err != nil {
		return nil, err
	}
	opfDir := path.Dir(opfPath)

	pkg, err := parseOPF(files[opfPath])
	if err != nil {
		return nil, err
	}

	manifest := make(map[string]opfItem)
	for _, item := range pkg.Manifest.Items {
		manifest[item.ID] = item
	}

	var chapters []Chapter
	chapterNum := 0
	for _, ref := range pkg.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok {
			continue
		}
		if !strings.Contains(item.MediaType, "html") {
			continue
		}

		itemPath := item.Href
		if opfDir != "." {
			itemPath = opfDir + "/" + item.Href
		}

		f, ok := files[itemPath]
		if !ok {
			continue
		}

		text, title, err := extractText(f)
		if err != nil {
			continue
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		chapterNum++
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chapterNum)
		}

		chapters = append(chapters, Chapter{
			Title: title,
			Text:  text,
		})
	}

	if len(chapters) == 0 {
		return nil, fmt.Errorf("no text content found in epub")
	}

	title := pkg.Metadata.Title
	if title == "" {
		title = strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
	}

	return &Book{
		Title:    sanitizeFilename(title),
		Author:   pkg.Metadata.Creator,
		Chapters: chapters,
	}, nil
}

func findOPFPath(files map[string]*zip.File) (string, error) {
	f, ok := files["META-INF/container.xml"]
	if !ok {
		return "", fmt.Errorf("missing META-INF/container.xml")
	}

	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var c container
	if err := xml.NewDecoder(rc).Decode(&c); err != nil {
		return "", fmt.Errorf("parse container.xml: %w", err)
	}

	for _, rf := range c.RootFiles {
		if rf.MediaType == "application/oebps-package+xml" {
			return rf.FullPath, nil
		}
	}
	if len(c.RootFiles) > 0 {
		return c.RootFiles[0].FullPath, nil
	}
	return "", fmt.Errorf("no rootfile in container.xml")
}

func parseOPF(f *zip.File) (*opfPackage, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var pkg opfPackage
	if err := xml.NewDecoder(rc).Decode(&pkg); err != nil {
		return nil, fmt.Errorf("parse OPF: %w", err)
	}
	return &pkg, nil
}

func extractText(f *zip.File) (string, string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	doc, err := html.Parse(rc)
	if err != nil {
		return "", "", err
	}

	var sb strings.Builder
	var title string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav":
				return
			}
		}

		if n.Type == html.TextNode {
			t := strings.TrimSpace(n.Data)
			if t != "" {
				sb.WriteString(t)
				sb.WriteString(" ")
			}
		}

		if n.Type == html.ElementNode && title == "" {
			switch n.Data {
			case "h1", "h2", "h3":
				title = innerText(n)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote":
				sb.WriteString("\n")
			}
		}
	}
	walk(doc)

	return sb.String(), strings.TrimSpace(title), nil
}

func innerText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

func sanitizeFilename(name string) string {
	r := strings.NewReplacer(
		"/", "-", "\\", "-", ":", " -", "*", "",
		"?", "", "\"", "'", "<", "", ">", "", "|", "-",
	)
	name = strings.TrimSpace(r.Replace(name))
	if name == "" {
		return "Unknown"
	}
	return name
}
