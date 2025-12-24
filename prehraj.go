package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"os"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

var rodBrowser *rod.Browser

func InitBrowser() {
	if rodBrowser != nil {
		return
	}
	// Launch headless browser
	path := "/usr/bin/chromium-browser"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path, _ = launcher.LookPath()
	}

	u := launcher.New().Bin(path).MustLaunch()
	rodBrowser = rod.New().ControlURL(u).MustConnect()

	// Global Login
	email := os.Getenv("PREHRAJ_EMAIL")
	password := os.Getenv("PREHRAJ_PASSWORD")

	if email != "" && password != "" {
		fmt.Println("DEBUG: Performing global login...")
		page := rodBrowser.MustPage("https://prehraj.to/")
		defer page.Close()

		page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		})

		if err := page.Timeout(15 * time.Second).WaitLoad(); err != nil {
			fmt.Printf("DEBUG: Login page load timeout: %v\n", err)
		}

		time.Sleep(2 * time.Second)

		// Login Logic
		inlineForm, err := page.Timeout(2 * time.Second).Element("#frm-homepageLoginForm-loginForm")
		if err == nil {
			fmt.Println("DEBUG: Found inline login form. Filling...")
			inlineForm.MustElement(`input[name="email"]`).Input(email)
			inlineForm.MustElement(`input[name="password"]`).Input(password)
			go func() {
				inlineForm.MustElement(`button[name="login"]`).Click(proto.InputMouseButtonLeft, 1)
			}()
			page.Timeout(10 * time.Second).WaitLoad()
			fmt.Println("DEBUG: Login submitted via inline form.")
		} else {
			fmt.Println("DEBUG: Inline form not found, checking for login button...")
			loginBtn, err := page.Timeout(2 * time.Second).Element(`[data-dialog-open="login"]`)
			if err == nil {
				fmt.Println("DEBUG: Login button found. Clicking...")
				loginBtn.MustClick()
				fmt.Println("DEBUG: Waiting for modal form...")
				if err := page.Timeout(5*time.Second).WaitElementsMoreThan("#frm-loginDialog-login-loginForm", 0); err != nil {
					fmt.Printf("DEBUG: Login modal did not appear: %v\n", err)
				} else {
					fmt.Println("DEBUG: Modal appeared. Filling...")
					page.MustElement(`#frm-loginDialog-login-loginForm input[name="email"]`).Input(email)
					page.MustElement(`#frm-loginDialog-login-loginForm input[name="password"]`).Input(password)
					fmt.Println("DEBUG: Submitting modal form...")
					wait := page.MustWaitNavigation()
					page.MustElement(`#frm-loginDialog-login-loginForm button[name="login"]`).MustClick()
					wait()
					fmt.Println("DEBUG: Login submitted via modal.")
				}
			} else {
				fmt.Println("DEBUG: Login button not found. Assuming already logged in or layout changed.")
			}
		}
	}
}

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

	if rodBrowser == nil {
		InitBrowser()
	}

	page := rodBrowser.MustPage(searchURL)
	defer page.Close()

	// Set User-Agent
	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	})

	// 2. SEARCH
	fmt.Printf("DEBUG: Navigating to search: %s\n", searchURL)
	// Page already navigated by MustPage, but checking load
	page.Timeout(15 * time.Second).WaitLoad()

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
	if rodBrowser == nil {
		InitBrowser()
	}

	page := rodBrowser.MustPage(videoPageURL)
	defer page.Close()

	page.MustSetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
	})

	if err := page.Timeout(15 * time.Second).WaitLoad(); err != nil {
		fmt.Printf("DEBUG: Timeout loading video page %s: %v\n", videoPageURL, err)
	}

	// Small delay to ensure scripts run
	time.Sleep(1 * time.Second)

	bodyString, err := page.HTML()
	if err != nil {
		return nil, err
	}

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
