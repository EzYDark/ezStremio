package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds the application configuration
var Config struct {
	TMDBApiKey string
}

// Global cache for localized poster paths to reduce API calls
var posterCache = struct {
	sync.RWMutex
	m map[int]string
}{m: make(map[int]string)}

// HTTP client with timeout
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// Manifest defines the metadata for the Stremio addon.
type Manifest struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Resources   []string  `json:"resources"`
	Types       []string  `json:"types"`
	Catalogs    []Catalog `json:"catalogs"`
	IdPrefixes  []string  `json:"idPrefixes"`
}

type CatalogExtra struct {
	Name       string `json:"name"`
	IsRequired bool   `json:"isRequired,omitempty"`
}

// Catalog defines a content catalog.
type Catalog struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Extra []CatalogExtra `json:"extra,omitempty"`
}

// MetaPreview represents a summary of a content item for the catalog.
type MetaPreview struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Poster      string   `json:"poster"`
	Logo        string   `json:"logo,omitempty"`
	Description string   `json:"description,omitempty"`
	ReleaseInfo string   `json:"releaseInfo,omitempty"`
	ImdbRating  string   `json:"imdbRating,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Cast        []string `json:"cast,omitempty"`
	Director    []string `json:"director,omitempty"`
	Runtime     string   `json:"runtime,omitempty"`
}

// TMDBGenreResponse for parsing genre list
type TMDBGenreResponse struct {
	Genres []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
}

var genreMap = make(map[int]string)

// TMDBResponse structure for decoding TMDB API responses
type TMDBResponse struct {
	Results []struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		Name         string  `json:"name"` // For TV shows
		PosterPath   string  `json:"poster_path"`
		Overview     string  `json:"overview"`
		MediaType    string  `json:"media_type"`
		VoteAverage  float64 `json:"vote_average"`
		ReleaseDate  string  `json:"release_date"`   // movie
		FirstAirDate string  `json:"first_air_date"` // tv
		GenreIDs     []int   `json:"genre_ids"`
	} `json:"results"`
}

type TMDBImage struct {
	FilePath    string  `json:"file_path"`
	ISO639_1    string  `json:"iso_639_1"`
	VoteAverage float64 `json:"vote_average"`
}

type TMDBImagesResponse struct {
	Posters []TMDBImage `json:"posters"`
}

type TMDBSeasonResponse struct {
	Episodes []struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		StillPath     string  `json:"still_path"`
		AirDate       string  `json:"air_date"`
		VoteAverage   float64 `json:"vote_average"`
	} `json:"episodes"`
}

// MetaVideo represents an episode for a series.
type MetaVideo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Released  string `json:"released"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Episode   int    `json:"episode"`
	Season    int    `json:"season"`
	Overview  string `json:"overview,omitempty"`
}

// Meta represents detailed metadata for a content item.
type Meta struct {
	ID           string      `json:"id"`
	Type         string      `json:"type"`
	Name         string      `json:"name"`
	Poster       string      `json:"poster"`
	Background   string      `json:"background,omitempty"`
	Logo         string      `json:"logo,omitempty"`
	Description  string      `json:"description,omitempty"`
	ReleaseInfo  string      `json:"releaseInfo,omitempty"`
	ImdbRating   string      `json:"imdbRating,omitempty"`
	Genres       []string    `json:"genres,omitempty"`
	Cast         []string    `json:"cast,omitempty"`
	Director     []string    `json:"director,omitempty"`
	Runtime      string      `json:"runtime,omitempty"`
	Videos       []MetaVideo `json:"videos,omitempty"`
	OriginalName string      `json:"-"` // Internal use for search
	Year         string      `json:"-"` // Internal use for search
}

