package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type YtdlpInfo struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Uploader string   `json:"uploader"`
	Duration float64  `json:"duration"` // seconds
	Tags     []string `json:"tags"`
	Webpage  string   `json:"webpage_url"`
	// store raw JSON too
}

type Job struct {
	URL string
}

func ensureDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	schema := `CREATE TABLE IF NOT EXISTS tracks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ytdlp_id TEXT UNIQUE,
		url TEXT NOT NULL,
		title TEXT,
		uploader TEXT,
		duration_seconds INTEGER,
		mp3_path TEXT,
		info_json TEXT,
		downloaded_at TEXT DEFAULT (datetime('now')),
		status TEXT DEFAULT 'downloaded',
		error_text TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_tracks_ytdlp_id ON tracks(ytdlp_id);
	CREATE INDEX IF NOT EXISTS idx_tracks_url ON tracks(url);`
	_, err = db.Exec(schema)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// moveFile attempts os.Rename, falls back to copy+remove if needed.
func moveFile(src, dst string) error {
	if src == dst {
		return nil
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// fallback copy
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		// ignore
	}
	if err := in.Close(); err != nil {
		// ignore
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	return nil
}

// callYtDlp downloads audio only into a per-job temporary directory, then moves files to mp3Dir and dataDir.
// Returns ytdlp id and final paths (infoPath, mp3Path).
func callYtDlp(mp3Dir, dataDir, url string) (ytdlpID string, infoPath string, mp3Path string, err error) {
	// create a unique temp dir (system temp) per job to avoid races and cross-filesystem issues.
	tmpDir, err := os.MkdirTemp("", "ytjob-*")
	if err != nil {
		return "", "", "", fmt.Errorf("mkdtemp: %w", err)
	}
	// ensure we cleanup temp dir if anything goes wrong; on success files will be moved out
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	outTpl := filepath.Join(tmpDir, "%(id)s.%(ext)s")

	args := []string{
		"--no-warnings",
		"--format", "bestaudio/best",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "0", // best quality
		"--write-info-json",
		"-o", outTpl,
		url,
	}

	cmd := exec.Command("yt-dlp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", "", fmt.Errorf("yt-dlp failed: %w", err)
	}

	// find .info.json in tmpDir
	infoFiles, err := filepath.Glob(filepath.Join(tmpDir, "*.info.json"))
	if err != nil || len(infoFiles) == 0 {
		// fallback recursive scan
		_ = filepath.WalkDir(tmpDir, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return nil
			}
			if strings.HasSuffix(p, ".info.json") {
				infoFiles = append(infoFiles, p)
			}
			return nil
		})
	}
	if len(infoFiles) == 0 {
		return "", "", "", errors.New("no .info.json produced by yt-dlp")
	}

	// pick newest info.json by modtime (safety)
	var newest string
	var newestMod time.Time
	for _, f := range infoFiles {
		fi, e := os.Stat(f)
		if e != nil {
			continue
		}
		if fi.ModTime().After(newestMod) {
			newestMod = fi.ModTime()
			newest = f
		}
	}
	if newest == "" {
		newest = infoFiles[0]
	}

	// parse ID from info json
	raw, err := os.ReadFile(newest)
	if err != nil {
		return "", "", "", fmt.Errorf("read info json: %w", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", "", fmt.Errorf("parse info json: %w", err)
	}
	idVal, _ := parsed["id"].(string)
	if idVal == "" {
		idVal = strings.TrimSuffix(filepath.Base(newest), ".info.json")
	}

	// tmp file paths
	tmpInfo := newest
	tmpMp3 := filepath.Join(tmpDir, idVal+".mp3")

	// final destinations
	finalInfo := filepath.Join(dataDir, idVal+".info.json")
	finalMp3 := filepath.Join(mp3Dir, idVal+".mp3")

	// ensure final directories exist (caller generally creates them, but double-check)
	if err := os.MkdirAll(filepath.Dir(finalInfo), 0o755); err != nil {
		return "", "", "", fmt.Errorf("mkdir dataDir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(finalMp3), 0o755); err != nil {
		return "", "", "", fmt.Errorf("mkdir mp3Dir: %w", err)
	}

	// move files
	if err := moveFile(tmpInfo, finalInfo); err != nil {
		return "", "", "", fmt.Errorf("move info.json: %w", err)
	}
	if _, err := os.Stat(tmpMp3); err == nil {
		if err := moveFile(tmpMp3, finalMp3); err != nil {
			return "", "", "", fmt.Errorf("move mp3: %w", err)
		}
	} else {
		return idVal, finalInfo, "", errors.New("no mp3 file produced by yt-dlp")
	}

	// cleanup tmp dir
	_ = os.RemoveAll(tmpDir)

	return idVal, finalInfo, finalMp3, nil
}

func parseInfoJSON(infoPath string) (YtdlpInfo, string, error) {
	var info YtdlpInfo
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		return info, "", err
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return info, "", err
	}
	return info, string(raw), nil
}

