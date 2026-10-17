package notes

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

func noteTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#>*-+`"))
		if line != "" {
			return string([]rune(line)[:min(80, len([]rune(line)))])
		}
	}
	return "笔记"
}

func filenameTitle(title string) string {
	text := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-' {
			return r
		}
		return ' '
	}, title)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "笔记"
	}
	return string([]rune(text)[:min(36, len([]rune(text)))])
}

func markdownLabel(text string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "<", "&lt;", ">", "&gt;").Replace(text)
}

func positionLabel(seconds int64) string {
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func originalTimeLink(id string, seconds int64) string {
	platform, value, _ := strings.Cut(id, ":")
	switch platform {
	case "youtube":
		return "https://www.youtube.com/watch?v=" + value + "&t=" + strconv.FormatInt(seconds, 10) + "s"
	case "bilibili":
		return "https://www.bilibili.com/video/" + value + "/?p=1&t=" + strconv.FormatInt(seconds, 10)
	}
	return ""
}

func exportSource(id string, source *ExportSource) *ExportSource {
	if source != nil {
		copy := *source
		u, err := url.Parse(copy.URL)
		if err != nil || u.User != nil || u.Hostname() == "" || u.Scheme != "http" && u.Scheme != "https" || len(copy.URL) > 4096 {
			copy.URL = ""
		}
		return &copy
	}
	platform, value, _ := strings.Cut(id, ":")
	var raw, title string
	switch platform {
	case "bilibili":
		raw = "https://www.bilibili.com/video/" + value + "/"
		title = "B站视频"
	case "youtube":
		raw = "https://www.youtube.com/watch?v=" + value
		title = "YouTube 视频"
	case "xiaoyuzhou":
		raw = "https://www.xiaoyuzhoufm.com/episode/" + value
		title = "小宇宙节目"
	case "douyin":
		raw = "https://www.douyin.com/video/" + value
		title = "抖音视频"
	case "xiaohongshu":
		raw = "https://www.xiaohongshu.com/explore/" + value
		title = "小红书内容"
	case "rss":
		title = "RSS 内容"
	case "pdf":
		title = "PDF 文档"
	default:
		return nil
	}
	return &ExportSource{Title: title, URL: raw, Platform: platform}
}

const exportReadme = `# Tangentry 笔记导出

sources/ 把同一视频、文章或文档的笔记合成一篇 Markdown，按播放时间排列：每段依次是「关联时间」（时间链接与当时的字幕原句）、「视频画面」（截屏）和「我的记录」，适合从头到尾回顾。

notes/ 中每条已保存笔记对应一份 Markdown，结构与上面相同；「我的记录」保持保存时的源文，未保存草稿不包含在内。images/ 存放笔记截屏，Markdown 用相对路径引用。可把这些 .md 文件放进 Obsidian 等 Markdown 工具。属性使用 Obsidian 官方支持的 JSON frontmatter，包含标签、创建 / 修改时间、内容身份和可用的来源信息。

INDEX.md 提供按创建时间排列的本机索引，同时显示可用的来源链接、关联时间、PDF 页码与原文摘录。摘录作为纯文字引用展示，不执行其中的 Markdown / HTML；每条笔记正文保持原样。文件名使用 UTC 日期、简化标题与完整笔记 ID，避免重名与跨系统路径问题；文件名与属性日期均使用 UTC，Tangentry 页面日期筛选仍按查看者本地日历日。

SOURCES.md 按内容身份分组，同一视频、文章或文档的笔记放在一起；名称相同的不同来源仍分开，未关联来源的随手记单独汇总。索引仅包含这次筛选导出的已保存笔记，不联网补充来源信息。

TAGS.md 按完整标签分组，保留分级标签名称；一条笔记可出现在多个标签下。只列出这次导出的笔记，不补入筛选范围之外的内容。

媒体位置显示到整秒；B站 / YouTube 的 source_url_at_time 可回原视频。RSS / 播客位置仍保留为秒，未伪造平台不支持的时间链接。文章批注保留引用、哈希和 UTF-16 偏移；PDF 保留页码、文件摘要与引用。完整原记录、前后文和 PDF 坐标另存 tangentry-notes.json（schemaVersion 1），便于后续迁移与查证；此版本没有自动导入 / 同步功能。

这是已保存笔记的独立只读快照，不是完整应用备份。除笔记截屏外，不包含 Cookie、工具配置、媒体、PDF、订阅数据库或未保存草稿；正文中原有图片 / 附件链接仍指向原地址。Obsidian 内不会自动显示 Tangentry 的字幕时钟、文章划线或 PDF 图层。来源信息只读取本机收藏 / 历史；缺少来源时保留内容身份，不联网补标题。
`
