package articles

import (
	"context"
	"strings"

	"golang.org/x/net/html"
)

// protectCode keeps source comments and other code content from readability's
// page-noise heuristics while removing only presentation identifiers.
func protectCode(ctx context.Context, root *html.Node) error {
	type entry struct {
		node *html.Node
		code bool
	}
	stack := []entry{{node: root}}
	elements, visited := 0, 0
	for len(stack) > 0 {
		value := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := value.node
		visited++
		if visited%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if node.Type == html.ElementNode {
			elements++
			if elements > 40_000 {
				return failure("原网页结构过于复杂，请打开原内容查看。")
			}
			value.code = value.code || node.Data == "pre" || node.Data == "code"
			// Substack's portable archive is a post browser, not article text.
			// Match its structural class, never visible words or code examples.
			if !value.code && node.Parent != nil {
				archive := false
				for _, attribute := range node.Attr {
					if attribute.Key == "class" {
						for _, class := range strings.Fields(attribute.Val) {
							archive = archive || class == "portable-archive-list"
						}
					}
				}
				if archive {
					node.Parent.RemoveChild(node)
					continue
				}
			}
			if value.code {
				kept := node.Attr[:0]
				for _, attribute := range node.Attr {
					if attribute.Key != "class" && attribute.Key != "id" {
						kept = append(kept, attribute)
					}
				}
				node.Attr = kept
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			stack = append(stack, entry{node: child, code: value.code})
		}
	}
	return ctx.Err()
}