func upsertTrack(db *sql.DB, info YtdlpInfo, rawJson, url, mp3Path, status, errText string) error {
	stmt := `INSERT INTO tracks (ytdlp_id, url, title, uploader, duration_seconds, mp3_path, info_json, status, error_text)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(ytdlp_id) DO UPDATE SET
		url=excluded.url,
		title=excluded.title,
		uploader=excluded.uploader,
		duration_seconds=excluded.duration_seconds,
		mp3_path=excluded.mp3_path,
		info_json=excluded.info_json,
		status=excluded.status,
		error_text=excluded.error_text;`
	_, err := db.Exec(stmt, info.ID, url, info.Title, info.Uploader, int64(info.Duration), mp3Path, rawJson, status, errText)
	return err
}

func worker(id int, db *sql.DB, mp3Dir, dataDir string, jobs <-chan Job, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		fmt.Printf("[worker %d] processing %s\n", id, job.URL)

		// quick skip: if DB already has this URL with successful status, skip
		var exists int
		err := db.QueryRow("SELECT 1 FROM tracks WHERE url = ? AND status = 'downloaded' LIMIT 1", job.URL).Scan(&exists)
		if err == nil {
			fmt.Printf("[worker %d] already downloaded (DB), skipping %s\n", id, job.URL)
			continue
		}

		yid, infoPath, mp3Path, err := callYtDlp(mp3Dir, dataDir, job.URL)
		if err != nil {
			fmt.Printf("[worker %d] download failed: %v\n", id, err)
			_ = upsertTrack(db, YtdlpInfo{ID: yid}, "", job.URL, "", "failed", err.Error())
			continue
		}

		info, raw, err := parseInfoJSON(infoPath)
		if err != nil {
			fmt.Printf("[worker %d] failed to parse info json: %v\n", id, err)
			_ = upsertTrack(db, YtdlpInfo{ID: yid}, "", job.URL, mp3Path, "failed", "parse-info-json:"+err.Error())
			continue
		}

		if info.ID == "" {
			info.ID = yid
		}
		if err := upsertTrack(db, info, raw, job.URL, mp3Path, "downloaded", ""); err != nil {
			fmt.Printf("[worker %d] db insert failed: %v\n", id, err)
			continue
		}
		fmt.Printf("[worker %d] done: %s -> %s\n", id, job.URL, mp3Path)
	}
}

func readCSVUrls(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	urls := []string{}

	// optional header
	first, err := r.Read()
	if err == nil {
		if len(first) > 0 && strings.Contains(strings.ToLower(first[0]), "url") {
			// header detected -> skip
		} else if len(first) > 0 {
			urls = append(urls, strings.TrimSpace(first[0]))
		}
	} else if err == io.EOF {
		return urls, nil
	} else {
		return nil, err
	}

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rec) == 0 {
			continue
		}
		url := strings.TrimSpace(rec[0])
		if url == "" {
			continue
		}
		urls = append(urls, url)
	}
	return urls, nil
}

func main() {
	csvPath := flag.String("csv", "urls.csv", "CSV file of URLs (first column)")
	dbPath := flag.String("db", "tracks.db", "sqlite db path")
	mp3Dir := flag.String("mp3dir", "./downloads/mp3", "directory to save mp3 files (default downloads/mp3)")
	dataDir := flag.String("datadir", "./data/json", "directory to save info.json blobs (default data/json)")
	workers := flag.Int("workers", 3, "concurrent workers")
	flag.Parse()

	// create default directories
	if err := os.MkdirAll(*mp3Dir, 0o755); err != nil {
		fmt.Println("cannot create mp3 dir:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fmt.Println("cannot create data dir:", err)
		os.Exit(1)
	}

	db, err := ensureDB(*dbPath)
	if err != nil {
		fmt.Println("db error:", err)
		os.Exit(1)
	}
	defer db.Close()

	urls, err := readCSVUrls(*csvPath)
	if err != nil {
		fmt.Println("csv error:", err)
		os.Exit(1)
	}

	seen := make(map[string]struct{})
	jobs := make(chan Job, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}

		// skip if already in DB
		var exists int
		err := db.QueryRow("SELECT 1 FROM tracks WHERE url = ? AND status = 'downloaded' LIMIT 1", u).Scan(&exists)
		if err == nil {
			fmt.Printf("[main] skipping already-downloaded url: %s\n", u)
			continue
		}
		jobs <- Job{URL: u}
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(*workers)
	for i := 0; i < *workers; i++ {
		go worker(i+1, db, *mp3Dir, *dataDir, jobs, &wg)
	}
	wg.Wait()
	fmt.Println("All done at", time.Now())
}
