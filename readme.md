# shiny-spork — Batch yt-dlp downloader

> **Short note:** This repo is "vibe coded as hell" — basically a quick browser-extension export → batch downloader workflow. Use Brave or any extension to collect URLs, export them as a CSV, then point this CLI at that CSV to download audio (mp3) and metadata. I will "unvibe" the project later, but for now — it works. Also AI totally made this readme boring after I gave it the summery...honestly these AI are trained with no soul or humour, or was it just the companies that made them...?

---

## What it does

Reads a CSV of URLs, downloads audio for each URL using `yt-dlp` (audio → mp3), writes a `.info.json` for each item, and stores metadata in a local SQLite database (`tracks.db` by default). Downloads and metadata are saved to configurable directories.

The program is intentionally minimal and pragmatic — meant for batch converting lists of links into MP3s + metadata.

---

## Prerequisites

Make sure you have the following installed and on your `PATH`:

- **Go** (to build / run the CLI): `go --version`
- **Python 3** (some helper actions and pip): `python --version`
- **pip**: check with `python -m pip --version` (use `python -m pip` if `pip` isn't on PATH)
- **ffmpeg** (yt-dlp needs it to extract/encode audio). On Windows you can install via Chocolatey: `choco install ffmpeg`.
- **yt-dlp**: `yt-dlp --version` (must be accessible from your shell)

> Tip: if `pip` or `yt-dlp` are not recognized, either call them with `python -m pip` or ensure the relevant `Scripts/` folder is added to your PATH.

---

## Quickstart

1. Clone the repo and `cd` into it.
2. If you just want to run without building:

```bash
# run from the repo root
go run .
```

3. Or build the binary and run it:

```bash
go build -o downloader
./downloader -csv urls.csv -workers 4
```

---

## Flags / CLI options

```
-csv       path to CSV file with URLs (default: "urls.csv")
-db        SQLite DB path (default: "tracks.db")
-mp3dir    directory to save mp3 files (default: "./downloads/mp3")
-datadir   directory to save info.json blobs (default: "./data/json")
-workers   number of concurrent workers (default: 3)
```

### Example usages

**Run with defaults:**

```bash
go run .
```

**Custom CSV file:**

```bash
go run . -csv mylist.csv
```

**Full example:**

```bash
go run . \
  -csv input.csv \
  -db downloads.db \
  -mp3dir ./out/mp3 \
  -datadir ./out/json \
  -workers 8
```

**Built binary example:**

```bash
./downloader -csv urls.csv -workers 5
```

---

## CSV format

Only the **first column** is read for the URL. A header row is allowed and detected automatically if its first cell contains the word "url" (case-insensitive).

Example `urls.csv`:

```csv
url
https://www.youtube.com/watch?v=...
https://www.youtube.com/watch?v=...
```

---

## Where files go

- MP3 files: `-mp3dir` (default `./downloads/mp3`)
- `.info.json` metadata blobs: `-datadir` (default `./data/json`)
- SQLite DB that tracks status and metadata: `-db` (default `tracks.db`)

The CLI creates directories automatically if they do not exist.

---

## Troubleshooting

- **`pip` not found:** use `python -m pip install <pkg>` or add your Python `Scripts` directory to PATH.
- **`yt-dlp` or `ffmpeg` not found:** install and ensure they are on PATH.
- **No `.info.json` produced:** yt-dlp failed for that URL — check terminal output for yt-dlp errors.
- **No `.mp3` produced:** ffmpeg missing or yt-dlp couldn't extract audio.

Look at the CLI output — workers print progress and errors to stdout/stderr.

---

## Notes / TODO

- This started as a quick and dirty workflow tied to a browser extension export — the code (and README) intentionally reflect that. Future cleanup and UX improvements are planned.
- The SQLite DB deduplicates by `ytdlp_id` and skips URLs already marked as `downloaded`.

---

## License

Pick whatever license you prefer — this README is intentionally minimal and permissive. If you want, I can add a `LICENSE` file.

---

*Have fun. If you want me to tweak the tone (more formal, more humorous, or more technical), I can update this README.*

