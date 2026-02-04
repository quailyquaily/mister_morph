package agent

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

const (
	rulePreferURLFetch = "When a user provides a direct URL, prefer url_fetch and skip web_search."
	ruleBatchURLFetch  = "When multiple URLs are provided, emit a batch of url_fetch tool calls in one response."
	rulePreferDownload = "When a URL likely points to a file (e.g. .pdf, .zip, .png, .jpg, .mp4), prefer download_path instead of inline body."
	ruleRangeProbe     = "If file type is unclear, you may first issue a small-range GET using a Range header with a low max_bytes to confirm content type before downloading."
	ruleURLFetchFail   = "If url_fetch fails (blocked, timeout, non-2xx), do not fabricate; report the error and ask for updated allowlist or parameters."
)

func augmentPromptSpecForTask(spec PromptSpec, task string) PromptSpec {
	urls := ExtractDirectURLs(task)
	if len(urls) == 0 {
		return spec
	}

	out := spec
	out.Rules = append([]string{}, spec.Rules...)
	out.Blocks = append([]PromptBlock{}, spec.Blocks...)

	out.Rules = appendRule(out.Rules, rulePreferURLFetch)
	if len(urls) > 1 {
		out.Rules = appendRule(out.Rules, ruleBatchURLFetch)
	}

	anyBinary := false
	anyNonBinary := false
	for _, u := range urls {
		if isBinaryLikelyURL(u) {
			anyBinary = true
		} else {
			anyNonBinary = true
		}
	}
	if anyBinary {
		out.Rules = appendRule(out.Rules, rulePreferDownload)
	}
	if anyNonBinary {
		out.Rules = appendRule(out.Rules, ruleRangeProbe)
	}
	out.Rules = appendRule(out.Rules, ruleURLFetchFail)

	return out
}

func appendRule(rules []string, rule string) []string {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return rules
	}
	for _, existing := range rules {
		if existing == rule {
			return rules
		}
	}
	return append(rules, rule)
}

var urlRegex = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"']+`)

func ExtractDirectURLs(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	matches := urlRegex.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		cleaned := strings.TrimSpace(strings.TrimRight(match, ".,;:!?)]}\"'>"))
		if cleaned == "" {
			continue
		}
		parsed, err := url.Parse(cleaned)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func isBinaryLikelyURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	lowerPath := strings.ToLower(parsed.Path)
	ext := strings.ToLower(path.Ext(lowerPath))
	if ext != "" && binaryExtensions[ext] {
		return true
	}

	if containsAny(lowerPath, []string{"/download", "/attachment", "/export", "/file", "/media", "/assets", "/blob"}) {
		return true
	}

	rawQuery := strings.ToLower(parsed.RawQuery)
	if containsAny(rawQuery, []string{
		"download=",
		"attachment=",
		"alt=media",
		"format=pdf",
		"raw=1",
	}) {
		return true
	}

	host := strings.ToLower(parsed.Host)
	if strings.HasPrefix(host, "cdn.") || strings.Contains(host, ".cdn.") ||
		strings.HasPrefix(host, "static.") || strings.Contains(host, ".static.") ||
		strings.HasPrefix(host, "assets.") || strings.Contains(host, ".assets.") {
		return true
	}

	return false
}

var binaryExtensions = map[string]bool{
	".pdf":  true,
	".zip":  true,
	".gz":   true,
	".tgz":  true,
	".tar":  true,
	".7z":   true,
	".rar":  true,
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".mp4":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
	".mp3":  true,
	".wav":  true,
	".csv":  true,
	".xlsx": true,
	".xls":  true,
	".ppt":  true,
	".pptx": true,
	".doc":  true,
	".docx": true,
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
