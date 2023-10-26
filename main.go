package main

import (
	"fmt"
	"log"
	// "net/url"
	"os" // https://pkg.go.dev/os
	"time"

	"github.com/arran4/golang-ical"
	"github.com/cavaliergopher/grab/v3" // https://pkg.go.dev/github.com/cavaliergopher/grab/v3
)

// Just using /tmp because that is simplest
const tmpDir = "/tmp/movie-cal-20231026"
const downloadDir = tmpDir + "/downloads"
const cacheDir = tmpDir + "/cache"

func ensureDirs() {
	err := os.MkdirAll(downloadDir, 0750)
	if err != nil {
		log.Fatal(err)
	}
	err = os.MkdirAll(cacheDir, 0750)
	if err != nil {
		log.Fatal(err)
	}
}

func putFileCache(cacheFileName string, path string) (string, error) {
	cachePath := cacheDir + "/" + cacheFileName
	err := os.Rename(path, cachePath)
	if err != nil {
		return "", err
	}
	return cachePath, nil
}

func getFileCache(cacheFileName string) (string, error) {
	cachePath := cacheDir + "/" + cacheFileName
	_, err := os.Lstat(cachePath)
	if err != nil {
		return "", err
	}
	return cachePath, nil
}

func downloadFile(filename string, url string) (string, error) {
	cachePath, err := getFileCache(filename)
	if err == nil {
		log.Printf("Using cached file: %s", cachePath)
		return cachePath, nil
	}
	client := grab.NewClient()
	req, _ := grab.NewRequest(downloadDir, url)

	// start download
	fmt.Printf("Downloading %v...\n", req.URL())
	resp := client.Do(req)
	fmt.Printf("  %v\n", resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			fmt.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		return "", err
	}

	fmt.Printf("Download saved to %v\n", resp.Filename)

	cachePath, err = putFileCache(filename, resp.Filename)
	if err != nil {
		return "", nil
	}
	return cachePath, nil
}

func openIcsFile(s string) (*ics.Calendar, error) {
	fileReader, err := os.Open(s)
	if err != nil {
		return nil, err
	}
	return ics.ParseCalendar(fileReader)
}

type Screening struct {
	title   string
	theater string
	time    time.Time
	url     string // url.URL
	// year string
}

func main() {
	fmt.Printf("Starting movie-cal...\n")
	ensureDirs()
	filename, err := downloadFile("cstpdx.ics", "https://cstpdx.com/schedule/list/?ical=1")
	if err != nil {
		log.Fatal(err)
	}
  cal, err := openIcsFile(filename)
	cal.SerializeTo(os.Stdout)

}
