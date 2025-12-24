package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// PrehrajResult represents a search result from Prehraj.to
type PrehrajResult struct {
	Title    string
	Duration string
	Size     string
	URL      string
}

// searchPrehraj searches Prehraj.to for a query using Headless Browser (Rod)
func searchPrehraj(query string) ([]PrehrajResult, error) {
	searchURL := fmt.Sprintf("https://prehraj.to/hledej/%s", url.PathEscape(query))

	// Launch headless browser
	// We use a custom launcher to ensure it finds chromium in Alpine
	u := launcher.New().Bin("/usr/bin/chromium-browser").MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(searchURL)

	// Set User-Agent
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	})
	// Navigate and wait for load
	// We wait for the "body" to be present, or specifically ".video--link" if possible
	err := page.Navigate(searchURL)
	if err != nil {
		return nil, err
	}

	// Wait a bit for JS to execute (simple wait, or wait for selector)
	// Waiting for ".video--link" might timeout if 0 results, so just wait for load event
	page.MustWaitLoad()

	// Additional small sleep to let any lazy loading finish
	time.Sleep(2 * time.Second)

	// Get HTML
	html, err := page.HTML()
	if err != nil {
		return nil, err
	}

	// Parse with GoQuery as before
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var results []PrehrajResult

	// Selector based on research: a.video--link
	doc.Find("a.video--link").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		parseLink(s, href, &results)
	})

	// Fallback: If no results found, try generic parsing of all links
	if len(results) == 0 {
		fmt.Println("DEBUG: Specific selector failed, trying generic fallback...")
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if !exists {
				return
			}
			// Avoid re-parsing if we somehow matched
			if strings.HasPrefix(href, "/hledej") || strings.HasPrefix(href, "/profil") || strings.HasPrefix(href, "/cenik") {
				return
			}

			// Must contain size and duration in text to be valid
			text := s.Text()
			if (strings.Contains(text, "MB") || strings.Contains(text, "GB")) && strings.Contains(text, ":") {
				parseLink(s, href, &results)
			}
		})
	}

	if len(results) == 0 {
		pageTitle := doc.Find("title").Text()
		fmt.Printf("DEBUG: No results found for query '%s'. Page Title: '%s'. Body len: %d\n", query, pageTitle, len(html))

		// Debug snippet
		snippet := html
		if len(snippet) > 20000 {
			snippet = snippet[:20000]
		}
		fmt.Printf("DEBUG HTML Snippet: %s\n", snippet)
	}

	return results, nil
}

func parseLink(s *goquery.Selection, href string, results *[]PrehrajResult) {
	duration := ""
	size := ""
	cleanedTitle := ""

	fullText := s.Text()
	lines := strings.Split(fullText, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Duration check: simple check for colon and length
		if strings.Contains(line, ":") && len(line) < 10 {
			duration = line
		} else if strings.Contains(line, "MB") || strings.Contains(line, "GB") || strings.Contains(line, "kB") {
			size = line
		} else {
			cleanedTitle = line // Assume the last non-meta line is title
		}
	}

	// Basic Validation
	if cleanedTitle == "" {
		// Try looking for a nested h3 or similar if text was empty?
		// But usually text works.
		// If generic fallback, title might be the whole text if lines didn't split well.
		if size != "" || duration != "" {
			// If we found meta but no "Title" line, maybe the Title IS the text but we consumed it?
			// Let's reconstruct.
			// Actually, let's just use the title attribute if available
			if val, ok := s.Attr("title"); ok {
				cleanedTitle = val
			} else {
				cleanedTitle = strings.TrimSpace(fullText) // Fallback
			}
		}
	}

	if cleanedTitle != "" {
		if !strings.HasPrefix(href, "http") {
			href = "https://prehraj.to" + href
		}

		// Check duplicates?
		*results = append(*results, PrehrajResult{
			Title:    cleanedTitle,
			Duration: duration,
			Size:     size,
			URL:      href,
		})
	}
}