// TMDBDetail structure for decoding TMDB API detail responses
type TMDBDetail struct {
	ID             int     `json:"id"`
	Title          string  `json:"title"`
	Name           string  `json:"name"` // For TV shows
	OriginalTitle  string  `json:"original_title"`
	OriginalName   string  `json:"original_name"`
	PosterPath     string  `json:"poster_path"`
	BackdropPath   string  `json:"backdrop_path"`
	Overview       string  `json:"overview"`
	VoteAverage    float64 `json:"vote_average"`
	ReleaseDate    string  `json:"release_date"`     // movie
	FirstAirDate   string  `json:"first_air_date"`   // tv
	LastAirDate    string  `json:"last_air_date"`    // tv
	Runtime        int     `json:"runtime"`          // movie
	EpisodeRunTime []int   `json:"episode_run_time"` // tv
	Genres         []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		EpisodeCount int    `json:"episode_count"`
		Name         string `json:"name"`
	} `json:"seasons"`
	Credits struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
	Images struct {
		Posters []TMDBImage `json:"posters"`
		Logos   []TMDBImage `json:"logos"`
	} `json:"images"`
}

var manifest = Manifest{
	ID:          "org.ezstremio.addon",
	Version:     "0.1.1",
	Name:        "ezStremio",
	Description: "Czech/Slovak dubbed films and TV shows",
	Resources:   []string{"catalog", "stream", "meta"},
	Types:       []string{"movie", "series"},
	Catalogs: []Catalog{
		{
			Type: "movie",
			ID:   "tmdb_movies_cs",
			Name: "CZ/SK Movies (TMDB)",
			Extra: []CatalogExtra{
				{Name: "search"},
				{Name: "skip"},
			},
		},
		{
			Type: "series",
			ID:   "tmdb_series_cs",
			Name: "CZ/SK Series (TMDB)",
			Extra: []CatalogExtra{
				{Name: "search"},
				{Name: "skip"},
			},
		},
	},
	IdPrefixes: []string{"eztmdb:"},
}

func loadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		// .env file is optional, we might be running in an environment where vars are already set
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Only set if not already set
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
}

