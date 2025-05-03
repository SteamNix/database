package main

import (
    "bufio"
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "html"
    "io"
    "io/fs"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "time"

    "github.com/PuerkitoBio/goquery"
    "github.com/lithammer/fuzzysearch/fuzzy"
)

type SteamApp struct {
    AppID int    `json:"appid"`
    Name  string `json:"name"`
}

type AppListResponse struct {
    Applist struct {
        Apps []SteamApp `json:"apps"`
    } `json:"applist"`
}

type Entry struct {
    InfoHash string
    AppID    int
}

func main() {
    rootDir := "./"

    // 1. Fetch full AppList for fuzzy candidates
    resp, err := http.Get("https://api.steampowered.com/ISteamApps/GetAppList/v2/")
    if err != nil {
        log.Fatalf("failed to fetch AppList: %v", err)
    }
    defer resp.Body.Close()

    var fullList AppListResponse
    if err := json.NewDecoder(resp.Body).Decode(&fullList); err != nil {
        log.Fatalf("failed to decode AppList JSON: %v", err)
    }

    // prepare fuzzy matching data
    candidates, nameToID := buildFuzzyCandidates(fullList.Applist.Apps)
    hashRe := regexp.MustCompile(`xt=urn:btih:([A-Fa-f0-9]+)`)

    var entries []Entry

    // 2. Walk game folders
    filepath.Walk(rootDir, func(path string, info fs.FileInfo, err error) error {
        if err != nil || (info.IsDir() && path == rootDir) {
            return err
        }
        if info.IsDir() {
            raw := info.Name()
            normalized := strings.NewReplacer("-", " ", "_", " ").Replace(raw)

            // extract torrent hash
            hash, err := extractInfoHash(path, hashRe)
            if err != nil {
                log.Printf("no hash in %q; skipping", raw)
                return filepath.SkipDir
            }

            // fuzzy-match to get candidates
            matches := fuzzy.RankFindFold(normalized, candidates)
            var chosenAppID int
            for _, m := range matches {
                id := nameToID[m.Target]
                ok, err := isGameApp(id)
                if err != nil {
                    log.Printf("error checking AppID %d: %v", id, err)
                    continue
                }
                if ok {
                    chosenAppID = id
                    break
                }
            }
            if chosenAppID == 0 {
                log.Printf("no valid game match for %q; skipping", raw)
                return filepath.SkipDir
            }

            log.Printf("Matched %q → AppID %d", raw, chosenAppID)
            entries = append(entries, Entry{InfoHash: hash, AppID: chosenAppID})
            return filepath.SkipDir
        }
        return nil
    })

    // 3. Write mapping.toml
    var buf bytes.Buffer
    buf.WriteString("[hashes]\n")
    for _, e := range entries {
        buf.WriteString(fmt.Sprintf("%s = \"%d\"\n", e.InfoHash, e.AppID))
    }
    if err := os.WriteFile("mapping.toml", buf.Bytes(), 0644); err != nil {
        log.Fatalf("failed to write mapping.toml: %v", err)
    }
    log.Printf("Wrote %d entries to mapping.toml", len(entries))
}

func extractInfoHash(dir string, re *regexp.Regexp) (string, error) {
    var found string
    filepath.Walk(dir, func(p string, info fs.FileInfo, err error) error {
        if err != nil || info.IsDir() || filepath.Ext(p) != ".html" {
            return err
        }
        f, err := os.Open(p)
        if err != nil {
            return err
        }
        defer f.Close()
        doc, err := goquery.NewDocumentFromReader(f)
        if err != nil {
            return err
        }
        doc.Find(`a[href^="magnet:?xt=urn:btih:"]`).EachWithBreak(func(i int, s *goquery.Selection) bool {
            raw, _ := s.Attr("href")
            href := html.UnescapeString(raw)
            if m := re.FindStringSubmatch(href); len(m) == 2 {
                found = m[1]
                return false
            }
            return true
        })
        if found != "" {
            return io.EOF
        }
        return nil
    })
    if found == "" {
        return "", errors.New("no magnet BTIH found")
    }
    return found, nil
}

func buildFuzzyCandidates(apps []SteamApp) ([]string, map[string]int) {
    names := make([]string, len(apps))
    nameToID := make(map[string]int, len(apps))
    for i, app := range apps {
        names[i] = app.Name
        nameToID[app.Name] = app.AppID
    }
    return names, nameToID
}

// isGameApp uses Storefront API with locale/region params plus a browser UA
func isGameApp(appID int) (bool, error) {
    client := &http.Client{Timeout: 5 * time.Second}

    // include locale (l) and country (cc) to force JSON over HTML :contentReference[oaicite:0]{index=0}
    url := fmt.Sprintf(
        "https://store.steampowered.com/api/appdetails?appids=%d&l=english&cc=US",
        appID,
    )

    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return false, err
    }
    // set a realistic browser User-Agent to avoid bot blocking :contentReference[oaicite:1]{index=1}
    req.Header.Set("User-Agent",
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36",
    )

    resp, err := client.Do(req)
    if err != nil {
        return false, err
    }
    defer resp.Body.Close()

    // ensure JSON content type
    ct := resp.Header.Get("Content-Type")
    if !strings.HasPrefix(ct, "application/json") {
        return false, fmt.Errorf("non-JSON response (Content-Type=%q)", ct)
    }

    // peek to avoid HTML masquerading as JSON
    buf := bufio.NewReader(resp.Body)
    first, err := buf.Peek(1)
    if err != nil {
        return false, err
    }
    if first[0] == '<' {
        return false, fmt.Errorf("response looks like HTML for AppID %d", appID)
    }

    // decode and check type == "game"
    var result map[string]struct {
        Success bool `json:"success"`
        Data    struct {
            Type string `json:"type"`
        } `json:"data"`
    }
    if err := json.NewDecoder(buf).Decode(&result); err != nil {
        return false, err
    }
    entry, ok := result[fmt.Sprintf("%d", appID)]
    if !ok || !entry.Success {
        return false, nil
    }
    return entry.Data.Type == "game", nil
}

