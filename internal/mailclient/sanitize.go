package mailclient

import (
	"bytes"
	"golang.org/x/net/html"
	"net/url"
	"strings"
)

// SanitizeHTML uses an allowlist parser, never regular expressions, for untrusted mail.
func SanitizeHTML(source string, remote bool) string {
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	allowed := map[string]bool{}
	for _, tag := range strings.Fields("p div span br hr b strong i em u s del strike h1 h2 h3 h4 h5 h6 ul ol li blockquote pre code a img table thead tbody tfoot tr td th caption sup sub center font") {
		allowed[tag] = true
	}
	forbidden := map[string]bool{}
	for _, tag := range strings.Fields("script style iframe object embed applet form input button textarea select option video audio source svg math template noscript head") {
		forbidden[tag] = true
	}
	var clean func(*html.Node)
	clean = func(n *html.Node) {
		for child := n.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.CommentNode {
				n.RemoveChild(child)
			} else if child.Type == html.ElementNode && forbidden[child.Data] {
				n.RemoveChild(child)
			} else {
				clean(child)
				if child.Type == html.ElementNode {
					if !allowed[child.Data] {
						for child.FirstChild != nil {
							v := child.FirstChild
							child.RemoveChild(v)
							n.InsertBefore(v, child)
						}
						n.RemoveChild(child)
					} else {
						var attrs []html.Attribute
						for _, at := range child.Attr {
							if at.Namespace != "" {
								continue
							}
							k := strings.ToLower(at.Key)
							v := strings.TrimSpace(at.Val)
							switch k {
							case "alt", "title", "colspan", "rowspan", "width", "height", "align", "dir":
								attrs = append(attrs, at)
							case "href":
								if child.Data == "a" {
									u, e := url.Parse(v)
									if e == nil && (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "mailto") {
										attrs = append(attrs, at)
									}
								}
							case "src":
								if child.Data == "img" {
									lower := strings.ToLower(v)
									data := strings.HasPrefix(lower, "data:image/png;base64,") || strings.HasPrefix(lower, "data:image/jpeg;base64,") || strings.HasPrefix(lower, "data:image/gif;base64,") || strings.HasPrefix(lower, "data:image/webp;base64,")
									u, e := url.Parse(v)
									if data || (remote && e == nil && (u.Scheme == "https" || u.Scheme == "http")) {
										attrs = append(attrs, at)
									}
								}
							}
						}
						child.Attr = attrs
					}
				}
			}
			child = next
		}
	}
	clean(doc)
	var b bytes.Buffer
	_ = html.Render(&b, doc)
	return b.String()
}