func main() {
	loadEnv()
	InitBrowser()
	Config.TMDBApiKey = os.Getenv("TMDB_API_KEY")
	if Config.TMDBApiKey == "" {
		log.Println("Warning: TMDB_API_KEY environment variable not set. Catalog will fail.")
	} else {
		loadGenres()
	}

	http.HandleFunc("/manifest.json", handleManifest)
	http.HandleFunc("/catalog/", handleCatalog)
	http.HandleFunc("/meta/", handleMeta)
	http.HandleFunc("/stream/", handleStream)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("Addon active on http://localhost%s/manifest.json", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func handleManifest(w http.ResponseWriter, r *http.Request) {
	log.Println("Handling Manifest request")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
}

func handleCatalog(w http.ResponseWriter, r *http.Request) {
	log.Printf("Catalog Request: %s", r.URL.Path)
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}

	catType := parts[2]
	catID := parts[3]
	if strings.HasSuffix(catID, ".json") {
		catID = strings.TrimSuffix(catID, ".json")
	}

	page := 1
	query := ""

	if len(parts) > 4 {
		for _, part := range parts[4:] {
			if strings.HasSuffix(part, ".json") {
				part = strings.TrimSuffix(part, ".json")
			}
			if strings.HasPrefix(part, "skip=") {
				if skip, err := strconv.Atoi(strings.TrimPrefix(part, "skip=")); err == nil {
					page = (skip / 20) + 1
				}
			} else if strings.HasPrefix(part, "search=") {
				query = strings.TrimPrefix(part, "search=")
				log.Printf("Search query detected: %s", query)
			}
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if strings.HasPrefix(catID, "tmdb_") {
		items, err := fetchTMDBItems(catType, page, query)
		if err != nil {
			log.Printf("Error fetching TMDB items: %v", err)
			json.NewEncoder(w).Encode(map[string]interface{}{"metas": []interface{}{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"metas": items})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"metas": []interface{}{}})
}

func handleMeta(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}

	metaType := parts[2]
	metaID := parts[3]
	if strings.HasSuffix(metaID, ".json") {
		metaID = strings.TrimSuffix(metaID, ".json")
	}

	log.Printf("Handling Meta request for Type: %s, ID: %s", metaType, metaID)

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if strings.HasPrefix(metaID, "eztmdb:") {
		tmdbID := strings.TrimPrefix(metaID, "eztmdb:")
		meta, err := fetchTMDBMeta(metaType, tmdbID)
		if err != nil {
			log.Printf("Error fetching TMDB meta: %v", err)
			json.NewEncoder(w).Encode(map[string]interface{}{"meta": nil})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"meta": meta})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"meta": nil})
}

func fetchTMDBMeta(metaType, tmdbID string) (*Meta, error) {
	if Config.TMDBApiKey == "" {
		return nil, fmt.Errorf("TMDB API Key missing")
	}

	tmdbType := "movie"
	if metaType == "series" {
		tmdbType = "tv"
	}

	// Fetch Details with credits and images
	url := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=cs-CZ&append_to_response=credits,images&include_image_language=cs,sk,en,null", tmdbType, tmdbID, Config.TMDBApiKey)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB returned status: %s", resp.Status)
	}

	var detail TMDBDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}

	// Resolve poster (CS > SK > Default)
	poster := ""
	if detail.PosterPath != "" {
		poster = "https://image.tmdb.org/t/p/w500" + detail.PosterPath
	}
	if len(detail.Images.Posters) > 0 {
		found := false
		for _, img := range detail.Images.Posters {
			if img.ISO639_1 == "cs" {
				poster = "https://image.tmdb.org/t/p/w500" + img.FilePath
				found = true
				break
			}
		}
		if !found {
			for _, img := range detail.Images.Posters {
				if img.ISO639_1 == "sk" {
					poster = "https://image.tmdb.org/t/p/w500" + img.FilePath
					found = true
					break
				}
			}
		}
	}

	// Resolve Logo (CS > SK > EN > NULL > First)
	logo := ""
	if len(detail.Images.Logos) > 0 {
		var finalLogo string
		// Try CS
		for _, img := range detail.Images.Logos {
			if img.ISO639_1 == "cs" {
				finalLogo = img.FilePath
				break
			}
		}
		// Try SK
		if finalLogo == "" {
			for _, img := range detail.Images.Logos {
				if img.ISO639_1 == "sk" {
					finalLogo = img.FilePath
					break
				}
			}
		}
		// Try EN
		if finalLogo == "" {
			for _, img := range detail.Images.Logos {
				if img.ISO639_1 == "en" {
					finalLogo = img.FilePath
					break
				}
			}
		}
		// Try Null/Textless
		if finalLogo == "" {
			for _, img := range detail.Images.Logos {
				if img.ISO639_1 == "null" || img.ISO639_1 == "" {
					finalLogo = img.FilePath
					break
				}
			}
		}
		// Fallback
		if finalLogo == "" && len(detail.Images.Logos) > 0 {
			finalLogo = detail.Images.Logos[0].FilePath
		}

		if finalLogo != "" {
			logo = "https://image.tmdb.org/t/p/w500" + finalLogo
		}
	}

	background := ""
	if detail.BackdropPath != "" {
		background = "https://image.tmdb.org/t/p/original" + detail.BackdropPath
	}

	title := detail.Title
	originalName := detail.OriginalTitle
	year := ""
	if tmdbType == "tv" {
		title = detail.Name
		originalName = detail.OriginalName
	}

	// Genres
	var genres []string
	for _, g := range detail.Genres {
		genres = append(genres, g.Name)
	}

	// Cast (limit to top 10)
	var cast []string
	for i, c := range detail.Credits.Cast {
		if i >= 10 {
			break
		}
		cast = append(cast, c.Name)
	}

	// Directors
	var directors []string
	for _, c := range detail.Credits.Crew {
		if c.Job == "Director" {
			directors = append(directors, c.Name)
		}
	}

	// Release Info & Runtime
	releaseInfo := ""
	runtime := ""
	if tmdbType == "movie" {
		if len(detail.ReleaseDate) >= 4 {
			releaseInfo = detail.ReleaseDate[:4]
			year = releaseInfo
		}
		if detail.Runtime > 0 {
			runtime = fmt.Sprintf("%d min", detail.Runtime)
		}
	} else {
		start := ""
		if len(detail.FirstAirDate) >= 4 {
			start = detail.FirstAirDate[:4]
			year = start
		}
		end := ""
		if len(detail.LastAirDate) >= 4 {
			end = detail.LastAirDate[:4]
		}
		if start != "" {
			if end != "" && end != start {
				releaseInfo = fmt.Sprintf("%s-%s", start, end)
			} else {
				releaseInfo = fmt.Sprintf("%s-", start)
			}
		}
		if len(detail.EpisodeRunTime) > 0 {
			runtime = fmt.Sprintf("%d min", detail.EpisodeRunTime[0])
		}
	}

	// Fetch Episodes (Videos) for TV Series
	var videos []MetaVideo
	if tmdbType == "tv" && len(detail.Seasons) > 0 {
		var wgV sync.WaitGroup
		var muV sync.Mutex

		// Iterate over seasons
		for _, s := range detail.Seasons {
			if s.SeasonNumber == 0 {
				continue
			} // Skip specials if desired, or keep them. Usually 0 is specials. Stremio handles them ok.

			wgV.Add(1)
			go func(seasonNum int) {
				defer wgV.Done()

				sUrl := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s/season/%d?api_key=%s&language=cs-CZ", tmdbID, seasonNum, Config.TMDBApiKey)
				if sResp, err := httpClient.Get(sUrl); err == nil {
					defer sResp.Body.Close()
					if sResp.StatusCode == http.StatusOK {
						var seasonResp TMDBSeasonResponse
						if err := json.NewDecoder(sResp.Body).Decode(&seasonResp); err == nil {
							muV.Lock()
							for _, ep := range seasonResp.Episodes {

								// Episode Thumbnail
								thumb := ""
								if ep.StillPath != "" {
									thumb = "https://image.tmdb.org/t/p/w500" + ep.StillPath
								} else if background != "" {
									thumb = background // Fallback to show background
								}

								// Release Date for Episode
								released := ep.AirDate
								if len(released) >= 10 {
									t, _ := time.Parse("2006-01-02", released)
									released = t.Format(time.RFC3339)
								}

								videos = append(videos, MetaVideo{
									ID:        fmt.Sprintf("eztmdb:%s:%d:%d", tmdbID, seasonNum, ep.EpisodeNumber),
									Title:     ep.Name,
									Released:  released,
									Thumbnail: thumb,
									Episode:   ep.EpisodeNumber,
									Season:    seasonNum,
									Overview:  ep.Overview,
								})
							}
							muV.Unlock()
						}
					}
				}
			}(s.SeasonNumber)
		}
		wgV.Wait()
	}

	return &Meta{
		ID:           "eztmdb:" + tmdbID,
		Type:         metaType,
		Name:         title,
		Poster:       poster,
		Logo:         logo,
		Background:   background,
		Description:  detail.Overview,
		ReleaseInfo:  releaseInfo,
		ImdbRating:   fmt.Sprintf("%.1f", detail.VoteAverage),
		Genres:       genres,
		Cast:         cast,
		Director:     directors,
		Runtime:      runtime,
		Videos:       videos,
		OriginalName: originalName,
		Year:         year,
	}, nil
}

