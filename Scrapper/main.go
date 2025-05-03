package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/PuerkitoBio/goquery"
)

// Configuration variables
var (
	maxPages  = 600  // Total number of pages to scrape (change as needed)
	batchSize = 10  // Number of pages to process per batch
)

func main() {
	// Map to store unique info hashes and their magnet links
	hashMap := make(map[string]string)

	// Calculate total batches
	totalBatches := (maxPages + batchSize - 1) / batchSize

	for batch := 0; batch < totalBatches; batch++ {
		startPage := batch*batchSize + 1
		endPage := (batch + 1) * batchSize
		if endPage > maxPages {
			endPage = maxPages
		}

		for page := startPage; page <= endPage; page++ {
			url := pageURL(page)
			doc, err := fetchDocument(url)
			if err != nil {
				log.Printf("Error fetching page %d: %v", page, err)
				continue
			}

			// Find all magnet links
			doc.Find("a[href^='magnet:']").Each(func(i int, s *goquery.Selection) {
				href, exists := s.Attr("href")
				if !exists {
					return
				}
				// Extract info hash from magnet link
				hash := extractInfoHash(href)
				if hash != "" {
					hashMap[hash] = href
				}
			})
		}
	}

	// Write to TOML file
	if err := writeTOML("hashes.toml", hashMap); err != nil {
		log.Fatalf("Error writing TOML: %v", err)
	}
	fmt.Println("hashes.toml generated successfully.")
}

// pageURL returns the URL for a given page number
func pageURL(page int) string {
	if page == 1 {
		return "https://fitgirl-repacks.site/"
	}
	return "https://fitgirl-repacks.site/page/" + strconv.Itoa(page)
}

// fetchDocument retrieves and parses the HTML document from a URL
func fetchDocument(url string) (*goquery.Document, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code error: %d %s", resp.StatusCode, resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// extractInfoHash extracts the BTIH info hash from a magnet link
func extractInfoHash(magnetLink string) string {
	re := regexp.MustCompile(`xt=urn:btih:([A-Fa-f0-9]{40})`)
	matches := re.FindStringSubmatch(magnetLink)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// writeTOML writes the hashMap to a TOML file
func writeTOML(filename string, hashMap map[string]string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write header
	if _, err := f.WriteString("[hashes]\n"); err != nil {
		return err
	}

	// Write each hash = "magnet link"
	for hash, magnet := range hashMap {
		line := fmt.Sprintf("%s = \"%s\"\n", hash, magnet)
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}