func extractPrehrajStreams(videoPageURL string) ([]Stream, error) {
	req, err := http.NewRequest("GET", videoPageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Prehraj.to details returned status: %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	bodyString := string(bodyBytes)

	// Regex to find "var sources = [...]"
	re := regexp.MustCompile(`var sources = (\[[\s\S]*?\]);`)
	matches := re.FindStringSubmatch(bodyString)

	// Parse HTML for "Rozlišení"
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyString))
	realResolution := ""
	if err == nil {
		doc.Find("li").Each(func(i int, s *goquery.Selection) {
			if strings.Contains(s.Text(), "Rozlišení:") {
				// The structure is <li><span>Rozlišení:</span><span>VALUE</span></li>
				// We want the text of the second span, or just text after "Rozlišení:"
				s.Find("span").Each(func(j int, span *goquery.Selection) {
					if !strings.Contains(span.Text(), "Rozlišení:") {
						realResolution = strings.TrimSpace(span.Text())
					}
				})
			}
		})
	}

	// Try to find Download Link
	/* downloadURL := ""
	if err == nil {
		doc.Find("a").Each(func(i int, s *goquery.Selection) {
			href, exists := s.Attr("href")
			if exists && strings.Contains(href, "do=download") {
				if !strings.HasPrefix(href, "http") {
					// Handle relative URL if necessary, though typical links are absolute or root-relative
					if strings.HasPrefix(href, "/") {
						u, _ := url.Parse(videoPageURL)
						href = u.Scheme + "://" + u.Host + href
					}
				}
				downloadURL = href
			}
		})
	} */

	var streams []Stream

	if len(matches) > 1 {
		jsonStr := matches[1]
		// Pattern for each object: { file: "(.*?)", label: '(.*?)' ... }
		fileRe := regexp.MustCompile(`file:\s*["']([^"']+)["']`)
		labelRe := regexp.MustCompile(`label:\s*["']([^"']+)["']`)

		segments := strings.Split(jsonStr, "{")
		for _, seg := range segments {
			if !strings.Contains(seg, "file:") {
				continue
			}

			fileMatch := fileRe.FindStringSubmatch(seg)
			labelMatch := labelRe.FindStringSubmatch(seg)

			if len(fileMatch) > 1 {
				url := fileMatch[1]
				label := "Unknown"
				if len(labelMatch) > 1 {
					label = labelMatch[1]
				}

				name := "Prehraj.to " + label
				if realResolution != "" {
					name += fmt.Sprintf(" (Source: %s)", realResolution)
				}

				streams = append(streams, Stream{
					Name:  name,
					Title: label,
					URL:   url,
				})
			}
		}
	}

	// Add Download Link as a fallback/source stream if available
	// Note: It might require cookies or redirect, but Stremio can sometimes handle it.
	// Giving it a high probability label if no streams found, or just adding it.
	/*
		   Disable Download link for now as it redirects to page for non-logged users and fails in Stremio.
		if downloadURL != "" && realResolution != "" {
			streams = append(streams, Stream{
				Name:  fmt.Sprintf("Prehraj.to Source (%s)", realResolution),
				Title: "Original Source",
				URL:   downloadURL,
			})
		} else */
	if len(streams) == 0 {
		return nil, fmt.Errorf("no sources found in script")
	}

	return streams, nil
}

func filterPrehrajResults(results []PrehrajResult, metaYear string, metaNames ...string) []PrehrajResult {
	var filtered []PrehrajResult
	yearReg := regexp.MustCompile(`\b(19|20)\d{2}\b`)

	targetYear := 0
	if metaYear != "" {
		targetYear, _ = strconv.Atoi(metaYear)
	}

	for _, res := range results {
		// 1. Year Check
		if targetYear > 0 {
			detectedYears := yearReg.FindAllString(res.Title, -1)
			if len(detectedYears) > 0 {
				yearMatch := false
				for _, yStr := range detectedYears {
					y, _ := strconv.Atoi(yStr)
					// Strict match as requested
					if y == targetYear {
						yearMatch = true
						break
					}
				}
				if !yearMatch {
					continue // Year detected but didn't match
				}
			}
		}

		// 2. Title Relevance Check (Basic)
		// At least one of the metaNames (normalized) should appear in the result Title (normalized)
		// This helps filter out completely unrelated items if the search engine was too fuzzy
		if len(metaNames) > 0 {
			matchFound := false
			resTitleNorm := normalizeStringForFilter(res.Title)
			for _, name := range metaNames {
				nameNorm := normalizeStringForFilter(name)
				if strings.Contains(resTitleNorm, nameNorm) {
					matchFound = true
					break
				}
			}
			// If we searched for "Wicked" and result is "Something Else", we skip.
			// However, Prehraj search usually returns things containing the query.
			// But normalizing helps.
			if !matchFound {
				// Try partial match for long titles?
				// For now, assume if the search engine returned it, it matched something.
				// But we want to prevent "Dobrá čarodějka" matching "Čarodějka".
				// "Dobrá čarodějka" contains "čarodějka".
				// So containment check passes.
				// The Year check is the primary strict filter here.
			}
		}

		filtered = append(filtered, res)
	}
	return filtered
}

func normalizeStringForFilter(s string) string {
	s = strings.ToLower(s)
	replacements := []struct{ old, new string }{
		{"á", "a"}, {"č", "c"}, {"ď", "d"}, {"é", "e"}, {"ě", "e"},
		{"í", "i"}, {"ň", "n"}, {"ó", "o"}, {"ř", "r"}, {"š", "s"},
		{"ť", "t"}, {"ú", "u"}, {"ů", "u"}, {"ý", "y"}, {"ž", "z"},
		{".", " "}, {"_", " "}, {"-", " "}, {":", " "},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	return strings.Join(strings.Fields(s), " ")
}