func normalizeString(s string) string {
	// Simple mapping for CZ/SK diacritics and common separators
	replacements := []struct{ old, new string }{
		{"á", "a"}, {"č", "c"}, {"ď", "d"}, {"é", "e"}, {"ě", "e"},
		{"í", "i"}, {"ň", "n"}, {"ó", "o"}, {"ř", "r"}, {"š", "s"},
		{"ť", "t"}, {"ú", "u"}, {"ů", "u"}, {"ý", "y"}, {"ž", "z"},
		{"Á", "A"}, {"Č", "C"}, {"Ď", "D"}, {"É", "E"}, {"Ě", "E"},
		{"Í", "I"}, {"Ň", "N"}, {"Ó", "O"}, {"Ř", "R"}, {"Š", "S"},
		{"Ť", "T"}, {"Ú", "U"}, {"Ů", "U"}, {"Ý", "Y"}, {"Ž", "Z"},
		{":", " "}, {"-", " "}, {".", " "},
	}
	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}
	// Remove double spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// Stream represents a stream source.
type Stream struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}

	streamType := parts[2]
	streamID := parts[3]
	if strings.HasSuffix(streamID, ".json") {
		streamID = strings.TrimSuffix(streamID, ".json")
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	log.Printf("Handling Stream request for Type: %s, ID: %s", streamType, streamID)

	// We only support eztmdb prefixes for now
	if !strings.HasPrefix(streamID, "eztmdb:") {
		json.NewEncoder(w).Encode(map[string]interface{}{"streams": []Stream{}})
		return
	}

	// Parsing ID to get TMDB ID
	// eztmdb:123
	// eztmdb:123:1:1
	idParts := strings.Split(streamID, ":")
	if len(idParts) < 2 {
		json.NewEncoder(w).Encode(map[string]interface{}{"streams": []Stream{}})
		return
	}
	tmdbID := idParts[1]

	// Determine if it's a series
	season := ""
	episode := ""
	if len(idParts) >= 4 {
		season = idParts[2]
		episode = idParts[3]
	}

	// Fetch Meta to get the Title
	meta, err := fetchTMDBMeta(streamType, tmdbID)
	if err != nil || meta == nil {
		log.Printf("Failed to fetch meta for title: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{"streams": []Stream{}})
		return
	}

	// Generate search queries
	var queries []string

	// Helper to add query variations
	addVariations := func(name string) {
		// 1. Exact Name
		queries = append(queries, name)

		// 2. Normalized Name (diacritics removed, punctuation to space)
		norm := normalizeString(name)
		if norm != name {
			queries = append(queries, norm)
		}

		// 3. Name with punctuation replaced by space (if distinct from normalized)
		// (normalizeString already does this, but let's ensure we handle ":" split separately if needed)
		if strings.Contains(name, ":") {
			replacedColon := strings.ReplaceAll(name, ":", " ")
			queries = append(queries, replacedColon)
		}
	}

	// Add variations for Localized Name and Original Name
	addVariations(meta.Name)
	if meta.OriginalName != "" && meta.OriginalName != meta.Name {
		addVariations(meta.OriginalName)
	}

	// Process Years

	years := []string{}

	if meta.Year != "" {

		years = append(years, meta.Year)

	}

	// Combine Name Queries with Years and Season/Episode

	var finalQueries []string

	for _, q := range queries {

		// Base query (Name only) - only if it's unique enough?

		// Searching just "Wicked" might return too much, but Prehraj might handle it.

		// Let's include it.

		suffix := ""

		if season != "" && episode != "" {

			sInt, _ := strconv.Atoi(season)

			eInt, _ := strconv.Atoi(episode)

			suffix = fmt.Sprintf(" S%02dE%02d", sInt, eInt)

		}

		// Query with suffix (S01E01)

		finalQueries = append(finalQueries, q+suffix)

		// Query with Year + Suffix (for movies, suffix is empty)

		for _, y := range years {

			finalQueries = append(finalQueries, fmt.Sprintf("%s %s%s", q, y, suffix))

		}

	}

	// Deduplicate queries

	uniqueQueries := make(map[string]bool)

	var dedupedQueries []string

	for _, q := range finalQueries {

		q = strings.TrimSpace(q) // clean up

		if _, exists := uniqueQueries[q]; !exists && q != "" {

			uniqueQueries[q] = true

			dedupedQueries = append(dedupedQueries, q)

		}

	}

	log.Printf("Searching Prehraj.to with queries: [%s]", strings.Join(dedupedQueries, ", "))

	// Collect results from all queries

	var allResults []PrehrajResult

	var resMu sync.Mutex

	var wgSearch sync.WaitGroup

	// Search concurrency limit to avoid being blocked and save resources
	// When using Headless Chrome, we must limit this to 1 to prevent spawning multiple browsers
	sem := make(chan struct{}, 1)

	for _, q := range dedupedQueries {

		wgSearch.Add(1)

		go func(query string) {

			defer wgSearch.Done()

			sem <- struct{}{} // Acquire

			defer func() { <-sem }() // Release

			results, err := searchPrehraj(query)

			if err == nil {

				resMu.Lock()

				allResults = append(allResults, results...)

				resMu.Unlock()

			} else {

				log.Printf("Error searching %s: %v", query, err)

			}

		}(q)

	}

	wgSearch.Wait()

	// Filter results based on year and titles

	// We pass meta.Name and meta.OriginalName for relevance checking

	names := []string{meta.Name}

	if meta.OriginalName != "" && meta.OriginalName != meta.Name {

		names = append(names, meta.OriginalName)

	}

	filteredResults := filterPrehrajResults(allResults, meta.Year, names...)

	// Deduplicate results by URL

	uniqueResults := make(map[string]PrehrajResult)

	var orderedUniqueResults []PrehrajResult // To keep some order

	for _, res := range filteredResults {

		if _, exists := uniqueResults[res.URL]; !exists {

			uniqueResults[res.URL] = res

			orderedUniqueResults = append(orderedUniqueResults, res)

		}

	}

	log.Printf("Found %d unique results", len(orderedUniqueResults))

	var streams []Stream
	var wgExtract sync.WaitGroup
	var streamMu sync.Mutex

	// Limit extraction to top 25 unique results
	limit := 25
	if len(orderedUniqueResults) < limit {
		limit = len(orderedUniqueResults)
	}

	// Extraction concurrency limit
	semExtract := make(chan struct{}, 5)

	for i := 0; i < limit; i++ {
		wgExtract.Add(1)
		go func(res PrehrajResult) {
			defer wgExtract.Done()
			semExtract <- struct{}{}
			defer func() { <-semExtract }()

			extracted, err := extractPrehrajStreams(res.URL)
			if err == nil && len(extracted) > 0 {
				streamMu.Lock()
				for _, s := range extracted {
					// Parse Source Resolution from s.Name if present
					sourceRes := ""
					if strings.Contains(s.Name, "Source:") {
						parts := strings.Split(s.Name, "Source:")
						if len(parts) > 1 {
							sourceRes = strings.TrimSuffix(strings.TrimSpace(parts[1]), ")")
						}
					}

					// Clean up label (s.Title currently holds the label e.g. "1080p")
					label := s.Title

					// Format Name (Header)
					s.Name = fmt.Sprintf("Prehraj.to ⚡ %s", label)

					// Format Description (Title)
					description := fmt.Sprintf("📂 %s\n💾 %s • ⏱️ %s", res.Title, res.Size, res.Duration)
					if sourceRes != "" {
						// Clean up source resolution for display (e.g. "3840 x 2160 px" -> "4K")
						displaySource := sourceRes
						if strings.Contains(sourceRes, "3840") || strings.Contains(sourceRes, "2160") {
							displaySource = "4K"
						} else if strings.Contains(sourceRes, "1920") || strings.Contains(sourceRes, "1080") {
							displaySource = "1080p"
						}
						description += fmt.Sprintf("\n⚙️ Source: %s", displaySource)
					}
					s.Title = description

					streams = append(streams, s)
				}
				streamMu.Unlock()
			}
		}(orderedUniqueResults[i])
	}

	wgExtract.Wait()

	// Sorting logic
	// Criteria: Source Resolution > Stream Resolution > Size > Filename contains Year

	// Pre-compile regexes
	// Source is now in Title: "⚙️ Source: 4K" or "Source: 3840 x 2160 px"
	reSourceRes4K := regexp.MustCompile(`Source:\s*4K`)
	reSourceRes1080 := regexp.MustCompile(`Source:\s*1080p`)
	reSourceResRaw := regexp.MustCompile(`Source:.*x\s*(\d+)`)

	// Stream Res in Name: "Prehraj ⚡ 1080p"
	reStreamRes := regexp.MustCompile(`⚡\s+(\d{3,4})p`)

	// Size in Title: "💾 56.37 GB"
	reSize := regexp.MustCompile(`💾\s*(\d+(?:\.\d+)?)\s*(GB|MB|kB)`)

	// Helper to get int resolution
	getRes := func(name string, title string) int {
		// Check Source in Title first
		if reSourceRes4K.MatchString(title) {
			return 2160
		}
		if reSourceRes1080.MatchString(title) {
			return 1080
		}
		matches := reSourceResRaw.FindStringSubmatch(title)
		if len(matches) > 1 {
			if val, err := strconv.Atoi(matches[1]); err == nil {
				return val
			}
		}
		return 0
	}

	getStreamRes := func(name string) int {
		matches := reStreamRes.FindStringSubmatch(name)
		if len(matches) > 1 {
			if val, err := strconv.Atoi(matches[1]); err == nil {
				return val
			}
		}
		return 0
	}

	// Helper to get size in MB
	getSize := func(title string) float64 {
		matches := reSize.FindStringSubmatch(title)
		if len(matches) > 2 {
			val, _ := strconv.ParseFloat(matches[1], 64)
			unit := matches[2]
			switch unit {
			case "GB":
				return val * 1024
			case "MB":
				return val
			case "kB":
				return val / 1024
			}
		}
		return 0
	}

	metaYear := meta.Year

	sort.Slice(streams, func(i, j int) bool {
		// 1. Source Resolution
		srcResI := getRes(streams[i].Name, streams[i].Title)
		srcResJ := getRes(streams[j].Name, streams[j].Title)
		if srcResI != srcResJ {
			return srcResI > srcResJ
		}

		// 2. Stream Resolution
		strmResI := getStreamRes(streams[i].Name)
		strmResJ := getStreamRes(streams[j].Name)
		if strmResI != strmResJ {
			return strmResI > strmResJ
		}

		// 3. Size
		sizeI := getSize(streams[i].Title)
		sizeJ := getSize(streams[j].Title)
		if sizeI != sizeJ {
			return sizeI > sizeJ
		}

		// 4. Filename contains Year
		hasYearI := strings.Contains(streams[i].Title, metaYear)
		hasYearJ := strings.Contains(streams[j].Title, metaYear)
		if hasYearI != hasYearJ {
			return hasYearI
		}

		return false
	})
	json.NewEncoder(w).Encode(map[string]interface{}{"streams": streams})
}

func loadGenres() {
	types := []string{"movie", "tv"}
	for _, t := range types {
		url := fmt.Sprintf("https://api.themoviedb.org/3/genre/%s/list?api_key=%s&language=cs-CZ", t, Config.TMDBApiKey)
		resp, err := httpClient.Get(url)
		if err != nil {
			log.Printf("Failed to fetch genres for %s: %v", t, err)
			continue
		}
		defer resp.Body.Close()

		var genreResp TMDBGenreResponse
		if err := json.NewDecoder(resp.Body).Decode(&genreResp); err == nil {
			for _, g := range genreResp.Genres {
				genreMap[g.ID] = g.Name
			}
		}
	}
	log.Printf("Loaded %d genres", len(genreMap))
}

func fetchTMDBItems(catType string, page int, query string) ([]MetaPreview, error) {
	if Config.TMDBApiKey == "" {
		return nil, fmt.Errorf("TMDB API Key missing")
	}

	tmdbType := "movie"
	if catType == "series" {
		tmdbType = "tv"
	}

	// Fetch the list of items
	apiURL := ""
	if query != "" {
		log.Printf("Fetching TMDB items with search query: %s", query)
		encodedQuery := url.QueryEscape(query)
		apiURL = fmt.Sprintf("https://api.themoviedb.org/3/search/%s?api_key=%s&language=cs-CZ&query=%s&page=%d&include_adult=false", tmdbType, Config.TMDBApiKey, encodedQuery, page)
	} else {
		log.Printf("Fetching TMDB items via discover for page %d", page)
		apiURL = fmt.Sprintf("https://api.themoviedb.org/3/discover/%s?api_key=%s&language=cs-CZ&sort_by=popularity.desc&include_adult=false&page=%d", tmdbType, Config.TMDBApiKey, page)
	}

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB returned status: %s", resp.Status)
	}

	var tmdbResp TMDBResponse
	if err := json.NewDecoder(resp.Body).Decode(&tmdbResp); err != nil {
		return nil, err
	}

	results := tmdbResp.Results
	metas := make([]MetaPreview, len(results))
	var wg sync.WaitGroup

	for i, item := range results {
		wg.Add(1)
		go func(i int, itemID int, initialTitle, initialName, initialOverview, initialPoster string, initialVote float64, initialRelease, initialFirstAir string, initialGenreIDs []int) {
			defer wg.Done()

			// Default values from discover response
			finalPosterPath := initialPoster
			finalOverview := initialOverview
			finalTitle := initialTitle
			if tmdbType == "tv" {
				finalTitle = initialName
			}

			var finalLogo string
			var finalRuntime string
			var finalCast []string
			var finalDirectors []string

			// Fetch Details (Credits, Images)
			// We check cache for POSTER only? Or should we just fetch details?
			// Since we need credits/runtime/logo, we MUST fetch details.
			// We can still use cache to skip processing if we really wanted to, but we want freshness.
			// We'll just fetch.

			detailUrl := fmt.Sprintf("https://api.themoviedb.org/3/%s/%d?api_key=%s&language=cs-CZ&append_to_response=credits,images&include_image_language=cs,sk,en,null", tmdbType, itemID, Config.TMDBApiKey)
			if detailResp, err := httpClient.Get(detailUrl); err == nil {
				defer detailResp.Body.Close()
				if detailResp.StatusCode == http.StatusOK {
					var detail TMDBDetail
					if err := json.NewDecoder(detailResp.Body).Decode(&detail); err == nil {
						// 1. Poster: CS > SK > Default
						posterFound := false
						if len(detail.Images.Posters) > 0 {
							for _, img := range detail.Images.Posters {
								if img.ISO639_1 == "cs" {
									finalPosterPath = img.FilePath
									posterFound = true
									break
								}
							}
							if !posterFound {
								for _, img := range detail.Images.Posters {
									if img.ISO639_1 == "sk" {
										finalPosterPath = img.FilePath
										posterFound = true
										break
									}
								}
							}
						}

						// 2. Logo: CS > SK > EN > NULL > First
						if len(detail.Images.Logos) > 0 {
							// Try CS
							for _, img := range detail.Images.Logos {
								if img.ISO639_1 == "cs" {
									finalLogo = img.FilePath
									break
								}
							}
							// Try SK
							if finalLogo == "" {
								for _, img := range detail.Images.Logos {
									if img.ISO639_1 == "sk" {
										finalLogo = img.FilePath
										break
									}
								}
							}
							// Try EN
							if finalLogo == "" {
								for _, img := range detail.Images.Logos {
									if img.ISO639_1 == "en" {
										finalLogo = img.FilePath
										break
									}
								}
							}
							// Try Null/Textless
							if finalLogo == "" {
								for _, img := range detail.Images.Logos {
									if img.ISO639_1 == "null" || img.ISO639_1 == "" {
										finalLogo = img.FilePath
										break
									}
								}
							}
							// Fallback to first
							if finalLogo == "" && len(detail.Images.Logos) > 0 {
								finalLogo = detail.Images.Logos[0].FilePath
							}
						}

						// 3. Runtime
						if tmdbType == "movie" && detail.Runtime > 0 {
							finalRuntime = fmt.Sprintf("%d min", detail.Runtime)
						} else if tmdbType == "tv" && len(detail.EpisodeRunTime) > 0 {
							finalRuntime = fmt.Sprintf("%d min", detail.EpisodeRunTime[0])
						}

						// 4. Cast (Top 3)
						for j, c := range detail.Credits.Cast {
							if j >= 3 {
								break
							}
							finalCast = append(finalCast, c.Name)
						}

						// 5. Director
						for _, c := range detail.Credits.Crew {
							if c.Job == "Director" {
								finalDirectors = append(finalDirectors, c.Name)
							}
						}
					}
				}
			}

			poster := ""
			if finalPosterPath != "" {
				poster = "https://image.tmdb.org/t/p/w500" + finalPosterPath
			}

			logo := ""
			if finalLogo != "" {
				logo = "https://image.tmdb.org/t/p/w500" + finalLogo
			}

			// Genres
			var genres []string
			for _, gid := range initialGenreIDs {
				if name, ok := genreMap[gid]; ok {
					genres = append(genres, name)
				}
			}

			// Release Info
			releaseInfo := ""
			if tmdbType == "movie" {
				if len(initialRelease) >= 4 {
					releaseInfo = initialRelease[:4]
				}
			} else {
				if len(initialFirstAir) >= 4 {
					releaseInfo = initialFirstAir[:4] + "-"
				}
			}

			metas[i] = MetaPreview{
				ID:          "eztmdb:" + strconv.Itoa(itemID),
				Type:        catType,
				Name:        finalTitle,
				Poster:      poster,
				Logo:        logo,
				Description: finalOverview,
				Genres:      genres,
				ReleaseInfo: releaseInfo,
				ImdbRating:  fmt.Sprintf("%.1f", initialVote),
				Cast:        finalCast,
				Director:    finalDirectors,
				Runtime:     finalRuntime,
			}
		}(i, item.ID, item.Title, item.Name, item.Overview, item.PosterPath, item.VoteAverage, item.ReleaseDate, item.FirstAirDate, item.GenreIDs)
	}

	wg.Wait()
	return metas, nil
}
